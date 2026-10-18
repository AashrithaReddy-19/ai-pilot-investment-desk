package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

// Balance-sheet documents through the gateway: CSV unchanged, PDF/XLSX/XLS converted, every refusal explained.

var aiRows = regexp.MustCompile(`(?i)\b(ai|software|cloud|licen[cs]e|tokens?)\b`)

// statementEngine behaves like the rules engine on the points the gateway relies on: it reads the CSV it
// is sent, numbers the lines from 1 (the header is line 1), flags rows by keyword and says where its unit
// came from. It also fails with a bare 500 on a row wider than the header, as the real one does.
type statementEngine struct {
	mu       sync.Mutex
	parses   []string          // CSV received on parse
	confirms []string          // CSV received on confirm
	fields   map[string]string // form fields of the last request
	filename string
	status   int // when set, every answer has this status with an engine-style body
	body     string
}

func (e *statementEngine) fake() *fakeRules {
	f := &fakeRules{}
	f.do = func(_ string, path string, body []byte) (*model.Upstream, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.status != 0 {
			return &model.Upstream{Status: e.status, Body: []byte(e.body), ContentType: "application/json"}, nil
		}
		_, params, _ := mime.ParseMediaType(f.lastCT)
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		e.fields = map[string]string{}
		var data []byte
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FormName() == "file" {
				data, e.filename = b, p.FileName()
			} else {
				e.fields[p.FormName()] = string(b)
			}
		}
		if path == "/balance-sheet/confirm" {
			e.confirms = append(e.confirms, string(data))
		} else {
			e.parses = append(e.parses, string(data))
		}
		cr := csv.NewReader(bytes.NewReader(data))
		cr.FieldsPerRecord, cr.LazyQuotes = -1, true
		rs, _ := cr.ReadAll()
		if len(rs) < 2 {
			return &model.Upstream{Status: 422, Body: []byte(`{"detail":"The CSV needs a header row and at least one data row."}`)}, nil
		}
		for _, r := range rs[1:] {
			if len(r) > len(rs[0]) {
				return &model.Upstream{Status: 500, Body: []byte("Internal Server Error")}, nil
			}
		}
		var cands, confs []map[string]any
		for i, r := range rs[1:] {
			if m := aiRows.FindAllString(r[0], -1); len(m) > 0 {
				var amount any
				if v := strings.ReplaceAll(strings.TrimPrefix(r[1], "-"), ",", ""); v != "" {
					f, _ := strconv.ParseFloat(v, 64)
					amount = f
				}
				for k := range m {
					m[k] = strings.ToLower(m[k])
				}
				cands = append(cands, map[string]any{"row": i + 2, "label": r[0], "matched": m, "raw": r[1], "amount_inr": amount})
				confs = append(confs, map[string]any{"row": i + 2, "label": r[0], "amount_inr": amount})
			}
		}
		unit := map[string]any{"choice": "auto", "unit": "rupees", "multiplier": 1.0, "source": "default (no unit detected)"}
		if u := e.fields["unit"]; u != "" && u != "auto" {
			unit = map[string]any{"choice": u, "unit": u, "multiplier": 1.0, "source": "your choice"}
		} else if strings.Contains(strings.ToLower(strings.Join(rs[0], " ")), "lakh") {
			unit = map[string]any{"choice": "auto", "unit": "lakh", "multiplier": 100000.0, "source": "amount-column header"}
		}
		out := map[string]any{"rows": len(rs) - 1, "amount_columns": rs[0][1:], "amount_column_used": rs[0][1], "unit": unit,
			"candidates": cands, "warnings": []string{}, "flags": []any{}, "ai_burn_to_date": nil}
		if path == "/balance-sheet/confirm" {
			out = map[string]any{"ai_burn_to_date": 1, "confirmations": confs, "unit": unit, "warnings": []string{}, "flags": []any{}}
		}
		b, _ := json.Marshal(out)
		return &model.Upstream{Status: 200, Body: b, ContentType: "application/json"}, nil
	}
	return f
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

