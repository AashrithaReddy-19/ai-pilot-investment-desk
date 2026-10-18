package docparse

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// PDF extraction for text-based (digital) PDFs. It reads positioned text, groups it into lines and
// cells, finds the amount columns by alignment, keeps them consistent across pages and hands the grid to
// the same table builder spreadsheets use. A PDF with no readable text (a scan) is refused, not guessed.
//
// Limits worth knowing: reading is heuristic. It works on tables whose columns line up; it cannot read
// text drawn as pictures, and a page whose fonts lack a usable text mapping comes out garbled and is
// reported as unreadable. OCR is not available.

type pdfRun struct {
	x, y, w, fs float64
	s           string
}

type pdfCell struct {
	x0, x1 float64
	text   string
}

type pdfLine struct {
	page     int
	y, fs    float64
	cells    []pdfCell
	text     string
	tableish bool
	numeric  []bool // per cell: an amount (or a dash standing for one)
}

// parsePages reads "3-5,9" or "10-" into sorted page numbers.
func parsePages(spec string, total int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		all := make([]int, total)
		for i := range all {
			all[i] = i + 1
		}
		return all, nil
	}
	bad := func() error {
		return fail(CodeInvalidPages, "pages", "pages must look like 3-5,9 (the document has %d pages)", total)
	}
	set := map[int]bool{}
	parts := strings.Split(spec, ",")
	if len(parts) > 60 {
		return nil, bad()
	}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, bad()
		}
		lo, hi := 0, 0
		var err1, err2 error
		if i := strings.IndexByte(p, '-'); i >= 0 {
			lo, err1 = strconv.Atoi(strings.TrimSpace(p[:i]))
			if rest := strings.TrimSpace(p[i+1:]); rest == "" {
				hi = total
			} else {
				hi, err2 = strconv.Atoi(rest)
			}
		} else {
			lo, err1 = strconv.Atoi(p)
			hi, err2 = lo, err1
		}
		if err1 != nil || err2 != nil || lo < 1 || hi < lo || hi > total {
			return nil, bad()
		}
		for n := lo; n <= hi; n++ {
			set[n] = true
		}
	}
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

