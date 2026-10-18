package aiassist

import (
	"context"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	txtypes "github.com/aws/aws-sdk-go-v2/service/textract/types"

	"c23desk/internal/docparse"
)

// Text as the ICICI Bank 2025-26 annual report prints it (page 215), with the rupee sign read as "`".
const softwarePage = "The following table sets forth, for the periods indicated, the movement in software acquired by the Bank, as included\n" +
	"in fixed assets.\n` in million\nParticulars | At | At\nMarch 31, 2026 | March 31, 2025\n" +
	"Gross Block at March 31 of preceding year | 40,875.1 | 36,834.5\nAdditions during the year | 9,663.3 | 6,196.8\n" +
	"Deductions during the year | (404.2) | (2,156.2)\nNet Block | 14,338.0 | 11,106.5\n"

const opexPage = "SCHEDULE 16 - OPERATING EXPENSES\n` in ‘000s\nYear ended | Year ended\n31.03.2026 | 31.03.2025\n" +
	"Repairs and maintenance | 38,165,670 | 31,705,041\nOther expenditure | 131,797,440 | 112,605,789\n" +
	"Software and cloud subscriptions | 2,415,160 | 2,824,293\n"

type fakeFinder struct {
	props []Proposal
	err   error
	calls int
	seen  []docparse.PageText
}

func (f *fakeFinder) Find(_ context.Context, pages []docparse.PageText) ([]Proposal, error) {
	f.calls++
	f.seen = pages
	return f.props, f.err
}
func (f *fakeFinder) Model() string { return "fake-model" }

func TestVerifyKeepsOnlyWhatThePageSays(t *testing.T) {
	pages := []docparse.PageText{{Page: 215, Text: softwarePage}, {Page: 158, Text: opexPage}}
	props := []Proposal{
		{Page: 215, Table: "Movement in software", Label: "Additions during the year", Amount: "9,663.3", Unit: "millions", Currency: "INR", Category: "software", Why: "software bought"},
		{Page: 215, Label: "Additions during the year", Amount: "9,999.9", Unit: "millions"},                                // invented amount
		{Page: 215, Label: "Generative AI platform fees", Amount: "9,663.3", Unit: "millions"},                              // invented line
		{Page: 999, Label: "Additions during the year", Amount: "9,663.3", Unit: "millions"},                                // page never read
		{Page: 158, Label: "Software and cloud subscriptions", Amount: "2,415,160", Unit: "millions"},                       // unit wrong: the page says '000s
		{Page: 215, Label: "Deductions during the year", Amount: "(404.2)", Unit: "millions", Currency: "USD"},              // not rupees
		{Page: 215, Table: "Movement in software", Label: "Additions during the year", Amount: "9,663.3", Unit: "millions"}, // duplicate
	}
	rows, dropped, notes := Verify(props, pages)
	if len(rows) != 2 {
		t.Fatalf("kept %d rows, want 2: %+v (dropped %+v)", len(rows), rows, dropped)
	}
	if rows[0].Unit != "millions" || rows[0].Number != "9,663.3" {
		t.Fatalf("first row: %+v", rows[0])
	}
	if rows[1].Unit != "thousands" {
		t.Fatalf("the page states '000s, so its unit wins over the model's: %+v", rows[1])
	}
	if len(dropped) != 4 || len(notes) != 1 {
		t.Fatalf("dropped %+v notes %q", dropped, notes)
	}
	for _, d := range dropped {
		if d.Reason == "" {
			t.Fatalf("every dropped proposal says why: %+v", d)
		}
	}
}

func TestBuildResultIsEngineReadyCSVWithSourcesAndReasons(t *testing.T) {
	pages := []docparse.PageText{{Page: 215, Text: softwarePage}}
	rows, _, _ := Verify([]Proposal{
		{Page: 215, Table: "Movement in software", Label: "Additions during the year", Column: "At March 31, 2026", Amount: "9,663.3", Unit: "millions", Category: "software", Why: "software bought"},
		{Page: 215, Table: "Movement in software", Label: "Deductions during the year", Amount: "(404.2)", Unit: "millions", Category: "software"},
	}, pages)
	res := buildResult(docparse.FormatPDF, rows, &Info{Method: "document text + fake-model", PagesSent: []int{215}})
	rs, err := csv.NewReader(strings.NewReader(string(res.CSV))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"Particulars", "Amount as printed"},
		{"Additions during the year (Movement in software)", "9,663.3 mn"},
		{"Deductions during the year (Movement in software)", "-404.2 mn"},
	}
	if len(rs) != len(want) {
		t.Fatalf("csv: %q", rs)
	}
	for i := range want {
		if strings.Join(rs[i], "|") != strings.Join(want[i], "|") {
			t.Fatalf("csv line %d: %q, want %q", i+1, rs[i], want[i])
		}
	}
	if len(res.Lines) != 3 || res.Lines[1].Page != 215 || len(res.Reasons) != 3 || !strings.Contains(res.Reasons[1], "software bought") {
		t.Fatalf("lines %+v reasons %q", res.Lines, res.Reasons)
	}
	if res.Flags[0].Code != "AI_READER_PROPOSED" || !strings.Contains(res.Flags[0].Message, "page 215") {
		t.Fatalf("flags: %+v", res.Flags)
	}
}