func newStatementRouter(f *fakeRules, logs io.Writer, mod func(*config.Config)) http.Handler {
	cfg := config.Config{
		AllowedOrigins: []string{"http://localhost:5173"}, RequestTimeout: 2 * time.Second,
		MaxBodyBytes: 1 << 20, DocumentMaxBytes: 4 << 20, DocumentMaxPages: 50, DocumentTimeout: 20 * time.Second, DocumentConcurrency: 2,
	}
	if mod != nil {
		mod(&cfg)
	}
	return NewRouter(Deps{Cfg: cfg, Store: fakeStore{}, Rules: f, Cache: fakeCache{}, Log: slog.New(slog.NewTextHandler(logs, nil))})
}

func formBody(fields map[string]string, name string, data []byte) (string, string) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if data != nil {
		fw, _ := w.CreateFormFile("file", name)
		_, _ = fw.Write(data)
	}
	_ = w.Close()
	return buf.String(), w.FormDataContentType()
}

func upload(h http.Handler, path string, fields map[string]string, name string, data []byte) *httptest.ResponseRecorder {
	body, ct := formBody(fields, name, data)
	return do(h, "POST", "/api/v1/"+path, body, "Content-Type", ct)
}

func parseOut(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.Body.String())
	}
	return m
}

func errCode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]any {
	t.Helper()
	m := parseOut(t, w)
	e, _ := m["error"].(map[string]any)
	if w.Code != status || e == nil || e["code"] != code {
		t.Fatalf("want %d %s, got %d %s", status, code, w.Code, w.Body.String())
	}
	if msg, _ := e["message"].(string); strings.TrimSpace(msg) == "" || strings.HasPrefix(strings.TrimSpace(msg), "{") {
		t.Fatalf("the message must be a plain sentence: %q", msg)
	}
	if rid, _ := m["request_id"].(string); rid == "" {
		t.Fatalf("an error must carry the request id: %s", w.Body.String())
	}
	return e
}

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../docparse/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func zipBytes(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(c))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func candidatesOf(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, c := range m["candidates"].([]any) {
		out = append(out, c.(map[string]any))
	}
	return out
}