func convertPDF(ctx context.Context, b []byte, opt Options) (res *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, fail(CodeCorrupt, "file", "the PDF is damaged and could not be read; open it in a PDF viewer, print it to a new PDF and try again")
		}
	}()
	rd, rerr := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if rerr != nil {
		if rerr == pdf.ErrInvalidPassword {
			return nil, fail(CodeEncrypted, "file", "the PDF is password-protected; remove the password and upload it again")
		}
		return nil, fail(CodeCorrupt, "file", "the PDF is damaged or incomplete and could not be opened (%s); open it in a PDF viewer, print it to a new PDF and try again", shortErr(rerr))
	}
	total := rd.NumPage()
	if total <= 0 {
		return nil, fail(CodeCorrupt, "file", "the PDF has no pages")
	}
	pages, perr := parsePages(opt.Pages, total)
	if perr != nil {
		return nil, perr
	}
	if len(pages) > opt.maxPages() {
		return nil, fail(CodeTooManyPages, "pages", "the document has %d pages and at most %d can be read at once; choose the pages that hold the statement, for example pages=120-140", len(pages), opt.maxPages())
	}

	var lines []pdfLine
	pageLinesIdx := map[int][]int{} // page -> indexes into lines
	var noText []int
	garbled := 0
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fail(CodeTimeout, "", "reading the PDF took too long; choose fewer pages")
		}
		pl, ok, bad := readPDFPage(rd, p)
		if !ok {
			noText = append(noText, p)
			if bad {
				garbled++
			}
			continue
		}
		for _, l := range pl {
			pageLinesIdx[p] = append(pageLinesIdx[p], len(lines))
			lines = append(lines, l)
		}
	}
	if len(noText) == len(pages) {
		msg := "the PDF has no readable text: it looks like a scan (page images). OCR is not available on this server, so a scanned statement cannot be read; upload a text-based PDF, or the spreadsheet the figures came from"
		if garbled == len(pages) {
			msg = "the PDF's text could not be decoded (its fonts have no usable text mapping), so the figures cannot be read reliably; export the statement again, or upload the spreadsheet the figures came from"
		}
		return nil, fail(CodeScanned, "file", "%s", msg)
	}

	// classify lines and find the table pages
	for i := range lines {
		classifyLine(&lines[i])
	}
	// A page is a table page when it has three or more table rows (or any, when the reader chose the
	// pages). A short page right next to a table page is its continuation and is kept too: dropping it
	// would silently lose the last rows of a statement.
	explicit := strings.TrimSpace(opt.Pages) != ""
	counts := map[int]int{}
	include := map[int]bool{}
	for _, p := range pages {
		for _, i := range pageLinesIdx[p] {
			if lines[i].tableish || realAmount(lines[i]) { // an amount line counts even when its description wrapped onto other lines
				counts[p]++
			}
		}
		if counts[p] >= 3 || (explicit && counts[p] >= 1) {
			include[p] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for k, p := range pages {
			if include[p] || counts[p] == 0 {
				continue
			}
			if k > 0 && include[pages[k-1]] || k+1 < len(pages) && include[pages[k+1]] {
				include[p], changed = true, true
			}
		}
	}
	var tablePages []int
	for _, p := range pages {
		if include[p] {
			tablePages = append(tablePages, p)
		}
	}
	if len(tablePages) == 0 {
		return nil, fail(CodeNoTable, "pages", "text was read from %d page(s) but no table of descriptions and amounts was found; choose the pages that hold the statement, for example pages=120-124", len(pages)-len(noText))
	}

	// columns, page by page, then matched across pages
	type pageCols struct {
		centers []float64
		useLeft bool
	}
	pc := map[int]pageCols{}
	refPage := -1
	for _, p := range tablePages {
		var rights, lefts []float64
		for _, i := range pageLinesIdx[p] {
			l := lines[i]
			if !l.tableish && !realAmount(l) {
				continue
			}
			for k, c := range l.cells {
				if l.numeric[k] && !isDash(c.text) && (l.tableish || !yearLike(c.text)) {
					rights, lefts = append(rights, c.x1), append(lefts, c.x0)
				}
			}
		}
		rc, lc := clusterEdges(rights, 10), clusterEdges(lefts, 10)
		cols, useLeft := rc, false
		if len(lc) < len(rc) {
			cols, useLeft = lc, true
		}
		pc[p] = pageCols{centers: cols, useLeft: useLeft}
		if refPage < 0 || len(cols) > len(pc[refPage].centers) {
			refPage = p
		}
	}
	global := append([]float64{}, pc[refPage].centers...)
	colMap := map[int][]int{} // page -> cluster index -> global column
	var shifted []int
	for _, p := range tablePages {
		cs := pc[p].centers
		m := make([]int, len(cs))
		if len(cs) == len(pc[refPage].centers) {
			moved := false
			for k := range cs {
				m[k] = k // same number of columns: match them by order
				if math.Abs(cs[k]-global[k]) > 25 {
					moved = true
				}
			}
			if moved {
				shifted = append(shifted, p)
			}
		} else {
			shifted = append(shifted, p)
			for k, c := range cs {
				bi, bd := -1, 60.0
				for g, gc := range global {
					if d := math.Abs(c - gc); d < bd {
						bi, bd = g, d
					}
				}
				if bi < 0 {
					global = append(global, c)
					bi = len(global) - 1
				}
				m[k] = bi
			}
		}
		colMap[p] = m
	}
	width := 1 + len(global)

	var rows []gridRow
	var titles []string
	pageUnits := map[int]string{}
	var ambiguousUnitPages []int
	prevUnit := ""
	for _, p := range tablePages {
		cols := pc[p]
		edge := func(c pdfCell) float64 {
			if cols.useLeft {
				return c.x0
			}
			return c.x1
		}
		nearest := func(c pdfCell) (int, float64) {
			bi, bd := -1, math.MaxFloat64
			for k, cc := range cols.centers {
				if d := math.Abs(edge(c) - cc); d < bd {
					bi, bd = k, d
				}
			}
			return bi, bd
		}
		idxs := pageLinesIdx[p]
		firstTable := -1
		for n, i := range idxs {
			if lines[i].tableish || realAmount(lines[i]) {
				firstTable = n
				break
			}
		}
		// The table block runs from the first line that holds an amount to the last one, widened by the
		// wrapped description lines of the first and last rows (whose label and amounts may be separate
		// lines, so they are not "table rows" on their own). Titles and repeated headings above it on later
		// pages, and footers and notes below it, are not rows; the first page keeps everything above the
		// block because its heading names the columns.
		startKeep, endKeep := blockBounds(lines, idxs)
		unitSet := map[string]bool{}
		for n, i := range idxs {
			l := lines[i]
			if n < firstTable && len(titles) < 400 && n >= firstTable-10 {
				titles = append(titles, l.text)
			}
			if !l.tableish {
				for _, u := range unitsIn(l.text) {
					unitSet[u] = true
				}
			}
			switch {
			case n < startKeep && p != tablePages[0]:
				continue
			case n > endKeep:
				continue
			case !l.tableish && n >= startKeep && utf8.RuneCountInString(l.text) > 90:
				continue // narrative inside the table block
			}
			cells := make([]string, width)
			label := []string{}
			firstNum, lastNum := -1, -1
			for k := range l.cells {
				if l.numeric[k] {
					if firstNum < 0 {
						firstNum = k
					}
					lastNum = k
				}
			}
			for k, c := range l.cells {
				if len(cols.centers) == 0 {
					label = append(label, c.text)
					continue
				}
				// a data row: everything before its first amount is the description
				if l.tableish && k < firstNum {
					label = append(label, c.text)
					continue
				}
				ci, dist := nearest(c)
				switch {
				case l.tableish && l.numeric[k]:
					// an amount always belongs to the nearest column
				case l.tableish:
					// Text after the row's last amount is a trailing text column ("Source", "Remarks":
					// "[Assumption]", "Calculated"), not part of an amount: glued to one it would make the
					// figure unreadable. A bare suffix such as "Cr" stays with its amount.
					trailing := k > lastNum && !suffixRe.MatchString(strings.TrimSpace(c.text))
					if dist > 80 || trailing { // text to the right of the amounts that is not under a column
						label = append(label, c.text)
						continue
					}
				default: // headings and other text: only cells that sit over the amount columns go there
					if c.x0 < cols.centers[0]-90 || dist > 80 {
						label = append(label, c.text)
						continue
					}
				}
				col := 1 + colMap[p][ci]
				if cells[col] != "" {
					cells[col] += " "
				}
				cells[col] += c.text
			}
			cells[0] = strings.Join(strings.Fields(strings.Join(label, " ")), " ")
			rows = append(rows, gridRow{Src: Source{Page: p}, Cells: cells, y: l.y, fs: l.fs})
		}
		switch len(unitSet) {
		case 0:
			pageUnits[p] = prevUnit
		case 1:
			for u := range unitSet {
				pageUnits[p], prevUnit = u, u
			}
		default:
			ambiguousUnitPages = append(ambiguousUnitPages, p)
			pageUnits[p] = ""
		}
	}
	rows = mergeWrappedLabels(rows)

	t, berr := buildTable(rows, titles, opt)
	if berr != nil {
		return nil, berr
	}
	res, ferr := finish(FormatPDF, t, pageUnits)
	if ferr != nil {
		return nil, ferr
	}
	info := &PageInfo{Total: total, Selected: pages, WithTables: tablePages, NoText: noText, Units: pageUnits}
	res.Pages = info
	if len(noText) > 0 {
		res.Flags = append(res.Flags, Flag{Code: "PDF_PAGES_NO_TEXT", Message: fmt.Sprintf("%d page(s) have no readable text (scanned images, blank pages or unreadable fonts) and were skipped: %s. Amounts printed on them are not in this list.", len(noText), pageList(noText))})
	}
	if len(shifted) > 0 {
		res.Flags = append(res.Flags, Flag{Code: "PDF_COLUMNS_SHIFTED", Message: fmt.Sprintf("The amount columns sit in different places on page(s) %s, so their rows were matched by column order. Check those values against the original.", pageList(shifted))})
	}
	if len(ambiguousUnitPages) > 0 {
		res.Flags = append(res.Flags, Flag{Code: "UNIT_AMBIGUOUS", Message: fmt.Sprintf("Page(s) %s state more than one unit. No unit was applied to them: choose the unit that matches the figures, or narrow the pages.", pageList(ambiguousUnitPages))})
	}
	res.Notes = append(res.Notes, fmt.Sprintf("Read %d of %d page(s); tables were found on %s.", len(pages), total, pageList(tablePages)))
	return res, nil
}

