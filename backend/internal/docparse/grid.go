package docparse

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// gridRow is one row of cells from a spreadsheet or from a PDF's reconstructed table.
type gridRow struct {
	Src   Source
	Cells []string
	y, fs float64 // PDF rows only: where the line sat on its page, to recognise a wrapped description
}

type tableRow struct {
	src     Source
	label   string
	amounts []string
}

// table is a cleaned description-and-amounts table, before the CSV is written.
type table struct {
	labelHeader string
	headers     []string // the document's own heading for each amount column
	rows        []tableRow
	headerSrc   Source
	dropped     []DroppedColumn
	notes       []string
	flags       []Flag
	currencies  map[string]bool
	titles      []string // text above and around the table, scanned for a stated unit/currency
}

var (
	headerWordRe = regexp.MustCompile(`(?i)\b(particulars?|description|details?|items?|note|notes|schedule|amounts?|year|fy|period|ended|as at|as on|current|previous|prior)\b|\b(19|20)\d\d\b`)
	refColumnRe  = regexp.MustCompile(`(?i)^(notes?|notes?\s*no\.?s?|notes?\s*ref\.?|ref\.?|reference|s\.?\s*no\.?|sr\.?\s*no\.?|sl\.?\s*no\.?|sch(?:edule)?\.?(?:\s*no\.?)?)$`)
	furnitureRe  = regexp.MustCompile(`(?i)^(page\s+\d+(\s+of\s+\d+)?|continued|contd\.?|\(contd\.?\)|total pages?.*)$`)
)

type rowInfo struct {
	labelCol int // first cell with at least two letters; -1 when none
	amtCols  []int
	nonEmpty int
	header   bool
	data     bool
}

func analyse(cells []string) rowInfo {
	ri := rowInfo{labelCol: -1}
	hdrWord := false
	for j, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		ri.nonEmpty++
		if headerWordRe.MatchString(c) {
			hdrWord = true
		}
		if ri.labelCol < 0 && hasLetters(c, 2) {
			if a := parseAmount(c); !a.OK {
				ri.labelCol = j
			}
		}
	}
	allYear, anyAmt := true, false
	for j, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" || j <= ri.labelCol {
			continue
		}
		if a := parseAmount(c); a.OK && !a.Blank {
			ri.amtCols = append(ri.amtCols, j)
			anyAmt = true
			if !yearLike(c) {
				allYear = false
			}
		}
	}
	switch {
	case ri.nonEmpty < 2:
	case !anyAmt:
		ri.header = hdrWord && ri.labelCol >= 0 || hdrWord && ri.labelCol < 0
	case allYear:
		ri.header = hdrWord || ri.labelCol < 0
	}
	ri.data = ri.labelCol >= 0 && len(ri.amtCols) > 0 && !ri.header
	return ri
}

func rowText(cells []string) string {
	var parts []string
	for _, c := range cells {
		if c = strings.Join(strings.Fields(normSpaces(c)), " "); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " | ")
}

func colName(j int) string {
	name := ""
	for j++; j > 0; j = (j - 1) / 26 {
		name = string(rune('A'+(j-1)%26)) + name
	}
	return name
}