func flagCodes(m map[string]any) []string {
	var out []string
	if fl, ok := m["flags"].([]any); ok {
		for _, f := range fl {
			out = append(out, f.(map[string]any)["code"].(string))
		}
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------- CSV is unchanged ----------------

func TestCSVReachesTheEngineByteForByteAndGainsReasons(t *testing.T) {
	eng := &statementEngine{}
	logs := &lockedBuf{}
	h := newStatementRouter(eng.fake(), logs, nil)
	csvIn := "Particulars,Amount (INR lakh)\nRevenue,100\nSoftware licences,245.30\nAI platform tokens,58.75\nRent,4\n"
	body, ct := formBody(map[string]string{"unit": "auto"}, "bs.csv", []byte(csvIn))
	f := eng.fake()
	h = newStatementRouter(f, logs, nil)
	w := do(h, "POST", "/api/v1/balance-sheet/parse", body, "Content-Type", ct)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if f.lastBody != body || f.lastCT != ct {
		t.Fatal("a CSV upload must reach the engine exactly as it was sent")
	}
	m := parseOut(t, w)
	cs := candidatesOf(t, m)
	if len(cs) != 2 || cs[0]["reason"] != "Matched keyword: software" || cs[1]["reason"] != "Matched keyword: ai, tokens" {
		t.Fatalf("each flagged row says why: %+v", cs)
	}
	if _, has := cs[0]["source"]; has {
		t.Fatalf("a CSV has no sheet or page to point at, and the row check must not invent one: %+v", cs[0])
	}
	ex := m["extraction"].(map[string]any)
	if ex["source_format"] != "csv" {
		t.Fatalf("extraction: %+v", ex)
	}
	if m["amount_columns"].([]any)[0] != "Amount (INR lakh)" || m["ai_burn_to_date"] != nil {
		t.Fatalf("the engine's own fields are kept: %+v", m)
	}
}

func TestUnitAssumedAndAmbiguousColumnsAreFlagged(t *testing.T) {
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	csvIn := "Particulars,Note,2026,2025\nSoftware,3,245.30,210.00\nCloud,4,312.80,280.40\n"
	w := upload(h, "balance-sheet/parse", map[string]string{"unit": "auto"}, "bs.csv", []byte(csvIn))
	m := parseOut(t, w)
	codes := flagCodes(m)
	if !has(codes, "UNIT_ASSUMED") || !has(codes, "AMOUNT_COLUMN_CHOSEN") {
		t.Fatalf("no unit stated and several amount columns must both be flagged: %v", codes)
	}
	var msg string
	for _, f := range m["flags"].([]any) {
		if f.(map[string]any)["code"] == "AMOUNT_COLUMN_CHOSEN" {
			msg = f.(map[string]any)["message"].(string)
		}
	}
	if !strings.Contains(msg, "Note, 2026, 2025") || !strings.Contains(msg, `"Note" was used`) {
		t.Fatalf("name the columns and the one used: %q", msg)
	}
	// choosing the unit and the column removes the warnings
	w = upload(h, "balance-sheet/parse", map[string]string{"unit": "lakh", "amount_column": "2026"}, "bs.csv", []byte(csvIn))
	if codes := flagCodes(parseOut(t, w)); has(codes, "UNIT_ASSUMED") || has(codes, "AMOUNT_COLUMN_CHOSEN") {
		t.Fatalf("an explicit choice is not a guess: %v", codes)
	}
}

func TestAnswerWithoutCandidatesIsReturnedUntouched(t *testing.T) {
	f := &fakeRules{do: ok200(`{"ai_burn_to_date":   1550000}`)}
	h := newStatementRouter(f, &lockedBuf{}, nil)
	w := upload(h, "balance-sheet/parse", nil, "bs.csv", []byte("a,b\nc,1\n"))
	if w.Body.String() != `{"ai_burn_to_date":   1550000}` {
		t.Fatalf("body changed: %s", w.Body.String())
	}
}

// ---------------- converted documents ----------------

func TestXLSXIsConvertedAndEveryRowKeepsItsSource(t *testing.T) {
	eng := &statementEngine{}
	f := eng.fake()
	logs := &lockedBuf{}
	h := newStatementRouter(f, logs, nil)
	x := testXLSX()
	w := upload(h, "balance-sheet/parse", map[string]string{"unit": "auto", "sheet": "Statement"}, "Report.xlsx", x)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if eng.filename != "statement.csv" || eng.fields["sheet"] != "" || eng.fields["unit"] != "auto" {
		t.Fatalf("the engine gets the converted CSV and the person's fields, not ours: file=%q fields=%v", eng.filename, eng.fields)
	}
	if !strings.HasPrefix(eng.parses[0], "Particulars,31 March 2026 (INR lakh),31 March 2025 (INR lakh)\n") || !strings.Contains(eng.parses[0], "Software licences and subscriptions,245.30,210.00") {
		t.Fatalf("converted CSV:\n%s", eng.parses[0])
	}
	m := parseOut(t, w)
	cs := candidatesOf(t, m)
	if len(cs) != 3 {
		t.Fatalf("candidates: %+v", cs)
	}
	src := cs[0]["source"].(map[string]any)
	if src["sheet"] != "Statement" || src["row"] != float64(6) || !strings.Contains(src["text"].(string), "Software licences") {
		t.Fatalf("source of the first candidate: %+v", src)
	}
	ex := m["extraction"].(map[string]any)
	if ex["source_format"] != "xlsx" || ex["sheet"] != "Statement" || ex["unit_detected"] != "lakh" || ex["currency"] != "INR" || ex["sources_matched"] != true {
		t.Fatalf("extraction: %+v", ex)
	}
	if len(ex["sheets"].([]any)) != 2 {
		t.Fatalf("the sheet list lets the person switch: %+v", ex["sheets"])
	}
	if codes := flagCodes(m); has(codes, "UNIT_ASSUMED") {
		t.Fatalf("the sheet states lakh, so the unit was not assumed: %v", codes)
	}
	// no document content in the logs
	for _, secret := range []string{"Software licences", "245.30", "Statement", "Report.xlsx"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("the log must not contain document content (%q):\n%s", secret, logs.String())
		}
	}
}

func testXLSX() []byte {
	// one real statement sheet written as an XLSX workbook, plus a cover sheet
	rows := [][]string{{"Statement of profit and loss (INR in lakh)"}, {}, {"Particulars", "Note", "31 March 2026", "31 March 2025"},
		{"Revenue from operations", "1", "12450.75", "11200.00"}, {"Employee benefit expense", "2", "4320.50", "3980.25"},
		{"Software licences and subscriptions", "3", "245.30", "210.00"}, {"Cloud hosting charges", "4", "312.80", "280.40"},
		{"AI platform subscription (LLM tokens)", "5", "58.75", "12.50"}, {"Rent and utilities", "6", "410.00", "400.00"}}
	var sheet strings.Builder
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, r+1)
		for c, v := range row {
			ref := string(rune('A'+c)) + strconv.Itoa(r+1)
			if _, err := strconv.ParseFloat(v, 64); err == nil && r > 2 && c > 0 {
				fmt.Fprintf(&sheet, `<c r="%s"><v>%s</v></c>`, ref, v)
			} else {
				fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, v)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	return zipBytes(map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Cover" sheetId="1" r:id="rId1"/><sheet name="Statement" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Acme annual report</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet2.xml":   sheet.String(),
	})
}