func shortErr(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func pageList(ps []int) string {
	var parts []string
	for i := 0; i < len(ps) && i < 20; i++ {
		parts = append(parts, strconv.Itoa(ps[i]))
	}
	s := strings.Join(parts, ", ")
	if len(ps) > 20 {
		s += fmt.Sprintf(" and %d more", len(ps)-20)
	}
	return s
}

func isDash(s string) bool { return parseAmount(s).Blank }

// readPDFPage returns a page's lines. ok is false when the page has no usable text; bad reports text
// that was present but undecodable.
func readPDFPage(rd *pdf.Reader, p int) (lines []pdfLine, ok bool, bad bool) {
	defer func() {
		if recover() != nil {
			lines, ok, bad = nil, false, false
		}
	}()
	pg := rd.Page(p)
	if pg.V.IsNull() {
		return nil, false, false
	}
	var runs []pdfRun
	letters, printable, total := 0, 0, 0
	for _, t := range pg.Content().Text {
		s := strings.ReplaceAll(strings.ReplaceAll(t.S, "\n", " "), "\r", " ")
		if s == "" {
			continue
		}
		fs := t.FontSize
		if fs <= 0 {
			fs = 10
		}
		// Fonts the reader has no widths for (embedded CID fonts, common in exported statements) report
		// every glyph of one text string at the same x with width 0: put the string back together.
		if n := len(runs); n > 0 && t.W == 0 && runs[n-1].w == 0 && runs[n-1].x == t.X && runs[n-1].y == t.Y && runs[n-1].fs == fs {
			runs[n-1].s += s
		} else {
			runs = append(runs, pdfRun{x: t.X, y: t.Y, w: t.W, fs: fs, s: s})
		}
		for _, r := range s {
			total++
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				letters++
			}
			if unicode.IsPrint(r) && r != utf8.RuneError {
				printable++
			}
		}
	}
	if letters < 15 {
		return nil, false, total >= 15 && printable*10 < total*6
	}
	if printable*10 < total*6 {
		return nil, false, true
	}
	for i := range runs {
		if runs[i].w <= 0 {
			runs[i].w = estimateWidth(runs[i].s, runs[i].fs)
		}
	}
	return assembleLines(runs, p), true, false
}