// buildTable finds the description column, the amount columns and the header in a grid of rows.
func buildTable(rows []gridRow, extraTitles []string, opt Options) (*table, error) {
	n := len(rows)
	info := make([]rowInfo, n)
	lastData := -1
	for i, r := range rows {
		info[i] = analyse(r.Cells)
		if info[i].data {
			lastData = i
		}
	}
	first := -1
	for i := 0; i < n && first < 0; i++ {
		if !info[i].data {
			continue
		}
		if i == lastData {
			first = i
			break
		}
		for j := i + 1; j < n && j <= i+4; j++ {
			if info[j].data {
				first = i
				break
			}
		}
	}
	if first < 0 {
		return nil, fail(CodeNoTable, "", "no table of descriptions and amounts was found; check that the right sheet or pages were read")
	}

	// header: the nearest non-data row above the first data row that has two or more cells
	hdr := -1
	for i := first - 1; i >= 0 && i >= first-15; i-- {
		if !info[i].data && info[i].nonEmpty >= 2 {
			hdr = i
			break
		}
	}
	t := &table{currencies: map[string]bool{}, titles: append([]string{}, extraTitles...)}
	for i := 0; i < first; i++ {
		if s := rowText(rows[i].Cells); s != "" {
			t.titles = append(t.titles, s)
		}
	}
	t.headerSrc = rows[first].Src
	if hdr >= 0 {
		t.headerSrc = rows[hdr].Src
	}

	// column statistics over the data rows
	alpha, numish, num, nonEmpty := map[int]int{}, map[int]int{}, map[int]int{}, map[int]int{}
	width := 0
	for i := first; i < n; i++ {
		if !info[i].data {
			continue
		}
		for j, c := range rows[i].Cells {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			if j+1 > width {
				width = j + 1
			}
			nonEmpty[j]++
			if a := parseAmount(c); a.OK {
				numish[j]++
				if !a.Blank {
					num[j]++
				}
			} else if hasLetters(c, 2) {
				alpha[j]++
			}
		}
	}
	label := info[first].labelCol
	best := -1
	for j := 0; j < width; j++ {
		if alpha[j] > best {
			best, label = alpha[j], j
		}
	}
	cellAt := func(i, j int) string {
		if i < 0 || i >= n || j >= len(rows[i].Cells) {
			return ""
		}
		return strings.TrimSpace(rows[i].Cells[j])
	}
	// A column heading is often several stacked lines ("31 March" over "2026"): join what the heading
	// block above the table holds for that column, top to bottom. Only rows that are not data and that
	// sit within three rows of the nearest heading row count.
	headerOf := func(j int) string {
		var parts []string
		for i := hdr - 2; i <= hdr; i++ {
			if i < 0 || i >= n || info[i].data {
				continue
			}
			if c := strings.Join(strings.Fields(cellAt(i, j)), " "); c != "" && (len(parts) == 0 || !strings.EqualFold(parts[len(parts)-1], c)) {
				parts = append(parts, c)
			}
		}
		return strings.Join(parts, " ")
	}
	// the description column's heading is the nearest non-empty cell above the data, not a joined block
	// (the lines above it may be the document's title)
	t.labelHeader = ""
	for i := hdr; i >= 0 && i >= hdr-3 && hdr >= 0; i-- {
		if c := strings.Join(strings.Fields(cellAt(i, label)), " "); c != "" && !info[i].data {
			t.labelHeader = c
			break
		}
	}
	if t.labelHeader == "" || parseAmount(t.labelHeader).OK || len([]rune(t.labelHeader)) > 40 {
		t.labelHeader = "Particulars"
	}

	var keep []int
	for j := label + 1; j < width; j++ {
		if num[j] < 1 || numish[j]*2 < nonEmpty[j] {
			continue
		}
		h := headerOf(j)
		if refColumnRe.MatchString(h) {
			t.dropped = append(t.dropped, DroppedColumn{Header: h, Reason: "reference column (note numbers are not amounts)"})
			continue
		}
		keep = append(keep, j)
	}
	if len(keep) == 0 {
		return nil, fail(CodeNoTable, "", "descriptions were found but no column of amounts; check that the right sheet or pages were read")
	}
	if len(keep) > 12 {
		t.dropped = append(t.dropped, DroppedColumn{Header: fmt.Sprintf("%d further columns", len(keep)-12), Reason: "only the first 12 amount columns are kept"})
		keep = keep[:12]
	}
	for k, j := range keep {
		h := headerOf(j)
		if h == "" {
			h = fmt.Sprintf("Column %s", colName(j))
			if rows[first].Src.Page > 0 {
				h = fmt.Sprintf("Amount %d", k+1)
			}
		}
		t.headers = append(t.headers, h)
	}

	repeated, noLabel := 0, 0
	var crdr []string
	for i := first; i < n; i++ {
		r := rows[i]
		if info[i].header {
			repeated++
			continue
		}
		lab := strings.Join(strings.Fields(normSpaces(cellAt(i, label))), " ")
		anyAmt := false
		amounts := make([]string, len(keep))
		for k, j := range keep {
			raw := cellAt(i, j)
			if raw == "" {
				continue
			}
			anyAmt = true
			a := parseAmount(raw)
			if a.OK && a.Currency != "" {
				t.currencies[a.Currency] = true
			}
			if a.OK && (strings.EqualFold(a.Suffix, "cr") || strings.EqualFold(a.Suffix, "dr")) && len(crdr) < 8 {
				crdr = append(crdr, fmt.Sprintf("%s (%s)", lab, strings.TrimSpace(raw)))
			}
			amounts[k] = cleanAmountText(raw, a)
		}
		if lab == "" {
			if anyAmt {
				noLabel++
			}
			continue
		}
		if furnitureRe.MatchString(lab) {
			continue
		}
		src := r.Src
		src.Text = rowText(r.Cells)
		t.rows = append(t.rows, tableRow{src: src, label: lab, amounts: amounts})
		if len(t.rows) > opt.maxRows() {
			return nil, fail(CodeTooLarge, "", "the table has more than %d rows; narrow the pages or sheet and try again", opt.maxRows())
		}
	}
	if len(t.rows) == 0 {
		return nil, fail(CodeNoTable, "", "no usable rows were found in the table")
	}
	if repeated > 0 {
		t.notes = append(t.notes, fmt.Sprintf("%d repeated heading row(s) were skipped", repeated))
	}
	if noLabel > 0 {
		t.notes = append(t.notes, fmt.Sprintf("%d row(s) with an amount but no description were left out", noLabel))
	}
	if len(crdr) > 0 {
		t.flags = append(t.flags, Flag{Code: "AMOUNT_SUFFIX_CR_DR", Message: "Some amounts end in Cr or Dr, which the parser reads as crore: " + strings.Join(crdr, "; ") + ". If these mean credit or debit, correct them before confirming."})
	}
	return t, nil
}