func TestPDFAndXLSAreConvertedToo(t *testing.T) {
	for _, tc := range []struct {
		name, file, format string
		rows               int
	}{
		{"annual.pdf", "real_annual_report.pdf", "pdf", 12},
		{"book.xls", "statement.xls", "xls", 8},
	} {
		eng := &statementEngine{}
		h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
		w := upload(h, "balance-sheet/parse", map[string]string{"unit": "auto"}, tc.name, testdata(t, tc.file))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", tc.format, w.Code, w.Body.String())
		}
		m := parseOut(t, w)
		ex := m["extraction"].(map[string]any)
		if ex["source_format"] != tc.format || ex["converted_rows"] != float64(tc.rows) || ex["sources_matched"] != true {
			t.Fatalf("%s extraction: %+v", tc.format, ex)
		}
		for _, c := range candidatesOf(t, m) {
			src := c["source"].(map[string]any)
			if tc.format == "pdf" && src["page"] != float64(15) || tc.format == "xls" && src["sheet"] != "Statement of P&L" {
				t.Fatalf("%s source: %+v", tc.format, src)
			}
		}
		if tc.format == "pdf" {
			pages := ex["pages"].(map[string]any)
			if pages["total"] != float64(16) || len(pages["with_tables"].([]any)) != 1 {
				t.Fatalf("pages: %+v", pages)
			}
		}
	}
}

func TestParseAndConfirmReadTheDocumentTheSameWay(t *testing.T) {
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	x := testXLSX()
	opts := map[string]string{"unit": "auto", "sheet": "Statement"}
	if w := upload(h, "balance-sheet/parse", opts, "r.xlsx", x); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	confirm := map[string]string{"unit": "auto", "sheet": "Statement", "rows": "6,7"}
	w := upload(h, "balance-sheet/confirm", confirm, "r.xlsx", x)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if eng.parses[0] != eng.confirms[0] {
		t.Fatalf("parse and confirm must see the same CSV, or row numbers drift:\n%s\n---\n%s", eng.parses[0], eng.confirms[0])
	}
	if eng.fields["rows"] != "6,7" || eng.fields["sheet"] != "" {
		t.Fatalf("confirm fields: %v", eng.fields)
	}
	m := parseOut(t, w)
	conf := m["confirmations"].([]any)[0].(map[string]any)
	if conf["source"].(map[string]any)["sheet"] != "Statement" {
		t.Fatalf("a confirmed row still names its source: %+v", conf)
	}
}