// helvetica holds the Helvetica/Arial glyph widths (per 1000 em) for ASCII 32..126. A string's width is
// estimated from them when the PDF reader cannot supply real widths: close enough (a few percent) to find
// where a cell ends and whether a gap is a word space or a column gap.
var helvetica = [95]int{
	278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, // space ! " # $ % & ' ( ) * + , - . /
	556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556, // 0-9 : ; < = > ?
	1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, // @ A-O
	667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556, // P-Z [ backslash ] ^ _
	333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, // ` a-o
	556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584, // p-z { | } ~
}

func estimateWidth(s string, fs float64) float64 {
	w := 0
	for _, r := range s {
		switch {
		case r >= 32 && r <= 126:
			w += helvetica[r-32]
		case r >= 0x2E80: // CJK and other wide scripts
			w += 1000
		default: // rupee sign, accented letters and the like
			w += 556
		}
	}
	return float64(w) / 1000 * fs
}

func assembleLines(runs []pdfRun, page int) []pdfLine {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y > runs[j].y })
	var lines [][]pdfRun
	for _, r := range runs {
		n := len(lines)
		if n > 0 {
			ref := lines[n-1][0]
			if math.Abs(ref.y-r.y) <= 0.45*math.Max(math.Min(ref.fs, r.fs), 6) {
				lines[n-1] = append(lines[n-1], r)
				continue
			}
		}
		lines = append(lines, []pdfRun{r})
	}
	out := make([]pdfLine, 0, len(lines))
	for _, ln := range lines {
		sort.SliceStable(ln, func(i, j int) bool { return ln[i].x < ln[j].x })
		cells := lineCells(ln)
		if len(cells) == 0 {
			continue
		}
		texts := make([]string, len(cells))
		for i, c := range cells {
			texts[i] = c.text
		}
		out = append(out, pdfLine{page: page, y: ln[0].y, fs: ln[0].fs, cells: cells, text: strings.Join(texts, "  ")})
	}
	return out
}

