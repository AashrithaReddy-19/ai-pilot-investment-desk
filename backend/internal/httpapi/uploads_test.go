package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeUploads struct {
	Uploads
	byHash  map[string]*model.Upload
	content map[string][]byte
	corr    []model.Correction
	status  string
	result  json.RawMessage
	kind    string
}

func TestSniffFormatChecksDocumentSignatures(t *testing.T) {
	var xlsx bytes.Buffer
	zw := zip.NewWriter(&xlsx)
	for _, name := range []string{"[Content_Types].xml"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte("ok"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var docx bytes.Buffer
	zw = zip.NewWriter(&docx)
	for _, name := range []string{"[Content_Types].xml", "word/document.xml"} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte("ok"))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, format string
		body         []byte
	}{
		{"statement.pdf", "pdf", []byte("%PDF-1.7")},
		{"statement.xls", "xls", []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}},
		{"statement.xlsx", "xlsx", xlsx.Bytes()},
		{"statement.docx", "docx", docx.Bytes()},
	}
	for _, tc := range tests {
		format, _, err := sniffFormat(tc.name, tc.body)
		if err != nil || format != tc.format {
			t.Errorf("%s: got format=%q err=%v", tc.name, format, err)
		}
	}
}

func (f *fakeUploads) CreateUpload(_ context.Context, in model.UploadIn) (*model.Upload, bool, error) {
	if u, ok := f.byHash[in.SHA256]; ok {
		return u, false, nil
	}
	u := &model.Upload{ID: "dddddddd-0000-0000-0000-000000000001", Kind: in.Kind, Filename: in.Filename, Format: in.Format,
		SizeBytes: len(in.Content), SHA256: in.SHA256, Status: "received", Options: in.Options}
	f.byHash[in.SHA256] = u
	f.content[u.ID] = in.Content
	return u, true, nil
}
func (f *fakeUploads) GetUpload(_ context.Context, id string) (*model.Upload, error) {
	if id != "dddddddd-0000-0000-0000-000000000001" {
		return nil, model.ErrNotFound
	}
	return &model.Upload{ID: id, Kind: f.kind, Status: f.status, Result: f.result}, nil
}
func (f *fakeUploads) UploadContent(_ context.Context, id string) (*model.UploadJob, error) {
	if id != "dddddddd-0000-0000-0000-000000000001" {
		return nil, model.ErrNotFound
	}
	return &model.UploadJob{ID: id, Kind: f.kind, Filename: `bs "q4".csv`, ContentType: "text/csv",
		Options: json.RawMessage(`{"unit":"lakh"}`), Content: []byte("Account,Amount\nAI software,12\n")}, nil
}
func (f *fakeUploads) ListCorrections(context.Context, string) ([]model.Correction, error) {
	return f.corr, nil
}
func (f *fakeUploads) AddCorrection(_ context.Context, in model.CorrectionIn) (*model.Correction, error) {
	c := model.Correction{ID: int64(len(f.corr) + 1), UploadID: in.UploadID, RowRef: in.RowRef, Field: in.Field,
		OriginalValue: in.OriginalValue, CorrectedValue: in.CorrectedValue}
	f.corr = append(f.corr, c)
	return &c, nil
}
func (f *fakeUploads) ConfirmUpload(_ context.Context, id string, confirmed json.RawMessage) (*model.Upload, error) {
	return &model.Upload{ID: id, Status: "confirmed", Confirmed: confirmed}, nil
}

func newUploadRouter(f *fakeRules, u *fakeUploads, notified *int) http.Handler {
	return NewRouter(Deps{Cfg: config.Config{RequestTimeout: 2e9, MaxBodyBytes: 1024, UploadMaxBytes: 4096},
		Store: fakeStore{}, Rules: f, Cache: fakeCache{}, Uploads: u, Notify: func() { *notified++ }, Log: discardLog()})
}