func TestWrongSourceIsNeverShown(t *testing.T) {
	// an engine that numbers rows differently from the CSV it was sent
	f := &fakeRules{}
	f.do = func(string, string, []byte) (*model.Upstream, error) {
		body := `{"candidates":[{"row":3,"label":"Software licences and subscriptions","matched":["software"],"raw":"245.30","amount_inr":245}],"unit":{"choice":"auto","source":"amount-column header"},"flags":[],"warnings":[]}`
		return &model.Upstream{Status: 200, Body: []byte(body), ContentType: "application/json"}, nil
	}
	h := newStatementRouter(f, &lockedBuf{}, nil)
	w := upload(h, "balance-sheet/parse", nil, "r.xlsx", testXLSX())
	m := parseOut(t, w)
	if _, has := candidatesOf(t, m)[0]["source"]; has {
		t.Fatalf("row 3 of the CSV is not that description, so no source may be shown: %+v", candidatesOf(t, m)[0])
	}
	ex := m["extraction"].(map[string]any)
	if ex["sources_matched"] != false || !strings.Contains(fmt.Sprint(ex["notes"]), "could not be matched") {
		t.Fatalf("say that the references are unavailable: %+v", ex)
	}
}

// ---------------- refusals ----------------

func TestEveryRefusalIsExplainedWithARequestID(t *testing.T) {
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	good := testXLSX()
	pdf := testdata(t, "real_annual_report.pdf")
	for _, tc := range []struct {
		name   string
		file   string
		body   []byte
		fields map[string]string
		status int
		code   string
	}{
		{"empty", "x.pdf", []byte{}, nil, 422, "empty_file"},
		{"text named pdf", "x.pdf", []byte("plain text, not a pdf\n"), nil, 422, "file_corrupt"},
		{"binary named csv", "x.csv", bytes.Repeat([]byte{0, 1, 2}, 100), nil, 422, "unsupported_format"},
		{"word document", "x.docx", zipBytes(map[string]string{"word/document.xml": "<w/>"}), nil, 422, "unsupported_format"},
		{"truncated workbook", "x.xlsx", good[:len(good)/2], nil, 422, "file_corrupt"},
		{"truncated pdf", "x.pdf", pdf[:len(pdf)/2], nil, 422, "file_corrupt"},
		{"encrypted xls", "x.xls", testdata(t, "encrypted.xls"), nil, 422, "file_encrypted"},
		{"old excel", "x.xls", testdata(t, "old_biff5.xls"), nil, 422, "unsupported_format"},
		{"unknown sheet", "x.xlsx", good, map[string]string{"sheet": "Nope"}, 422, "sheet_not_found"},
		{"bad pages", "x.pdf", pdf, map[string]string{"pages": "99"}, 422, "invalid_pages"},
		{"no table on the chosen pages", "x.pdf", pdf, map[string]string{"pages": "1-3"}, 422, "no_table_found"},
		{"workbook with no statement", "x.xls", testdata(t, "real_excel_table.xls"), nil, 422, "no_table_found"},
		{"dollars", "x.pdf", testdata(t, "real_statement_usd.pdf"), nil, 422, "currency_not_supported"},
	} {
		w := upload(h, "balance-sheet/parse", tc.fields, tc.file, tc.body)
		t.Run(tc.name, func(t *testing.T) { errCode(t, w, tc.status, tc.code) })
	}
	if len(eng.parses) != 0 {
		t.Fatalf("the engine must not be called for a document that could not be read: %d calls", len(eng.parses))
	}
	// no file part at all
	body, ct := formBody(map[string]string{"unit": "auto"}, "", nil)
	e := errCode(t, do(h, "POST", "/api/v1/balance-sheet/parse", body, "Content-Type", ct), 422, "invalid_value")
	if e["field"] != "file" {
		t.Fatalf("name the field: %+v", e)
	}
}