// lineCells merges the text runs of one line into cells: runs closer than about a word space belong to
// the same cell, a wider gap starts a new one.
func lineCells(ln []pdfRun) []pdfCell {
	var cells []pdfCell
	var cur *pdfCell
	var end, fs float64
	flush := func() {
		if cur != nil {
			cur.text = strings.Join(strings.Fields(normSpaces(cur.text)), " ")
			if cur.text != "" {
				cur.x1 = end
				cells = append(cells, *cur)
			}
			cur = nil
		}
	}
	for _, r := range ln {
		width := r.w
		if width <= 0 {
			width = estimateWidth(r.s, r.fs)
		}
		if cur != nil {
			gap := r.x - end
			if gap > 0.9*math.Max(fs, r.fs) {
				flush()
			} else if gap > 0.15*r.fs && !strings.HasSuffix(cur.text, " ") && !strings.HasPrefix(r.s, " ") {
				cur.text += " "
			}
		}
		if cur == nil {
			cur = &pdfCell{x0: r.x}
		}
		cur.text += r.s
		if e := r.x + width; e > end || len(cur.text) == len(r.s) {
			end = e
		}
		fs = r.fs
	}
	flush()
	return cells
}

func classifyLine(l *pdfLine) {
	l.numeric = make([]bool, len(l.cells))
	firstLabel := -1
	for k, c := range l.cells {
		a := parseAmount(c.text)
		l.numeric[k] = a.OK
		if !a.OK && hasLetters(c.text, 2) && firstLabel < 0 {
			firstLabel = k
		}
	}
	if firstLabel < 0 {
		return
	}
	hasAmt := false
	for k := firstLabel + 1; k < len(l.cells); k++ {
		if l.numeric[k] && !isDash(l.cells[k].text) {
			hasAmt = true
		}
	}
	if !hasAmt {
		return
	}
	for _, c := range l.cells {
		if furnitureRe.MatchString(strings.TrimSpace(c.text)) {
			return
		}
	}
	// a repeated heading ("Particulars  Note  2026  2025") is not a data row
	if ri := analyse(cellTexts(l.cells)); ri.header {
		return
	}
	l.tableish = true
}

func cellTexts(cs []pdfCell) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.text
	}
	return out
}

// clusterEdges groups sorted edge positions that lie within tol of their neighbour. Weak clusters (stray
// numbers) are dropped unless they are all there is.
func clusterEdges(xs []float64, tol float64) []float64 {
	if len(xs) == 0 {
		return nil
	}
	sort.Float64s(xs)
	type cl struct {
		sum float64
		n   int
	}
	var cls []cl
	last := xs[0]
	cur := cl{xs[0], 1}
	for _, x := range xs[1:] {
		if x-last <= tol {
			cur.sum, cur.n = cur.sum+x, cur.n+1
		} else {
			cls = append(cls, cur)
			cur = cl{x, 1}
		}
		last = x
	}
	cls = append(cls, cur)
	maxN := 0
	for _, c := range cls {
		if c.n > maxN {
			maxN = c.n
		}
	}
	var out []float64
	for _, c := range cls {
		if c.n >= 2 || maxN < 2 || c.n*4 >= maxN {
			out = append(out, c.sum/float64(c.n))
		}
	}
	return out
}