// finish decides currency and unit, then writes the CSV the engine reads.
func finish(format string, t *table, pageUnits map[int]string) (*Result, error) {
	res := &Result{Format: format, Rows: len(t.rows), Notes: t.notes, Flags: t.flags, Dropped: t.dropped}

	// currency: INR is the only currency the desk works in
	seen := map[string]bool{}
	for c := range t.currencies {
		seen[c] = true
	}
	for _, s := range append(append([]string{}, t.titles...), t.headers...) {
		if c := currencyIn(s); c != "" {
			seen[c] = true
		}
	}
	var foreign []string
	for c := range seen {
		if c != "INR" {
			foreign = append(foreign, c)
		}
	}
	sort.Strings(foreign)
	if len(foreign) > 0 {
		return nil, fail(CodeCurrency, "", "the document states amounts in %s; the desk works in rupees only. Convert the figures to INR and upload again", strings.Join(foreign, ", "))
	}
	if seen["INR"] {
		res.Currency = "INR"
	}

	// unit: what the document states, from its titles and headings (per page for a PDF)
	units := map[string]string{} // unit -> where it was stated
	for _, s := range t.titles {
		for _, u := range unitsIn(s) {
			if _, ok := units[u]; !ok {
				units[u] = "title: " + trimTo(s, 60)
			}
		}
	}
	for _, h := range t.headers {
		for _, u := range unitsIn(h) {
			if _, ok := units[u]; !ok {
				units[u] = "column heading: " + trimTo(h, 60)
			}
		}
	}
	for p, u := range pageUnits {
		if u != "" {
			if _, ok := units[u]; !ok {
				units[u] = fmt.Sprintf("page %d", p)
			}
		}
	}
	if units[unitMillions] != "" || units[unitBillions] != "" {
		which := unitMillions
		if units[unitMillions] == "" {
			which = unitBillions
		}
		return nil, fail(CodeUnit, "", "the document states amounts in %s, which the desk does not support; convert to rupees, thousands, lakh or crore and upload again", which)
	}
	delete(units, unitMillions)
	delete(units, unitBillions)
	switch len(units) {
	case 0:
	case 1:
		for u, src := range units {
			res.Unit, res.UnitSource = u, src
		}
	default:
		var names []string
		for u := range units {
			names = append(names, u)
		}
		sort.Strings(names)
		res.Flags = append(res.Flags, Flag{Code: "UNIT_AMBIGUOUS", Message: "The document states more than one unit (" + strings.Join(names, ", ") + "). No unit was applied: choose the unit that matches the figures you are reviewing, or narrow the pages or sheet."})
	}

	headers := make([]string, len(t.headers))
	for i, h := range t.headers {
		if res.Unit != "" && !hasUnitWord(h) {
			u := res.Unit
			if u == unitRupees {
				u = "rupees"
			}
			h = fmt.Sprintf("%s (INR %s)", h, u)
		}
		headers[i] = h
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write(append([]string{t.labelHeader}, headers...))
	res.Lines = append(res.Lines, t.headerSrc)
	for _, r := range t.rows {
		_ = w.Write(append([]string{r.label}, r.amounts...))
		res.Lines = append(res.Lines, r.src)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fail(CodeCorrupt, "", "the converted table could not be written: %v", err)
	}
	res.CSV = buf.Bytes()
	return res, nil
}

func trimTo(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