// pdfOf assembles a PDF from object bodies, computing the cross-reference offsets.
func pdfOf(objs []string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func TestScannedPDFIsRefusedNotReturnedEmpty(t *testing.T) {
	// a page that only draws a picture: no text layer at all
	scan := pdfOf([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 4 >>\nstream\nAAAA\nendstream",
		"<< /Length 31 >>\nstream\nq 200 0 0 200 0 0 cm /Im0 Do Q\nendstream",
	})
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	w := upload(h, "balance-sheet/parse", nil, "scan.pdf", scan)
	e := errCode(t, w, 422, "scanned_document")
	if !strings.Contains(strings.ToLower(e["message"].(string)), "scan") || !strings.Contains(e["message"].(string), "OCR") {
		t.Fatalf("explain that it is a scan and that OCR is unavailable: %q", e["message"])
	}
	if len(eng.parses) != 0 {
		t.Fatal("a scan must never reach the engine as an empty table")
	}
}

func TestOversizedUploadIs413WithTheLimit(t *testing.T) {
	h := newStatementRouter((&statementEngine{}).fake(), &lockedBuf{}, func(c *config.Config) { c.DocumentMaxBytes = 1 << 20; c.MaxBodyBytes = 1 << 10 })
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("A"), 2<<20)...)
	e := errCode(t, upload(h, "balance-sheet/parse", nil, "big.pdf", big), 413, "payload_too_large")
	if !strings.Contains(e["message"].(string), "1 MB") {
		t.Fatalf("state the limit: %q", e["message"])
	}
	// an oversized CSV keeps the old, smaller limit and the old message
	h = newStatementRouter((&statementEngine{}).fake(), &lockedBuf{}, func(c *config.Config) { c.MaxBodyBytes = 1 << 10 })
	w := upload(h, "balance-sheet/parse", nil, "big.csv", []byte("Particulars,Amount\n"+strings.Repeat("AI software,12\n", 500)))
	errCode(t, w, 413, "payload_too_large")
	if !strings.Contains(w.Body.String(), "request body exceeds 1024 bytes") {
		t.Fatalf("the CSV limit message is unchanged: %s", w.Body.String())
	}
}

func TestBusyWhenTooManyDocumentsAreBeingRead(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		entered <- struct{}{}
		<-release // the first document stays in the engine, holding the only slot
		return &model.Upstream{Status: 200, Body: []byte(`{}`), ContentType: "application/json"}, nil
	}}
	h := newStatementRouter(f, io.Discard, func(c *config.Config) { c.DocumentConcurrency = 1 })
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- upload(h, "balance-sheet/parse", nil, "a.xlsx", testXLSX()) }()
	<-entered
	w := upload(h, "balance-sheet/parse", nil, "b.xlsx", testXLSX())
	errCode(t, w, 429, "busy")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("tell the client when to retry")
	}
	close(release)
	if r := <-first; r.Code != 200 {
		t.Fatalf("the first document is unaffected: %d %s", r.Code, r.Body.String())
	}
	// the slot is free again
	f.do = ok200(`{}`)
	if w := upload(h, "balance-sheet/parse", nil, "c.xlsx", testXLSX()); w.Code != 200 {
		t.Fatalf("a slot must be released when the request ends: %d %s", w.Code, w.Body.String())
	}
}

// ---------------- the engine's failures ----------------

func TestRowsWiderThanTheHeaderGetAClearMessageInsteadOfARulesEngineFailure(t *testing.T) {
	eng := &statementEngine{}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	csvIn := "Particulars,Amount\nRevenue,100\nCloud hosting,Rs. 1,20,000\nRent,5\n"
	e := errCode(t, upload(h, "balance-sheet/parse", nil, "bs.csv", []byte(csvIn)), 422, "csv_row_too_wide")
	msg := e["message"].(string)
	if !strings.Contains(msg, "Row 3 has 4 cells but the header row has 2") || !strings.Contains(msg, "quotes") || e["field"] != "file" {
		t.Fatalf("name the row and how to fix it: %q", msg)
	}
	// an engine failure that is not that stays an engine failure
	eng.status, eng.body = 500, "Traceback ..."
	errCode(t, upload(h, "balance-sheet/parse", nil, "ok.csv", []byte("Particulars,Amount\nA,1\n")), 502, "rules_error")
}

func TestEngineRefusalsAreShownAsSentencesNotJSON(t *testing.T) {
	eng := &statementEngine{status: 422, body: `{"detail":"No numeric amount column found; cannot read money from this sheet."}`}
	h := newStatementRouter(eng.fake(), &lockedBuf{}, nil)
	e := errCode(t, upload(h, "balance-sheet/parse", nil, "bs.csv", []byte("Particulars,Amount\nA,x\n")), 422, "rules_rejected")
	if e["message"] != "No numeric amount column found; cannot read money from this sheet." {
		t.Fatalf("message: %q", e["message"])
	}
	eng.body = `{"detail":[{"type":"missing","loc":["body","file"],"msg":"Field required","input":null}]}`
	e = errCode(t, upload(h, "balance-sheet/parse", nil, "bs.csv", []byte("Particulars,Amount\nA,x\n")), 422, "rules_rejected")
	if e["message"] != "file: Field required" {
		t.Fatalf("a validation list is joined: %q", e["message"])
	}
}