// mergeWrappedLabels joins a description that wrapped onto a second line. Where the wrapped lines and
// the amounts sit is decided by geometry, because exporters differ:
//   - amounts centred between the two label lines (a table cell centred vertically): the amount-only line
//     sits within about a line of both, so the three lines are one row;
//   - amounts beside the first line, continuation underneath: a label-only line within about a line of the
//     row above is its continuation (and so is a lower-case one a little further down);
//   - amounts printed just under a label-only line: they belong to that label.
//
// Ordinary rows are spaced about two lines apart, so they are never merged.
func mergeWrappedLabels(rows []gridRow) []gridRow {
	lower := func(s string) bool {
		for _, r := range s {
			return unicode.IsLower(r)
		}
		return false
	}
	hasAmount := func(r gridRow) bool {
		for _, c := range r.Cells[1:] {
			if a := parseAmount(c); a.OK && !a.Blank {
				return true
			}
		}
		return false
	}
	label := func(r gridRow) string { return strings.TrimSpace(r.Cells[0]) }
	labelOnly := func(r gridRow) bool { return !hasAmount(r) && label(r) != "" && onlyLabel(r) }
	amountOnly := func(r gridRow) bool { return hasAmount(r) && label(r) == "" }
	// near: a sits above b by no more than k lines of text
	near := func(a, b gridRow, k float64) bool {
		d := a.y - b.y
		return a.Src.Page == b.Src.Page && d > 0 && a.fs > 0 && d <= k*math.Max(math.Max(a.fs, b.fs), 6)
	}
	var out []gridRow
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		if labelOnly(r) && i+2 < len(rows) && amountOnly(rows[i+1]) && labelOnly(rows[i+2]) && near(r, rows[i+1], 1.1) && near(rows[i+1], rows[i+2], 1.1) {
			m := rows[i+1]
			m.Cells = append([]string{}, m.Cells...)
			m.Cells[0] = strings.Join(strings.Fields(label(r)+" "+label(rows[i+2])), " ")
			m.Src = r.Src
			out = append(out, m)
			i += 2
			continue
		}
		if labelOnly(r) && i+1 < len(rows) && amountOnly(rows[i+1]) && near(r, rows[i+1], 1.7) {
			m := rows[i+1]
			m.Cells = append([]string{}, m.Cells...)
			m.Cells[0] = label(r)
			m.Src = r.Src
			out = append(out, m)
			i++
			continue
		}
		if labelOnly(r) && i+1 < len(rows) && rows[i+1].Src.Page == r.Src.Page && hasAmount(rows[i+1]) && label(rows[i+1]) != "" && lower(label(rows[i+1])) {
			rows[i+1].Cells[0] = strings.TrimSpace(label(r) + " " + label(rows[i+1]))
			continue
		}
		if n := len(out); n > 0 && labelOnly(r) && hasAmount(out[n-1]) && label(out[n-1]) != "" &&
			(near(out[n-1], r, 1.45) || (lower(label(r)) && near(out[n-1], r, 2.2))) {
			out[n-1].Cells = append([]string{}, out[n-1].Cells...)
			out[n-1].Cells[0] = strings.Join(strings.Fields(label(out[n-1])+" "+label(r)), " ")
			continue
		}
		out = append(out, r)
	}
	return out
}

func onlyLabel(r gridRow) bool {
	for _, c := range r.Cells[1:] {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// realAmount reports a line holding an actual amount: not a dash and not a bare year (a heading).
func realAmount(l pdfLine) bool {
	for k, c := range l.cells {
		if l.numeric[k] {
			if a := parseAmount(c.text); a.OK && !a.Blank && !yearLike(c.text) {
				return true
			}
		}
	}
	return false
}

func labelOnlyLine(l pdfLine) bool {
	has := false
	for k, c := range l.cells {
		if l.numeric[k] {
			return false
		}
		if strings.TrimSpace(c.text) != "" { // a wrapped tail such as "4)" has no letters at all
			has = true
		}
	}
	return has
}

// blockBounds returns the first and last line (indexes into idxs) that belong to the table block.
func blockBounds(lines []pdfLine, idxs []int) (start, end int) {
	first, last := -1, -1
	for n, i := range idxs {
		if realAmount(lines[i]) {
			if first < 0 {
				first = n
			}
			last = n
		}
	}
	if first < 0 {
		return 0, len(idxs) - 1
	}
	start, end = first, last
	space := func(l pdfLine) float64 { return math.Max(l.fs, 6) }
	// a wrapped first row: its description line sits just above the first amount line
	if first > 0 {
		above, cur := lines[idxs[first-1]], lines[idxs[first]]
		if labelOnlyLine(above) && above.y-cur.y <= 1.7*space(cur) {
			start = first - 1
		}
	}
	// a wrapped last row: description lines just below the last amount line
	for k := last + 1; k < len(idxs); k++ {
		cur, prev := lines[idxs[k]], lines[idxs[k-1]]
		r, _ := utf8.DecodeRuneInString(cur.text)
		gap := prev.y - cur.y
		if labelOnlyLine(cur) && (gap <= 1.45*space(cur) || unicode.IsLower(r) && gap <= 2.2*space(cur)) {
			end = k
			continue
		}
		break
	}
	return start, end
}
