package docparse

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"reflect"
	"strings"
	"testing"
)

func convert(t *testing.T, format string, b []byte, opt Options) *Result {
	t.Helper()
	res, err := Convert(context.Background(), format, b, opt)
	if err != nil {
		t.Fatalf("convert %s: %v", format, err)
	}
	return res
}

func wantErr(t *testing.T, format string, b []byte, opt Options, code string) *Error {
	t.Helper()
	res, err := Convert(context.Background(), format, b, opt)
	if err == nil {
		t.Fatalf("want error %s, got a result:\n%s", code, res.CSV)
	}
	e, ok := err.(*Error)
	if !ok || e.Code != code {
		t.Fatalf("want error code %s, got %v", code, err)
	}
	if strings.TrimSpace(e.Message) == "" {
		t.Fatalf("error %s has no message", code)
	}
	return e
}

func records(t *testing.T, res *Result) [][]string {
	t.Helper()
	rs, err := csv.NewReader(bytes.NewReader(res.CSV)).ReadAll()
	if err != nil {
		t.Fatalf("converted CSV is not valid CSV: %v\n%s", err, res.CSV)
	}
	return rs
}

// Every line has exactly the header's width: the engine's parser crashes on a row wider than its header.
func assertRectangular(t *testing.T, res *Result) {
	t.Helper()
	rs := records(t, res)
	for i, r := range rs {
		if len(r) != len(rs[0]) {
			t.Fatalf("line %d has %d fields, the header has %d: %q", i+1, len(r), len(rs[0]), r)
		}
	}
	if len(res.Lines) != len(rs) {
		t.Fatalf("%d source references for %d CSV lines", len(res.Lines), len(rs))
	}
}

var wantStatement = [][]string{
	{"Revenue from operations", "12450.75", "11200.00"},
	{"Employee benefit expense", "4320.50", "3980.25"},
	{"Software licences and subscriptions", "245.30", "210.00"},
	{"Cloud hosting charges", "312.80", "280.40"},
	{"AI platform subscription (LLM tokens)", "58.75", "12.50"},
	{"Rent and utilities", "410.00", "400.00"},
	{"Professional fees", "-120.00", "-95.00"},
	{"Depreciation and amortisation", "198.40", "187.10"},
}

func commaless(rows [][]string) [][]string {
	out := make([][]string, len(rows))
	for i, r := range rows {
		out[i] = make([]string, len(r))
		for j, c := range r {
			out[i][j] = strings.ReplaceAll(c, ",", "")
		}
	}
	return out
}

// ---------------- detection ----------------

func TestDetect(t *testing.T) {
	x := buildXLSX([]xsheet{statementSheet("S", "t")})
	pdf := buildPDF([]tpage{statementPage("", statementRows, 0, 1, 1, true, "")}, pdfOpts{})
	xls, _ := os.ReadFile("testdata/statement.xls")
	encOOXML := append(append([]byte{}, oleMagic...), utf16le("EncryptedPackage")...)
	for _, tc := range []struct {
		name string
		file string
		body []byte
		want string
		code string
	}{
		{"a.pdf", "pdf", pdf, FormatPDF, ""},
		{"a.xlsx", "xlsx", x, FormatXLSX, ""},
		{"a.xls", "xls", xls, FormatXLS, ""},
		{"a.csv", "csv", []byte("Particulars,Amount\nA,1\n"), FormatCSV, ""},
		{"a.csv", "csv with BOM", append([]byte("\xef\xbb\xbf"), "Particulars,Amount\nA,1\n"...), FormatCSV, ""},
		{"export", "csv without an extension", []byte("Particulars,Amount\nA,1\n"), FormatCSV, ""},
		{"renamed.csv", "pdf renamed .csv is still a pdf", pdf, FormatPDF, ""},
		{"empty.pdf", "empty", nil, "", CodeEmptyFile},
		{"fake.pdf", "text named .pdf", []byte("just some text\n"), "", CodeCorrupt},
		{"fake.xlsx", "text named .xlsx", []byte("a,b\n1,2\n"), "", CodeCorrupt},
		{"fake.xls", "binary named .xls", bytes.Repeat([]byte{0, 1, 2, 3}, 50), "", CodeCorrupt},
		{"x.csv", "binary named .csv", bytes.Repeat([]byte{0, 1, 2, 3}, 50), "", CodeUnsupportedFormat},
		{"note.docx", "zip that is not a workbook", zipOf(map[string]string{"[Content_Types].xml": "<x/>", "word/document.xml": "<w/>"}), "", CodeUnsupportedFormat},
		{"a.xlsx", "truncated zip", x[:len(x)/2], "", CodeCorrupt},
		{"a.xlsx", "password-protected workbook", encOOXML, "", CodeEncrypted},
	} {
		got, err := Detect(tc.name, tc.body)
		if tc.code == "" {
			if err != nil || got != tc.want {
				t.Errorf("%s: want %s, got %q %v", tc.file, tc.want, got, err)
			}
			continue
		}
		if e, ok := err.(*Error); !ok || e.Code != tc.code {
			t.Errorf("%s: want error %s, got %q %v", tc.file, tc.code, got, err)
		}
	}
}

