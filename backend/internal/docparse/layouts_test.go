package docparse

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// Real PDFs (printed by Chromium, see testdata/README.md) and the layouts different exporters produce.
// These pin down defects that hand-built PDFs hid: wrapped descriptions that lose their amounts,
// headings that wrap over two lines, and rows at the very top or bottom of a page.

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRealPDFStatementSpanningThreePages(t *testing.T) {
	res := convert(t, FormatPDF, readTestdata(t, "real_statement_multipage.pdf"), Options{})
	assertRectangular(t, res)
	rs := records(t, res)
	if !reflect.DeepEqual(rs[0], []string{"Particulars", "31 March 2026 (INR lakh)", "31 March 2025 (INR lakh)"}) {
		t.Fatalf("a heading that wraps over two lines must be joined: %q", rs[0])
	}
	// 12 lines repeated 5 times: none may be lost, including descriptions that wrap and rows at page edges
	if len(rs) != 61 {
		t.Fatalf("want 60 rows, got %d", len(rs)-1)
	}
	byLabel := map[string][]string{}
	for _, r := range rs[1:] {
		byLabel[r[0]] = r[1:]
	}
	for _, c := range []struct{ label, cur, prev string }{
		{"Revenue from operations", "12,450.75", "11,200.00"},
		{"Software licences and subscriptions", "245.30", "210.00"},
		{"Software licences and subscriptions (segment 4)", "245.30", "210.00"},
		{"Advertising and business promotion (segment 4)", "140.00", "125.50"},
		{"AI platform subscription (LLM tokens) (segment 2)", "58.75", "12.50"},
		{"Professional and legal fees (segment 3)", "-120.00", "-95.00"},
	} {
		if got := byLabel[c.label]; len(got) != 2 || got[0] != c.cur || got[1] != c.prev {
			t.Errorf("%q: got %q, want [%s %s]", c.label, got, c.cur, c.prev)
		}
	}
	if res.Unit != unitLakh || !reflect.DeepEqual(res.Pages.WithTables, []int{1, 2, 3}) || res.Pages.Total != 3 {
		t.Fatalf("unit=%q pages=%+v", res.Unit, res.Pages)
	}
	if len(res.Dropped) != 1 || res.Dropped[0].Header != "Note" {
		t.Fatalf("dropped: %+v", res.Dropped)
	}
}

func TestRealPDFAnnualReportFindsTheStatementAmongNarrative(t *testing.T) {
	res := convert(t, FormatPDF, readTestdata(t, "real_annual_report.pdf"), Options{})
	assertRectangular(t, res)
	if res.Pages.Total != 16 || !reflect.DeepEqual(res.Pages.WithTables, []int{15}) || res.Unit != unitCrore || res.Rows != 12 {
		t.Fatalf("pages=%+v unit=%q rows=%d", res.Pages, res.Unit, res.Rows)
	}
	for _, l := range res.Lines[1:] {
		if l.Page != 15 {
			t.Fatalf("a row came from page %d, not the statement page: %+v", l.Page, l)
		}
	}
	rs := records(t, res)
	if rs[0][1] != "31 March 2026 (INR crore)" || rs[3][0] != "Employee benefit expense" || rs[3][1] != "4,320.50" {
		t.Fatalf("rows: %q", rs[:4])
	}
	// the same document, narrowed to the statement page, reads the same
	if again := convert(t, FormatPDF, readTestdata(t, "real_annual_report.pdf"), Options{Pages: "15"}); again.Rows != 12 {
		t.Fatalf("pages=15: %d rows", again.Rows)
	}
}

func TestRealPDFInDollarsIsRefused(t *testing.T) {
	e := wantErr(t, FormatPDF, readTestdata(t, "real_statement_usd.pdf"), Options{}, CodeCurrency)
	if !strings.Contains(e.Message, "USD") {
		t.Fatalf("name the currency: %q", e.Message)
	}
}

// centred lays out one table row whose description wraps onto two lines with the amounts centred between
// them, as HTML and Word table cells do: three separate text lines for one row.
func centred(y float64, first, second, note, cur, prev string) []tline {
	return []tline{
		{y: y + 6, cells: []tcell{{x: xLabel, text: first}}},
		{y: y, cells: []tcell{{x: xNote, right: true, text: note}, {x: xCur, right: true, text: cur}, {x: xPrev, right: true, text: prev}}},
		{y: y - 6, cells: []tcell{{x: xLabel, text: second}}},
	}
}