func multipartBody(t *testing.T, fields map[string]string, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if filename != "" {
		part, _ := w.CreateFormFile("file", filename)
		_, _ = part.Write(content)
	}
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func postMultipart(h http.Handler, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/uploads", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestUploadIsValidatedStoredAndDeduplicated(t *testing.T) {
	u := &fakeUploads{byHash: map[string]*model.Upload{}, content: map[string][]byte{}}
	n := 0
	r := newUploadRouter(&fakeRules{do: ok200(`{}`)}, u, &n)
	csv := []byte("Account,Amount\nAI software,12\n")
	b, ct := multipartBody(t, map[string]string{"kind": "balance_sheet", "unit": "lakh"}, "bs.csv", csv)
	w := postMultipart(r, b, ct)
	if w.Code != 202 || !contains(w.Body.String(), `"duplicate":false`) || n != 1 {
		t.Fatalf("first upload: %d %s notified=%d", w.Code, w.Body.String(), n)
	}

	if !contains(w.Body.String(), `"sha256":"`) || !contains(w.Body.String(), `"format":"csv"`) {
		t.Fatalf("upload must carry checksum and format: %s", w.Body.String())
	}
	b, ct = multipartBody(t, map[string]string{"kind": "balance_sheet"}, "again.csv", csv)
	w = postMultipart(r, b, ct)
	if w.Code != 200 || !contains(w.Body.String(), `"duplicate":true`) || n != 1 {
		t.Fatalf("same bytes again: %d %s", w.Code, w.Body.String())
	}
	cases := []struct {
		fields   map[string]string
		name     string
		content  []byte
		want     int
		contains string
	}{
		{map[string]string{"kind": "balance_sheet"}, "x.pdf", []byte("%PDF"), 415, "valid PDF signature"},
		{map[string]string{"kind": "balance_sheet"}, "x.xlsx", []byte("not a zip"), 415, "not an XLSX"},
		{map[string]string{"kind": "balance_sheet"}, "x.csv", []byte("a,b\x00c"), 415, "not UTF-8"},
		{map[string]string{"kind": "invoices"}, "x.csv", csv, 422, "kind must be"},
		{map[string]string{"kind": "pilots", "period": "weekly"}, "x.csv", csv, 422, "period"},
		{map[string]string{"kind": "balance_sheet"}, "", nil, 422, "multipart field"},
		{map[string]string{"kind": "balance_sheet"}, "big.csv", bytes.Repeat([]byte("a"), 5000), 413, "payload_too_large"},
	}
	for _, tc := range cases {
		b, ct := multipartBody(t, tc.fields, tc.name, tc.content)
		w := postMultipart(r, b, ct)
		if w.Code != tc.want || !contains(w.Body.String(), tc.contains) {
			t.Errorf("%s %v: want %d %q, got %d %s", tc.name, tc.fields, tc.want, tc.contains, w.Code, w.Body.String())
		}
	}

}

func TestUploadDownloadIsAnAttachment(t *testing.T) {
	n := 0
	r := newUploadRouter(&fakeRules{do: ok200(`{}`)}, &fakeUploads{kind: "balance_sheet"}, &n)
	w := do(r, "GET", "/api/v1/uploads/dddddddd-0000-0000-0000-000000000001/file", "")
	if w.Code != 200 || w.Header().Get("X-Content-Type-Options") != "nosniff" ||
		w.Header().Get("Content-Disposition") != `attachment; filename="bs _q4_.csv"` || !contains(w.Body.String(), "AI software") {
		t.Fatalf("download: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if w := do(r, "GET", "/api/v1/uploads/eeeeeeee-0000-0000-0000-000000000009/file", ""); w.Code != 404 {
		t.Fatalf("another workspace's upload must be 404, got %d", w.Code)
	}
}

func TestCorrectionsKeepOriginalsAndConfirmSendsThemToTheEngine(t *testing.T) {
	result := json.RawMessage(`{"candidates":[{"row":2,"label":"AI software","amount_inr":1200000},{"row":3,"label":"Cloud AI","amount_inr":350000}]}`)
	u := &fakeUploads{kind: "balance_sheet", status: "parsed", result: result}
	var engineBody, engineCT string
	f := &fakeRules{do: func(_, path string, body []byte) (*model.Upstream, error) {
		engineBody = string(body)
		return &model.Upstream{Status: 200, Body: []byte(`{"ai_burn_to_date":1500000}`)}, nil
	}}
	n := 0
	r := newUploadRouter(f, u, &n)
	base := "/api/v1/uploads/dddddddd-0000-0000-0000-000000000001"
	w := do(r, "POST", base+"/corrections", `{"row_ref":3,"field":"amount_inr","value":300000,"note":"invoice says 3 L"}`)
	if w.Code != 201 || !contains(w.Body.String(), `"original_value":350000`) || !contains(w.Body.String(), `"corrected_value":300000`) {
		t.Fatalf("correction: %d %s", w.Code, w.Body.String())
	}
	for body, want := range map[string]string{
		`{"row_ref":9,"field":"amount_inr","value":1}`:  "not a candidate row",
		`{"row_ref":2,"field":"label","value":"x"}`:     "amount_inr only",
		`{"row_ref":2,"field":"amount_inr","value":-5}`: "non-negative",
		`{"row_ref":2,"field":"amount_inr"}`:            "value is required",
	} {
		if w := do(r, "POST", base+"/corrections", body); w.Code != 422 || !contains(w.Body.String(), want) {
			t.Errorf("%s: want 422 %q, got %d %s", body, want, w.Code, w.Body.String())
		}
	}
	w = do(r, "GET", base, "")
	if !contains(w.Body.String(), `"corrected_amount_inr":300000`) || !contains(w.Body.String(), `"amount_inr":350000`) {
		t.Fatalf("review must show original beside corrected: %s", w.Body.String())
	}
	w = do(r, "POST", base+"/confirm", `{"rows":[2,3]}`)
	if w.Code != 200 || f.lastPath != "/balance-sheet/confirm" || !contains(w.Body.String(), "1500000") {
		t.Fatalf("confirm: %d %s", w.Code, w.Body.String())
	}
	engineCT = f.lastCT
	for _, want := range []string{`name="rows"`, "2,3", `name="overrides"`, `{"3":300000}`, `name="unit"`, "lakh", "AI software,12"} {
		if !strings.Contains(engineBody, want) {
			t.Errorf("engine confirm body lacks %q", want)
		}
	}
	if !contains(engineCT, "multipart/form-data") {
		t.Fatalf("confirm must post multipart: %s", engineCT)
	}
	if w := do(r, "POST", base+"/confirm", `{"rows":[]}`); w.Code != 422 {
		t.Fatalf("confirm without rows: %d", w.Code)
	}
	u.status = "processing"
	if w := do(r, "POST", base+"/confirm", `{"rows":[2]}`); w.Code != 409 {
		t.Fatalf("confirm before parsing: %d", w.Code)
	}
}

func TestPilotCorrectionsAreTypeChecked(t *testing.T) {
	result := json.RawMessage(`{"pilots":[{"id":"P1","task":"Invoices","setup_cost_inr":200000,"annual_benefit_inr":180000}],"extensions":[{"id":"P1","readiness":"low"}]}`)
	u := &fakeUploads{kind: "pilots", status: "parsed", result: result}
	n := 0
	r := newUploadRouter(&fakeRules{do: ok200(`{}`)}, u, &n)
	base := "/api/v1/uploads/dddddddd-0000-0000-0000-000000000001/corrections"
	if w := do(r, "POST", base, `{"row_ref":"P1","field":"readiness","value":"high"}`); w.Code != 201 || !contains(w.Body.String(), `"original_value":"low"`) {
		t.Fatalf("extension field correction: %d %s", w.Code, w.Body.String())
	}
	for body, want := range map[string]string{
		`{"row_ref":"P1","field":"adoption_pct","value":140}`: "0 to 100",
		`{"row_ref":"P1","field":"ramp_months","value":1.5}`:  "whole number",
		`{"row_ref":"P1","field":"benefit_type","value":"x"}`: "cashable",
		`{"row_ref":"P9","field":"task","value":"x"}`:         "not a parsed pilot id",
		`{"row_ref":"P1","field":"id","value":"P2"}`:          "cannot be corrected",
	} {
		if w := do(r, "POST", base, body); w.Code != 422 || !contains(w.Body.String(), want) {
			t.Errorf("%s: want %q, got %d %s", body, want, w.Code, w.Body.String())
		}
	}
}