func zipOf(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := newZip(&buf)
	for n, c := range files {
		w, _ := zw.Create(n)
		_, _ = w.Write([]byte(c))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// ---------------- amounts and units ----------------

func TestParseAmount(t *testing.T) {
	for _, tc := range []struct {
		in       string
		ok       bool
		blank    bool
		neg      bool
		currency string
		suffix   string
		number   string
	}{
		{"12,450.75", true, false, false, "", "", "12,450.75"},
		{"1,20,000", true, false, false, "", "", "1,20,000"},
		{"(120.00)", true, false, true, "", "", "120.00"},
		{"-95", true, false, true, "", "", "95"},
		{"−500", true, false, true, "", "", "500"}, // Unicode minus
		{"₹ 2.5 cr", true, false, false, "INR", "cr", "2.5"},
		{"Rs. 1,20,000", true, false, false, "INR", "", "1,20,000"},
		{"INR 250", true, false, false, "INR", "", "250"},
		{"250 INR", true, false, false, "INR", "", "250"},
		{"$ 1,000", true, false, false, "USD", "", "1,000"},
		{"€5", true, false, false, "EUR", "", "5"},
		{"50k", true, false, false, "", "k", "50"},
		{"1.2 L", true, false, false, "", "L", "1.2"},
		{"12,000 Cr", true, false, false, "", "Cr", "12,000"},
		{"4,500 Dr", true, false, false, "", "Dr", "4,500"},
		{"12.5%", true, false, false, "", "", "12.5"},
		{"—", true, true, false, "", "", ""},
		{"n/a", true, true, false, "", "", ""},
		{"31 March 2026", false, false, false, "", "", ""},
		{"FY 2025-26", false, false, false, "", "", ""},
		{"Note 3", false, false, false, "", "", ""},
		{"Software licences", false, false, false, "", "", ""},
		{"1,2,3 and 4", false, false, false, "", "", ""},
		{"2)", false, false, false, "", "", ""},      // the tail of "(segment 2)"
		{"(120.00", false, false, false, "", "", ""}, // unclosed bracket
		{"(₹ 1,20,000)", true, false, true, "INR", "", "1,20,000"},
		{"", false, false, false, "", "", ""},
	} {
		a := parseAmount(tc.in)
		if a.OK != tc.ok || a.Blank != tc.blank || (tc.ok && !tc.blank && (a.Negative != tc.neg || a.Currency != tc.currency || a.Suffix != tc.suffix || a.Number != tc.number)) {
			t.Errorf("parseAmount(%q) = %+v", tc.in, a)
		}
	}
}

// A figure is copied through: never multiplied, rounded or re-grouped.
func TestCleanAmountKeepsTheFigure(t *testing.T) {
	for in, want := range map[string]string{
		"₹ 1,20,000.50": "1,20,000.50",
		"Rs. 250":       "250",
		"(120.00)":      "-120.00",
		"−500":          "-500",
		"2.5 cr":        "2.5 cr",
		"12,000 Cr":     "12,000 Cr",
		"—":             "",
		"see note":      "see note",
	} {
		if got := cleanAmountText(in, parseAmount(in)); got != want {
			t.Errorf("cleanAmountText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnitsIn(t *testing.T) {
	for in, want := range map[string][]string{
		"Statement of profit and loss (INR in lakh)": {unitLakh},
		"(₹ in crore)":                          {unitCrore},
		"All amounts in INR lakh unless stated": {unitLakh},
		"Rs. in thousands":                      {unitThousands},
		"(Rs. in '000)":                         {unitThousands},
		"Amounts in millions of dollars":        {unitMillions},
		"(in INR crores) and (in INR lakh)":     {unitCrore, unitLakh},
		"Amount in rupees":                      {unitRupees},
		"Software licences and subscriptions":   nil,
		"Cr and Dr columns":                     nil,
		"The lakh economy grew":                 nil,
		"Particulars":                           nil,
		"31 March 2026":                         nil,
	} {
		if got := unitsIn(in); !reflect.DeepEqual(got, want) {
			t.Errorf("unitsIn(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSerialToText(t *testing.T) {
	for _, tc := range []struct {
		serial float64
		d1904  bool
		want   string
	}{
		{46112, false, "2026-03-31"},
		{43531.55069444444, false, "2019-03-07 13:13:00"},
		{1, false, "1900-01-01"},
		{61, false, "1900-03-01"},
		{0, true, "1904-01-01"},
	} {
		if got := serialToText(tc.serial, tc.d1904); got != tc.want {
			t.Errorf("serialToText(%v, %v) = %q, want %q", tc.serial, tc.d1904, got, tc.want)
		}
	}
	if !isDateFormat("DD/MM/YYYY\\ HH:MM:SS") || !isDateFormat("d-mmm-yy") || isDateFormat("#,##0.00;(#,##0.00)") || isDateFormat("0.00%") || isDateFormat("General") || isDateFormat(`0.0 "dm"`) {
		t.Error("isDateFormat misclassifies a number format")
	}
}

// ---------------- XLSX ----------------

func TestXLSXPicksTheStatementSheetAndReadsExactly(t *testing.T) {
	x := buildXLSX([]xsheet{
		{name: "Cover", rows: [][]any{{"Acme Ltd - annual report extract"}, {"All amounts in INR lakh unless stated"}}},
		statementSheet("Statement of P&L", "Statement of profit and loss (INR in lakh)"),
		{name: "Notes", rows: [][]any{{"Note", "Detail"}, {"Note 3", "Includes cloud-based software"}}},
	})
	res := convert(t, FormatXLSX, x, Options{})
	assertRectangular(t, res)
	rs := records(t, res)
	if !reflect.DeepEqual(rs[0], []string{"Particulars", "31 March 2026 (INR lakh)", "31 March 2025 (INR lakh)"}) {
		t.Fatalf("header: %q", rs[0])
	}
	if !reflect.DeepEqual(rs[1:], wantStatement) {
		t.Fatalf("rows:\n got %q\nwant %q", rs[1:], wantStatement)
	}
	if res.Sheet != "Statement of P&L" || res.Unit != unitLakh || res.Currency != "INR" || res.Rows != 8 {
		t.Fatalf("result: sheet=%q unit=%q currency=%q rows=%d", res.Sheet, res.Unit, res.Currency, res.Rows)
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Header != "Note" {
		t.Fatalf("the Note column is a reference column, not an amount: %+v", res.Dropped)
	}
	if len(res.Sheets) != 3 || !res.Sheets[1].Suggested || !res.Sheets[1].Usable || res.Sheets[0].Usable || res.Sheets[2].Usable {
		t.Fatalf("sheet list: %+v", res.Sheets)
	}
	// every converted line points back at its sheet and row
	if res.Lines[3].Sheet != "Statement of P&L" || res.Lines[3].Row != 6 || !strings.Contains(res.Lines[3].Text, "Software licences") {
		t.Fatalf("source of line 4: %+v", res.Lines[3])
	}
}

func TestXLSXSheetChoice(t *testing.T) {
	a := statementSheet("Standalone", "Standalone (INR in crore)")
	b := statementSheet("Consolidated", "Consolidated (INR in lakh)")
	x := buildXLSX([]xsheet{a, b})
	res := convert(t, FormatXLSX, x, Options{Sheet: "consolidated"}) // case-insensitive
	if res.Sheet != "Consolidated" || res.Unit != unitLakh {
		t.Fatalf("chosen sheet: %q unit %q", res.Sheet, res.Unit)
	}
	if res := convert(t, FormatXLSX, x, Options{Sheet: "Standalone"}); res.Unit != unitCrore {
		t.Fatalf("each sheet keeps its own unit: %q", res.Unit)
	}
	e := wantErr(t, FormatXLSX, x, Options{Sheet: "Nope"}, CodeSheetNotFound)
	if !strings.Contains(e.Message, "Standalone") || !strings.Contains(e.Message, "Consolidated") || e.Field != "sheet" {
		t.Fatalf("the error should list the sheets: %+v", e)
	}
}

func TestXLSXNumbersDatesFormulasAndHiddenSheets(t *testing.T) {
	hidden := statementSheet("Hidden", "(INR in lakh)")
	hidden.hidden = true
	real := xsheet{name: "Sheet1", rows: [][]any{
		{"Description", dateSerial(46112), dateSerial(45747)},
		{"Software subscription", numTxt("2.5"), numTxt("1.2345678E3")},
		{"Cloud compute", numTxt("120000"), numTxt("0.1")},
		{"AI tokens", formulaNoValue{}, numTxt("7500")},
		{"Licences", numTxt("-300"), numTxt("100")},
	}}
	res := convert(t, FormatXLSX, buildXLSX([]xsheet{hidden, real}), Options{})
	rs := records(t, res)
	if res.Sheet != "Sheet1" {
		t.Fatalf("a hidden sheet must not be the default, got %q", res.Sheet)
	}
	if !reflect.DeepEqual(rs[0], []string{"Description", "2026-03-31", "2025-03-31"}) {
		t.Fatalf("date headings must read as dates, not serial numbers: %q", rs[0])
	}
	want := [][]string{
		{"Software subscription", "2.5", "1234.5678"}, // exponent notation expanded, no digit lost
		{"Cloud compute", "120000", "0.1"},
		{"AI tokens", "", "7500"}, // a formula with no stored result is blank, never guessed
		{"Licences", "-300", "100"},
	}
	if !reflect.DeepEqual(rs[1:], want) {
		t.Fatalf("rows:\n got %q\nwant %q", rs[1:], want)
	}
	if !hasFlag(res, "FORMULA_NO_VALUE") {
		t.Fatalf("missing the formula warning: %+v", res.Flags)
	}
	// choosing the hidden sheet explicitly still works, and says so
	h := convert(t, FormatXLSX, buildXLSX([]xsheet{hidden, real}), Options{Sheet: "Hidden"})
	if !strings.Contains(strings.Join(h.Notes, " "), "hidden") {
		t.Fatalf("notes should mention the hidden sheet: %v", h.Notes)
	}
	if !res.Sheets[0].Hidden {
		t.Fatal("sheet list should mark the hidden sheet")
	}
}

func hasFlag(res *Result, code string) bool {
	for _, f := range res.Flags {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestXLSXFailuresAreExplained(t *testing.T) {
	good := buildXLSX([]xsheet{statementSheet("S", "(INR in lakh)")})
	wantErr(t, FormatXLSX, []byte("not a zip"), Options{}, CodeCorrupt)
	wantErr(t, FormatXLSX, good[:len(good)/3], Options{}, CodeCorrupt)
	wantErr(t, FormatXLSX, buildXLSX([]xsheet{{name: "Empty"}}), Options{}, CodeNoTable)
	wantErr(t, FormatXLSX, buildXLSX([]xsheet{{name: "Text", rows: [][]any{{"Particulars", "Amount"}, {"Software", "n/a"}, {"Cloud", "—"}}}}), Options{}, CodeNoTable)
	wantErr(t, FormatXLSX, buildXLSX([]xsheet{statementSheet("S", "USD in millions")}), Options{}, CodeCurrency)
	if res := convert(t, FormatXLSX, buildXLSX([]xsheet{statementSheet("S", "(INR in millions)")}), Options{}); res.Unit != unitMillions {
		t.Fatalf("millions are read, not refused: %q", res.Unit)
	}
	// a sheet over the row cap is refused, not truncated
	e := wantErr(t, FormatXLSX, good, Options{MaxRows: 3}, CodeTooLarge)
	if !strings.Contains(e.Message, "3") {
		t.Fatalf("the message should state the limit: %q", e.Message)
	}
}

func TestStatedUnitsConflictAreFlaggedNotGuessed(t *testing.T) {
	s := statementSheet("S", "Statement (INR in lakh)")
	s.rows[2][2] = "31 March 2026 (INR in crore)" // the heading contradicts the title
	res := convert(t, FormatXLSX, buildXLSX([]xsheet{s}), Options{})
	if res.Unit != "" || !hasFlag(res, "UNIT_AMBIGUOUS") {
		t.Fatalf("conflicting units must be flagged and not applied: unit=%q flags=%+v", res.Unit, res.Flags)
	}
	for _, h := range records(t, res)[0][1:] {
		if strings.Contains(h, "(INR lakh)") {
			t.Fatalf("no unit may be added when they conflict: %q", h)
		}
	}
}

func TestNoUnitStatedAddsNothing(t *testing.T) {
	res := convert(t, FormatXLSX, buildXLSX([]xsheet{statementSheet("S", "Statement of profit and loss")}), Options{})
	if res.Unit != "" || res.UnitSource != "" {
		t.Fatalf("no unit is stated, so none may be invented: %q", res.Unit)
	}
	if h := records(t, res)[0]; h[1] != "31 March 2026" {
		t.Fatalf("the heading must be left exactly as written: %q", h)
	}
}

func TestCrDrIsFlagged(t *testing.T) {
	s := xsheet{name: "S", rows: [][]any{{"Description", "Amount (INR lakh)"}, {"Cloud services", "12,000 Cr"}, {"Software", "5,000 Dr"}, {"Rent", "7"}}}
	res := convert(t, FormatXLSX, buildXLSX([]xsheet{s}), Options{})
	if !hasFlag(res, "AMOUNT_SUFFIX_CR_DR") {
		t.Fatalf("Cr/Dr is ambiguous with crore and must be flagged: %+v", res.Flags)
	}
	if rs := records(t, res); rs[1][1] != "12,000 Cr" {
		t.Fatalf("the figure itself must be kept as printed: %q", rs[1])
	}
}

func TestRowsAreNeverWiderThanTheHeader(t *testing.T) {
	// merged-cell style sheet: a banner row, a ragged notes column, an amount with unquoted-looking commas
	s := xsheet{name: "S", rows: [][]any{
		{"Annual report"},
		{},
		{"Particulars", "Amount"},
		{"Cloud software", numTxt("100"), "extra note", "more", "even more"},
		{"AI spend", "Rs. 1,20,000", "n"},
		{"Licence", numTxt("5")},
	}}
	res := convert(t, FormatXLSX, buildXLSX([]xsheet{s}), Options{})
	assertRectangular(t, res)
	if rs := records(t, res); rs[2][1] != "1,20,000" {
		t.Fatalf("a quoted amount keeps its grouping and loses only the currency marker: %q", rs[2])
	}
}

// ---------------- XLS ----------------

func TestXLSReadsEveryNumberEncodingExactly(t *testing.T) {
	b, err := os.ReadFile("testdata/statement.xls")
	if err != nil {
		t.Fatal(err)
	}
	sheets, err := readXLS(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 4 || sheets[1].Name != "Statement of P&L" || !sheets[3].Hidden || sheets[0].Hidden {
		t.Fatalf("sheets: %d %q hidden=%v/%v", len(sheets), sheets[1].Name, sheets[0].Hidden, sheets[3].Hidden)
	}
	// RK integer/100 (245.30), RK negative integer (-95), RK integer (11200), float, custom-format NUMBER
	var got [][]string
	for _, r := range sheets[1].Rows[2:] {
		got = append(got, r.Cells)
	}
	want := [][]string{
		{"Revenue from operations", "1", "12450.75", "11200"},
		{"Employee benefit expense", "2", "4320.5", "3980.25"},
		{"Software licences and subscriptions", "3", "245.3", "210"},
		{"Cloud hosting charges", "4", "312.8", "280.4"},
		{"AI platform subscription (LLM tokens)", "5", "58.75", "12.5"},
		{"Rent and utilities", "6", "410", "400"},
		{"Professional fees", "7", "-120", "-95"},
		{"Depreciation and amortisation", "8", "198.4", "187.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("XLS cells:\n got %q\nwant %q", got, want)
	}
	if sheets[1].Rows[1].Src.Row != 3 || sheets[1].Rows[1].Cells[0] != "Particulars" {
		t.Fatalf("row numbers are 1-based sheet rows: %+v", sheets[1].Rows[1].Src)
	}
}

func TestXLSConvertsLikeXLSX(t *testing.T) {
	b, _ := os.ReadFile("testdata/statement.xls")
	res := convert(t, FormatXLS, b, Options{})
	assertRectangular(t, res)
	rs := records(t, res)
	if res.Sheet != "Statement of P&L" || res.Unit != unitLakh || res.Currency != "INR" {
		t.Fatalf("sheet=%q unit=%q currency=%q", res.Sheet, res.Unit, res.Currency)
	}
	wantXLS := [][]string{
		{"Revenue from operations", "12450.75", "11200"}, {"Employee benefit expense", "4320.5", "3980.25"},
		{"Software licences and subscriptions", "245.3", "210"}, {"Cloud hosting charges", "312.8", "280.4"},
		{"AI platform subscription (LLM tokens)", "58.75", "12.5"}, {"Rent and utilities", "410", "400"},
		{"Professional fees", "-120", "-95"}, {"Depreciation and amortisation", "198.4", "187.1"},
	}
	if !reflect.DeepEqual(rs[1:], wantXLS) {
		t.Fatalf("rows:\n got %q\nwant %q", rs[1:], wantXLS)
	}
	if len(res.Sheets) != 4 || !res.Sheets[3].Hidden || res.Sheets[3].Suggested {
		t.Fatalf("sheet list: %+v", res.Sheets)
	}
}

func TestXLSOpensARealExcelFile(t *testing.T) {
	b, err := os.ReadFile("testdata/real_excel_table.xls")
	if err != nil {
		t.Fatal(err)
	}
	if f, err := Detect("Table.xls", b); err != nil || f != FormatXLS {
		t.Fatalf("detect: %q %v", f, err)
	}
	sheets, err := readXLS(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheets) != 1 || sheets[0].Name != "Table" || len(sheets[0].Rows) != 12 {
		t.Fatalf("real workbook: %d sheets, %d rows", len(sheets), len(sheets[0].Rows))
	}
	if !reflect.DeepEqual(sheets[0].Rows[0].Cells, []string{"Code", "Name", "Description"}) || sheets[0].Rows[1].Cells[0] != "code1" {
		t.Fatalf("real workbook cells: %q %q", sheets[0].Rows[0].Cells, sheets[0].Rows[1].Cells)
	}
	// it holds no amounts, so converting it must say so instead of returning something
	e := wantErr(t, FormatXLS, b, Options{}, CodeNoTable)
	if !strings.Contains(e.Message, "amount") {
		t.Fatalf("the message should say what is missing: %q", e.Message)
	}
}

func TestXLSFailures(t *testing.T) {
	enc, _ := os.ReadFile("testdata/encrypted.xls")
	old, _ := os.ReadFile("testdata/old_biff5.xls")
	good, _ := os.ReadFile("testdata/statement.xls")
	wantErr(t, FormatXLS, enc, Options{}, CodeEncrypted)
	e := wantErr(t, FormatXLS, old, Options{}, CodeUnsupportedFormat)
	if !strings.Contains(e.Message, "XLSX") {
		t.Fatalf("tell the user what to do: %q", e.Message)
	}
	wantErr(t, FormatXLS, []byte("not an ole2 file at all"), Options{}, CodeCorrupt)
	wantErr(t, FormatXLS, good[:700], Options{}, CodeCorrupt)  // cut inside the container
	wantErr(t, FormatXLS, good[:6000], Options{}, CodeCorrupt) // cut inside the workbook stream
	// flipped bytes anywhere must be an error or a result, never a panic or a hang
	for i := 512; i < len(good); i += 97 {
		bad := append([]byte{}, good...)
		bad[i] ^= 0xFF
		_, _ = Convert(context.Background(), FormatXLS, bad, Options{})
	}
}

// ---------------- PDF ----------------

func statementPDF(pages int) []byte {
	if pages == 3 {
		return buildPDF([]tpage{
			statementPage("(INR in lakh)", statementRows[:3], 0, 1, 3, true, "Statement of profit and loss"),
			statementPage("(INR in lakh)", statementRows[3:6], 0, 2, 3, true, ""),
			statementPage("(INR in lakh)", statementRows[6:], 0, 3, 3, true, ""),
		}, pdfOpts{})
	}
	return buildPDF([]tpage{statementPage("(INR in lakh)", statementRows, 0, 1, 1, true, "Statement of profit and loss")}, pdfOpts{})
}

func TestPDFTableAcrossPages(t *testing.T) {
	res := convert(t, FormatPDF, statementPDF(3), Options{})
	assertRectangular(t, res)
	rs := records(t, res)
	if !reflect.DeepEqual(rs[0], []string{"Particulars", "31 March 2026 (INR lakh)", "31 March 2025 (INR lakh)"}) {
		t.Fatalf("header: %q", rs[0])
	}
	if !reflect.DeepEqual(rs[1:], commaless(wantStatementPDF())) && !reflect.DeepEqual(rs[1:], wantStatementPDF()) {
		t.Fatalf("rows:\n got %q\nwant %q", rs[1:], wantStatementPDF())
	}
	if res.Unit != unitLakh || res.Currency != "INR" || res.Rows != 8 {
		t.Fatalf("unit=%q currency=%q rows=%d", res.Unit, res.Currency, res.Rows)
	}
	if res.Pages == nil || res.Pages.Total != 3 || !reflect.DeepEqual(res.Pages.WithTables, []int{1, 2, 3}) || len(res.Pages.NoText) != 0 {
		t.Fatalf("pages: %+v", res.Pages)
	}
	// each row knows its page: rows 4-6 are on page 2
	pagesOf := []int{}
	for _, l := range res.Lines[1:] {
		pagesOf = append(pagesOf, l.Page)
	}
	if !reflect.DeepEqual(pagesOf, []int{1, 1, 1, 2, 2, 2, 3, 3}) {
		t.Fatalf("source pages: %v", pagesOf)
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Header != "Note" {
		t.Fatalf("dropped: %+v", res.Dropped)
	}
	if hasFlag(res, "PDF_PAGES_NO_TEXT") || hasFlag(res, "PDF_COLUMNS_SHIFTED") {
		t.Fatalf("unexpected flags: %+v", res.Flags)
	}
}

// the PDF prints grouped thousands with commas; the CSV keeps them (quoted) exactly as printed
func wantStatementPDF() [][]string {
	out := make([][]string, len(wantStatement))
	for i, r := range wantStatement {
		out[i] = append([]string{}, r...)
	}
	out[0][1], out[0][2] = "12,450.75", "11,200.00"
	out[1][1], out[1][2] = "4,320.50", "3,980.25"
	return out
}

func TestPDFSingleCompactPage(t *testing.T) {
	res := convert(t, FormatPDF, statementPDF(1), Options{})
	assertRectangular(t, res)
	if rs := records(t, res); len(rs) != 9 {
		t.Fatalf("rows: %d", len(rs))
	}
}

func TestPDFWrappedDescriptionIsJoined(t *testing.T) {
	rows := [][]string{
		{"Revenue from operations", "1", "100.00", "90.00"},
		{"Software licences and", "", "", ""},
		{"subscriptions", "2", "245.30", "210.00"},
		{"Cloud hosting charges", "3", "312.80", "280.40"},
		{"Rent", "4", "410.00", "400.00"},
	}
	res := convert(t, FormatPDF, buildPDF([]tpage{statementPage("(INR in lakh)", rows, 0, 1, 1, true, "")}, pdfOpts{}), Options{})
	rs := records(t, res)
	found := false
	for _, r := range rs[1:] {
		if r[0] == "Software licences and subscriptions" && r[1] == "245.30" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the wrapped description should be one row with its amount: %q", rs)
	}
}

func TestPDFColumnsThatShiftBetweenPagesAreFlagged(t *testing.T) {
	b := buildPDF([]tpage{
		statementPage("(INR in lakh)", statementRows[:4], 0, 1, 2, true, "Statement"),
		statementPage("(INR in lakh)", statementRows[4:], 38, 2, 2, true, ""),
	}, pdfOpts{})
	res := convert(t, FormatPDF, b, Options{})
	assertRectangular(t, res)
	if !hasFlag(res, "PDF_COLUMNS_SHIFTED") {
		t.Fatalf("shifted columns must be flagged: %+v", res.Flags)
	}
	rs := records(t, res)
	if len(rs) != 9 || rs[5][2] == "" { // row 5 is the first of page 2: its previous-year figure must still be in place
		t.Fatalf("rows on the shifted page must still land in the right columns: %q", rs)
	}
}

func TestPDFNarrativePagesAreIgnored(t *testing.T) {
	narrative := func(n int) tpage {
		return tpage{lines: []tline{
			{y: 780, cells: []tcell{{x: 50, text: "Management discussion and analysis"}}},
			{y: 760, cells: []tcell{{x: 50, text: "Revenue grew 12% in FY 2025 as cloud adoption rose across the business units"}}},
			{y: 740, cells: []tcell{{x: 50, text: "The company employed 1,250 people at the end of the year and opened 3 offices"}}},
			{y: 720, cells: []tcell{{x: 50, text: "Further discussion of strategy, risks and outlook follows on the next pages of this report"}}},
		}}
	}
	b := buildPDF([]tpage{narrative(1), narrative(2), statementPage("(INR in lakh)", statementRows, 0, 3, 4, true, "Statement"), narrative(4)}, pdfOpts{})
	res := convert(t, FormatPDF, b, Options{})
	if !reflect.DeepEqual(res.Pages.WithTables, []int{3}) {
		t.Fatalf("only page 3 holds the table: %+v", res.Pages)
	}
	for _, l := range res.Lines[1:] {
		if l.Page != 3 {
			t.Fatalf("a row came from narrative page %d: %+v", l.Page, l)
		}
	}
	if rs := records(t, res); len(rs) != 9 {
		t.Fatalf("rows: %d\n%q", len(rs), rs)
	}
}

func TestPDFPageSelection(t *testing.T) {
	b := statementPDF(3)
	res := convert(t, FormatPDF, b, Options{Pages: "2"})
	rs := records(t, res)
	if len(rs) != 4 || rs[1][0] != "Cloud hosting charges" || !reflect.DeepEqual(res.Pages.Selected, []int{2}) {
		t.Fatalf("page 2 only: %q %+v", rs, res.Pages)
	}
	if res := convert(t, FormatPDF, b, Options{Pages: "1-2, 3"}); res.Rows != 8 {
		t.Fatalf("1-2, 3 is every page: %d rows", res.Rows)
	}
	for _, bad := range []string{"0", "4", "3-1", "a", "1,,2", "1-99", "-"} {
		e := wantErr(t, FormatPDF, b, Options{Pages: bad}, CodeInvalidPages)
		if e.Field != "pages" || !strings.Contains(e.Message, "3 pages") {
			t.Fatalf("pages %q: %+v", bad, e)
		}
	}
	e := wantErr(t, FormatPDF, b, Options{MaxPages: 2}, CodeTooManyPages)
	if !strings.Contains(e.Message, "pages=") {
		t.Fatalf("tell the user how to narrow it: %q", e.Message)
	}
	if got, _ := parsePages("10-", 12); !reflect.DeepEqual(got, []int{10, 11, 12}) {
		t.Fatalf("open-ended range: %v", got)
	}
}

func TestPDFScannedAndUnreadable(t *testing.T) {
	e := wantErr(t, FormatPDF, imagePDF(), Options{}, CodeScanned)
	if !strings.Contains(strings.ToLower(e.Message), "scan") || !strings.Contains(e.Message, "OCR") {
		t.Fatalf("say it is a scan and that OCR is unavailable: %q", e.Message)
	}
	// a page with no text beside pages with a table is skipped and reported, not silently ignored
	b := buildPDF([]tpage{statementPage("(INR in lakh)", statementRows, 0, 1, 2, true, "Statement"), {lines: nil}}, pdfOpts{})
	res := convert(t, FormatPDF, b, Options{})
	if !hasFlag(res, "PDF_PAGES_NO_TEXT") || !reflect.DeepEqual(res.Pages.NoText, []int{2}) {
		t.Fatalf("the skipped page must be reported: %+v %+v", res.Flags, res.Pages)
	}
}

func TestPDFDamagedAndEncrypted(t *testing.T) {
	good := statementPDF(1)
	wantErr(t, FormatPDF, buildPDF([]tpage{statementPage("", statementRows, 0, 1, 1, true, "")}, pdfOpts{encrypt: true}), Options{}, CodeEncrypted)
	wantErr(t, FormatPDF, good[:len(good)/2], Options{}, CodeCorrupt)
	wantErr(t, FormatPDF, []byte("%PDF-1.4\nnonsense without any structure\n"), Options{}, CodeCorrupt)
	wantErr(t, FormatPDF, append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{0xFF, 0x00, 0x13}, 400)...), Options{}, CodeCorrupt)
	// damage anywhere inside the file must be an error or a result: never a panic, never a hang
	for i := 9; i < len(good); i += 41 {
		bad := append([]byte{}, good...)
		bad[i] ^= 0x5A
		_, _ = Convert(context.Background(), FormatPDF, bad, Options{})
	}
}

func TestPDFWithoutFontWidthsStillReads(t *testing.T) {
	// base-14 fonts may carry no /Widths: positions inside a string are then unknown, but cells still split
	rows := statementRows
	b := buildPDF([]tpage{statementPage("(INR in lakh)", rows, 0, 1, 1, true, "Statement")}, pdfOpts{noWidths: true})
	res := convert(t, FormatPDF, b, Options{})
	assertRectangular(t, res)
	if rs := records(t, res); len(rs) != 9 || rs[3][0] != "Software licences and subscriptions" {
		t.Fatalf("rows: %q", rs)
	}
}

func TestPDFCurrencyAndUnitRules(t *testing.T) {
	usd := buildPDF([]tpage{statementPage("(USD in millions)", statementRows, 0, 1, 1, true, "Statement")}, pdfOpts{})
	wantErr(t, FormatPDF, usd, Options{}, CodeCurrency)
	mn := buildPDF([]tpage{statementPage("(INR in millions)", statementRows, 0, 1, 1, true, "Statement")}, pdfOpts{})
	if res := convert(t, FormatPDF, mn, Options{}); res.Unit != unitMillions || !strings.Contains(string(res.CSV), "INR millions") {
		t.Fatalf("millions are read and named in the heading: %q %s", res.Unit, res.CSV)
	}
	// page 1 states lakh and page 2 states crore: no single unit is right for both
	mixed := buildPDF([]tpage{
		statementPage("(INR in lakh)", statementRows[:4], 0, 1, 2, true, "Statement"),
		statementPage("(INR in crore)", statementRows[4:], 0, 2, 2, true, ""),
	}, pdfOpts{})
	res := convert(t, FormatPDF, mixed, Options{})
	if res.Unit != "" || !hasFlag(res, "UNIT_AMBIGUOUS") {
		t.Fatalf("mixed units must be flagged and none applied: %q %+v", res.Unit, res.Flags)
	}
	// narrowing to one page resolves it
	if one := convert(t, FormatPDF, mixed, Options{Pages: "2"}); one.Unit != unitCrore || hasFlag(one, "UNIT_AMBIGUOUS") {
		t.Fatalf("page 2 alone is crore: %q %+v", one.Unit, one.Flags)
	}
	// a page without its own unit line continues the unit of the page before
	cont := buildPDF([]tpage{
		statementPage("(INR in crore)", statementRows[:4], 0, 1, 2, true, "Statement"),
		statementPage("", statementRows[4:], 0, 2, 2, true, ""),
	}, pdfOpts{})
	if res := convert(t, FormatPDF, cont, Options{}); res.Unit != unitCrore || res.Pages.Units[2] != unitCrore {
		t.Fatalf("unit should carry to the next page: %q %v", res.Unit, res.Pages.Units)
	}
}

func TestCancelledContextStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Convert(ctx, FormatPDF, statementPDF(3), Options{})
	if e, ok := err.(*Error); !ok || e.Code != CodeTimeout {
		t.Fatalf("a cancelled context should stop extraction: %v", err)
	}
}