func TestSelectPagesPrefersTechnologyTables(t *testing.T) {
	pages := []docparse.PageText{
		{Page: 1, Text: "Our investments in technology, including Gen AI, drive growth."},   // no amounts
		{Page: 2, Text: "Rent | 19,620,358 | 17,388,438\nPrinting | 2,415,160 | 2,824,293"}, // no technology
		{Page: 215, Text: softwarePage},                                      // both
		{Page: 158, Text: opexPage},                                          // both
		{Page: 300, Text: strings.Repeat("software 1,234.5 6,789.0 ", 5000)}, // over the budget
	}
	got := SelectPages(pages, 8, 20_000)
	if len(got) != 2 || got[0].Page != 158 || got[1].Page != 215 {
		t.Fatalf("pages: %+v", pageNums(got))
	}
	if one := SelectPages(pages, 1, 20_000); len(one) != 1 {
		t.Fatalf("page budget: %+v", pageNums(one))
	}
}

func pageNums(ps []docparse.PageText) []int {
	var out []int
	for _, p := range ps {
		out = append(out, p.Page)
	}
	return out
}

func TestReadCachesForConfirmAndExplainsFailures(t *testing.T) {
	csvDoc := []byte("Particulars,Amount\nGross Block,\"40,875.1\"\nAdditions during the year (software),\"9,663.3\"\nTechnology platform licences in million,\"1,200.5\"\n")
	f := &fakeFinder{props: []Proposal{{Page: 1, Label: "Additions during the year (software)", Amount: "9,663.3", Unit: "millions", Category: "software"}}}
	r := New(f, nil, Config{})
	clock := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return clock }
	q := Request{Format: docparse.FormatCSV, Doc: csvDoc, Trigger: TriggerNoCandidates}

	res, inf, err := r.Read(context.Background(), q)
	if err != nil || res == nil || !inf.Used || inf.Kept != 1 || inf.Trigger != TriggerNoCandidates {
		t.Fatalf("read: %v %+v", err, inf)
	}
	if _, inf2, err := r.Read(context.Background(), q); err != nil || !inf2.Cached || f.calls != 1 {
		t.Fatalf("the second read of the same file must come from the cache: %v %+v calls=%d", err, inf2, f.calls)
	}
	if _, _, ok := r.Cached(Key(docparse.FormatCSV, csvDoc, docparse.Options{})); !ok {
		t.Fatal("confirm finds the reading by key")
	}
	clock = clock.Add(61 * time.Minute)
	if _, _, ok := r.Cached(Key(docparse.FormatCSV, csvDoc, docparse.Options{})); ok {
		t.Fatal("a reading expires after an hour")
	}

	// the model fails
	r2 := New(&fakeFinder{err: errors.New("boom")}, nil, Config{})
	if _, inf, err := r2.Read(context.Background(), q); !isCode(err, CodeFailed) || inf.Used || !strings.Contains(inf.Error, "boom") {
		t.Fatalf("failure: %v %+v", err, inf)
	}
	// nothing relevant: the model is not even asked
	f3 := &fakeFinder{}
	r3 := New(f3, nil, Config{})
	if _, _, err := r3.Read(context.Background(), Request{Format: docparse.FormatCSV, Doc: []byte("Particulars,Amount\nRent,\"1,000\"\nSalaries,\"2,000\"\n")}); !isCode(err, CodeNothing) || f3.calls != 0 {
		t.Fatalf("nothing relevant: %v calls=%d", err, f3.calls)
	}
	// a scan without OCR configured
	if _, _, err := r3.Read(context.Background(), Request{Format: docparse.FormatPDF, Doc: []byte("%PDF-1.4"), Scanned: true}); !isCode(err, CodeUnavailable) {
		t.Fatalf("no OCR: %v", err)
	}
}

func isCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

func TestDecodeRowsToleratesStringPages(t *testing.T) {
	got, err := decodeRows([]byte(`{"rows":[{"page":"215","label":"Additions during the year","amount":"9,663.3","unit":"millions","table":"","column":"","currency":"INR","category":"software","why":"x"}]}`))
	if err != nil || len(got) != 1 || got[0].Page != 215 || got[0].Unit != "millions" {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := decodeRows([]byte(`not json`)); err == nil {
		t.Fatal("bad JSON is an error")
	}
}

func TestPagesFromTextractBlocks(t *testing.T) {
	id := func(s string) *string { return aws.String(s) }
	p := aws.Int32(3)
	child := func(ids ...string) []txtypes.Relationship {
		return []txtypes.Relationship{{Type: txtypes.RelationshipTypeChild, Ids: ids}}
	}
	cell := func(cid string, r, c int32, wid string) txtypes.Block {
		return txtypes.Block{Id: id(cid), BlockType: txtypes.BlockTypeCell, RowIndex: aws.Int32(r), ColumnIndex: aws.Int32(c), Page: p, Relationships: child(wid)}
	}
	word := func(wid, text string) txtypes.Block {
		return txtypes.Block{Id: id(wid), BlockType: txtypes.BlockTypeWord, Text: id(text), Page: p}
	}
	blocks := []txtypes.Block{
		{Id: id("l1"), BlockType: txtypes.BlockTypeLine, Text: id("Schedule 16 (` in '000s)"), Page: p, Relationships: child("w0")},
		word("w0", "Schedule"),
		{Id: id("t1"), BlockType: txtypes.BlockTypeTable, Page: p, Relationships: child("c1", "c2")},
		cell("c1", 1, 1, "w1"), cell("c2", 1, 2, "w2"),
		word("w1", "Software"), word("w2", "2,415,160"),
		{Id: id("l2"), BlockType: txtypes.BlockTypeLine, Text: id("Software 2,415,160"), Page: p, Relationships: child("w1", "w2")},
	}
	got := PagesFromBlocks(blocks)
	if len(got) != 1 || got[0].Page != 3 {
		t.Fatalf("pages: %+v", got)
	}
	if got[0].Text != "Schedule 16 (` in '000s)\nSoftware | 2,415,160" {
		t.Fatalf("text: %q", got[0].Text)
	}
}