func TestDiagnoseCSV(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"unquoted grouping": {"Particulars,Amount\nCloud,Rs. 1,20,000\n", "Row 2 has 4 cells but the header row has 2"},
		"semicolons":        {"Particulars;Amount\nCloud;5;6;7\n", "Row 2 has 4 cells but the header row has 2"},
		"with BOM":          {"\xef\xbb\xbfParticulars,Amount\nA,1\nB,1,2\n", "Row 3 has 3 cells"},
		"quoted is fine":    {"Particulars,Amount\nCloud,\"1,20,000\"\n", ""},
		"narrower is fine":  {"Particulars,Amount,Note\nCloud,5\n", ""},
		"empty":             {"", ""},
	} {
		if got := diagnoseCSV([]byte(tc.in)); (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestEngineDetail(t *testing.T) {
	for in, want := range map[string]string{
		`{"detail":"Unknown unit 'x'"}`: "Unknown unit 'x'",
		`{"detail":[{"loc":["body","unit"],"msg":"bad"},{"loc":["query"],"msg":"worse"}]}`: "unit: bad; query: worse",
		`plain text`:  "plain text",
		`{"other":1}`: `{"other":1}`,
	} {
		if got := engineDetail([]byte(in)); got != want {
			t.Errorf("engineDetail(%s) = %q, want %q", in, got, want)
		}
	}
}

// ---------------- slow uploads ----------------

// slowBody delivers its data only after a delay, like a large upload over a slow link.
type slowBody struct {
	data    []byte
	delay   time.Duration
	started bool
}

func (s *slowBody) Read(p []byte) (int, error) {
	if !s.started {
		s.started = true
		time.Sleep(s.delay)
	}
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}

// strictRules fails any engine call whose context has already expired, as a real HTTP client would.
type strictRules struct{ *fakeRules }

func (s strictRules) DoUpload(ctx context.Context, path string, body []byte, ct, rid string) (*model.Upstream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.fakeRules.DoUpload(ctx, path, body, ct, rid)
}

func TestAUploadSlowerThanTheRequestTimeoutStillReachesTheEngine(t *testing.T) {
	eng := &statementEngine{}
	rules := strictRules{eng.fake()}
	h := NewRouter(Deps{Cfg: config.Config{AllowedOrigins: []string{"http://localhost:5173"}, RequestTimeout: 300 * time.Millisecond,
		MaxBodyBytes: 1 << 20, DocumentMaxBytes: 4 << 20, DocumentMaxPages: 50, DocumentTimeout: 20 * time.Second, DocumentConcurrency: 2},
		Store: fakeStore{}, Rules: rules, Cache: fakeCache{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	body, ct := formBody(map[string]string{"unit": "auto", "sheet": "Statement"}, "slow.xlsx", testXLSX())
	// the body takes longer to arrive than the whole request timeout
	req := httptest.NewRequest("POST", "/api/v1/balance-sheet/parse", &slowBody{data: []byte(body), delay: 700 * time.Millisecond})
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("a slow upload must still be read and answered once it has arrived: %d %s", w.Code, w.Body.String())
	}
	if len(eng.parses) != 1 || !strings.Contains(eng.parses[0], "Software licences") {
		t.Fatalf("the engine must receive the converted document: %v", eng.parses)
	}
	// an ordinary route keeps the short deadline
	slow := httptest.NewRequest("POST", "/api/v1/pilots", &slowBody{data: []byte("{}"), delay: 700 * time.Millisecond})
	slow.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, slow)
	if w2.Code == 200 {
		t.Fatalf("other routes must keep their short request deadline: %d %s", w2.Code, w2.Body.String())
	}
}