func plain(y float64, label, note, cur, prev string) tline {
	return tline{y: y, cells: []tcell{{x: xLabel, text: label}, {x: xNote, right: true, text: note}, {x: xCur, right: true, text: cur}, {x: xPrev, right: true, text: prev}}}
}

func TestPDFWrappedRowsAndHeadingsInEveryPosition(t *testing.T) {
	heading := []tline{ // a heading whose date wraps: "31 March" over "2026", the other words centred between
		{y: 715, cells: []tcell{{x: xCur, right: true, text: "31 March"}, {x: xPrev, right: true, text: "31 March"}}},
		{y: 708, cells: []tcell{{x: xLabel, text: "Particulars"}, {x: xNote, right: true, text: "Note"}}},
		{y: 701, cells: []tcell{{x: xCur, right: true, text: "2026"}, {x: xPrev, right: true, text: "2025"}}},
	}
	unit := tline{y: 740, cells: []tcell{{x: xLabel, text: "(INR in lakh)"}}}
	cat := func(parts ...[]tline) []tline {
		var out []tline
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	// page 1 starts and ends with a wrapped row; page 2 starts with one
	p1 := tpage{lines: cat([]tline{unit}, heading,
		centred(660, "Software licences and", "subscriptions (group)", "3", "245.30", "210.00"),
		[]tline{plain(630, "Cloud hosting charges", "4", "312.80", "280.40"), plain(605, "Rent and utilities", "5", "410.00", "400.00")},
		centred(570, "Advertising and business", "promotion (group)", "6", "140.00", "125.50"))}
	p2 := tpage{lines: cat([]tline{unit}, heading,
		centred(660, "AI platform subscription", "(LLM tokens)", "7", "58.75", "12.50"),
		[]tline{plain(630, "Professional fees", "8", "(120.00)", "(95.00)"), plain(605, "Depreciation", "9", "198.40", "187.10")})}
	for _, noWidths := range []bool{false, true} {
		res := convert(t, FormatPDF, buildPDF([]tpage{p1, p2}, pdfOpts{noWidths: noWidths}), Options{})
		assertRectangular(t, res)
		rs := records(t, res)
		want := [][]string{
			{"Software licences and subscriptions (group)", "245.30", "210.00"},
			{"Cloud hosting charges", "312.80", "280.40"},
			{"Rent and utilities", "410.00", "400.00"},
			{"Advertising and business promotion (group)", "140.00", "125.50"},
			{"AI platform subscription (LLM tokens)", "58.75", "12.50"},
			{"Professional fees", "-120.00", "-95.00"},
			{"Depreciation", "198.40", "187.10"},
		}
		if !reflect.DeepEqual(rs[1:], want) {
			t.Fatalf("font widths known=%v rows:\n got %q\nwant %q", !noWidths, rs[1:], want)
		}
		if !reflect.DeepEqual(rs[0], []string{"Particulars", "31 March 2026 (INR lakh)", "31 March 2025 (INR lakh)"}) {
			t.Fatalf("font widths known=%v heading: %q", !noWidths, rs[0])
		}
	}
}

func TestPDFOrdinaryRowsAreNotMergedTogether(t *testing.T) {
	// a label-only heading line between two normal rows must stay a heading, not glue to a neighbour
	lines := []tline{
		{y: 740, cells: []tcell{{x: xLabel, text: "Particulars"}, {x: xNote, right: true, text: "Note"}, {x: xCur, right: true, text: "31 March 2026"}, {x: xPrev, right: true, text: "31 March 2025"}}},
		plain(700, "Revenue", "1", "100.00", "90.00"),
		{y: 675, cells: []tcell{{x: xLabel, text: "Expenses"}}},
		plain(650, "Cloud hosting charges", "2", "312.80", "280.40"),
		plain(625, "Software licences", "3", "245.30", "210.00"),
		plain(600, "Rent", "4", "410.00", "400.00"),
	}
	res := convert(t, FormatPDF, buildPDF([]tpage{{lines: lines}}, pdfOpts{}), Options{})
	rs := records(t, res)
	got := map[string]string{}
	for _, r := range rs[1:] {
		got[r[0]] = r[1]
	}
	if got["Revenue"] != "100.00" || got["Cloud hosting charges"] != "312.80" || got["Software licences"] != "245.30" || got["Rent"] != "410.00" {
		t.Fatalf("every row keeps its own amounts: %q", rs)
	}
	if v, ok := got["Expenses"]; !ok || v != "" {
		t.Fatalf("a heading stays a heading with no amount: %q", rs)
	}
}
