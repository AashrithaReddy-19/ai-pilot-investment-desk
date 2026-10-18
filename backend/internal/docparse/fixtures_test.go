package docparse

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
)

// Builders for the synthetic documents the tests read: PDFs written by hand (so there is no PDF library
// in the test path), XLSX workbooks written as the ZIP of XML parts Excel produces, and a few damaged
// or hostile variants. Everything here is made-up data.

// ---------------- PDF ----------------

type tcell struct {
	x     float64 // left edge, or right edge when right is set
	right bool
	text  string
}

type tline struct {
	y     float64
	cells []tcell
}

type tpage struct {
	lines  []tline
	nowrap bool // reserved
}

// helvWidth approximates Helvetica glyph widths (per 1000 em). The same numbers are written into the
// font's /Widths so a reader reports real text widths.
func helvWidth(r byte) int {
	switch {
	case r == ' ' || r == '.' || r == ',' || r == '/' || r == ':' || r == ';' || r == '!' || r == '|' || r == 'I' || r == 'f' || r == 't' || r == '\'':
		return 278
	case r == 'i' || r == 'j' || r == 'l':
		return 222
	case r == '(' || r == ')' || r == '-' || r == 'r' || r == '[' || r == ']':
		return 333
	case r == 'm':
		return 833
	case r == 'w' || r == 'W' || r == 'M':
		return 778
	case r >= 'A' && r <= 'Z':
		return 667
	case r >= '0' && r <= '9':
		return 556
	case r >= 'a' && r <= 'z':
		return 500
	}
	return 556
}

func textWidth(s string, fs float64) float64 {
	w := 0
	for i := 0; i < len(s); i++ {
		w += helvWidth(s[i])
	}
	return float64(w) / 1000 * fs
}

func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	return strings.ReplaceAll(s, ")", "\\)")
}

type pdfOpts struct {
	noWidths bool
	encrypt  bool
}

func buildPDF(pages []tpage, o pdfOpts) []byte {
	const fs = 10.0
	var objs [][]byte
	add := func(b []byte) int { objs = append(objs, b); return len(objs) }
	add(nil) // 1 catalog
	add(nil) // 2 pages
	var widths []string
	for c := 32; c <= 126; c++ {
		widths = append(widths, fmt.Sprint(helvWidth(byte(c))))
	}
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding"
	if !o.noWidths {
		font += " /FirstChar 32 /LastChar 126 /Widths [" + strings.Join(widths, " ") + "]"
	}
	fontID := add([]byte(font + " >>"))
	var kids []string
	for _, pg := range pages {
		var cs strings.Builder
		for _, ln := range pg.lines {
			for _, c := range ln.cells {
				x := c.x
				if c.right {
					x = c.x - textWidth(c.text, fs)
				}
				fmt.Fprintf(&cs, "BT /F1 %g Tf %.2f %.2f Td (%s) Tj ET\n", fs, x, ln.y, pdfEscape(c.text))
			}
		}
		stream := cs.String()
		cid := add([]byte(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)))
		pid := add([]byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, cid)))
		kids = append(kids, fmt.Sprintf("%d 0 R", pid))
	}
	objs[0] = []byte("<< /Type /Catalog /Pages 2 0 R >>")
	objs[1] = []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	return pdfFile(objs, o.encrypt)
}

func pdfFile(objs [][]byte, encrypt bool) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	extra := ""
	if encrypt { // a standard-security handler whose password check cannot succeed with an empty password
		extra = " /ID [(IIIIIIIIIIIIIIII)] /Encrypt << /Filter /Standard /V 1 /R 2 /Length 40 /O (" + strings.Repeat("O", 32) + ") /U (" + strings.Repeat("U", 32) + ") /P 0 >>"
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, extra, xref)
	return b.Bytes()
}

// imagePDF is a one-page PDF that only draws a picture: no text at all, like a scan.
func imagePDF() []byte {
	img := bytes.Repeat([]byte{0xFF}, 40*40)
	objs := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Im0 4 0 R >> >> /Contents 5 0 R >>"),
		append([]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 40 /Height 40 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n", len(img))), append(img, []byte("\nendstream")...)...),
		[]byte("<< /Length 31 >>\nstream\nq 200 0 0 200 0 0 cm /Im0 Do Q\nendstream"),
	}
	return pdfFile(objs, false)
}

// Layout used by the statement pages: description at the left, a note column and two amount columns,
// all numbers right-aligned.
const (
	xLabel = 50.0
	xNote  = 330.0
	xCur   = 440.0
	xPrev  = 530.0
)

func statementPage(unitLine string, rows [][]string, shift float64, pageNo, pages int, withHeader bool, title string) tpage {
	var ls []tline
	y := 790.0
	if title != "" {
		ls = append(ls, tline{y: y, cells: []tcell{{x: xLabel, text: title}}})
		y -= 22
	}
	if unitLine != "" {
		ls = append(ls, tline{y: y, cells: []tcell{{x: xLabel, text: unitLine}}})
		y -= 22
	}
	if withHeader {
		ls = append(ls, tline{y: y, cells: []tcell{
			{x: xLabel, text: "Particulars"}, {x: xNote + shift, right: true, text: "Note"},
			{x: xCur + shift, right: true, text: "31 March 2026"}, {x: xPrev + shift, right: true, text: "31 March 2025"}}})
		y -= 18
	}
	for _, r := range rows {
		cells := []tcell{{x: xLabel, text: r[0]}}
		if len(r) > 1 && r[1] != "" {
			cells = append(cells, tcell{x: xNote + shift, right: true, text: r[1]})
		}
		if len(r) > 2 && r[2] != "" {
			cells = append(cells, tcell{x: xCur + shift, right: true, text: r[2]})
		}
		if len(r) > 3 && r[3] != "" {
			cells = append(cells, tcell{x: xPrev + shift, right: true, text: r[3]})
		}
		ls = append(ls, tline{y: y, cells: cells})
		y -= 18
	}
	ls = append(ls, tline{y: 60, cells: []tcell{{x: 280, text: fmt.Sprintf("Page %d of %d", pageNo, pages)}}})
	return tpage{lines: ls}
}

// statementRows is the synthetic statement every format test uses.
var statementRows = [][]string{
	{"Revenue from operations", "1", "12,450.75", "11,200.00"},
	{"Employee benefit expense", "2", "4,320.50", "3,980.25"},
	{"Software licences and subscriptions", "3", "245.30", "210.00"},
	{"Cloud hosting charges", "4", "312.80", "280.40"},
	{"AI platform subscription (LLM tokens)", "5", "58.75", "12.50"},
	{"Rent and utilities", "6", "410.00", "400.00"},
	{"Professional fees", "7", "(120.00)", "(95.00)"},
	{"Depreciation and amortisation", "8", "198.40", "187.10"},
}

// ---------------- XLSX ----------------

type numTxt string // a number cell, kept as the exact text the workbook stores
type dateSerial float64
type formulaNoValue struct{}

type xsheet struct {
	name   string
	hidden bool
	rows   [][]any // string | numTxt | dateSerial | formulaNoValue | nil
}

func colLetters(j int) string { return colName(j) }

func buildXLSX(sheets []xsheet) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name, body string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	put("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>`)
	put("_rels/.rels", `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`)
	var wb, rels strings.Builder
	wb.WriteString(`<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><workbookPr/><sheets>`)
	rels.WriteString(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	// one shared string table, as Excel writes
	var shared []string
	idx := map[string]int{}
	sst := func(s string) int {
		if i, ok := idx[s]; ok {
			return i
		}
		idx[s] = len(shared)
		shared = append(shared, s)
		return idx[s]
	}
	for i, sh := range sheets {
		state := ""
		if sh.hidden {
			state = ` state="hidden"`
		}
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d"%s r:id="rId%d"/>`, xmlEsc(sh.name), i+1, state, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		var sx strings.Builder
		sx.WriteString(`<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
		for r, row := range sh.rows {
			fmt.Fprintf(&sx, `<row r="%d">`, r+1)
			for c, cell := range row {
				ref := fmt.Sprintf("%s%d", colLetters(c), r+1)
				switch v := cell.(type) {
				case string:
					fmt.Fprintf(&sx, `<c r="%s" t="s"><v>%d</v></c>`, ref, sst(v))
				case numTxt:
					fmt.Fprintf(&sx, `<c r="%s"><v>%s</v></c>`, ref, string(v))
				case dateSerial:
					fmt.Fprintf(&sx, `<c r="%s" s="1"><v>%g</v></c>`, ref, float64(v))
				case formulaNoValue:
					fmt.Fprintf(&sx, `<c r="%s"><f>SUM(A1:A2)</f></c>`, ref)
				}
			}
			sx.WriteString(`</row>`)
		}
		sx.WriteString(`</sheetData></worksheet>`)
		put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sx.String())
	}
	wb.WriteString(`</sheets></workbook>`)
	rels.WriteString(`</Relationships>`)
	put("xl/workbook.xml", wb.String())
	put("xl/_rels/workbook.xml.rels", rels.String())
	var ss strings.Builder
	ss.WriteString(`<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range shared {
		fmt.Fprintf(&ss, `<si><t xml:space="preserve">%s</t></si>`, xmlEsc(s))
	}
	ss.WriteString(`</sst>`)
	put("xl/sharedStrings.xml", ss.String())
	put("xl/styles.xml", `<?xml version="1.0"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><cellXfs count="2"><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs></styleSheet>`)
	_ = zw.Close()
	return buf.Bytes()
}

func xmlEsc(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	return strings.ReplaceAll(s, ">", "&gt;")
}

// statementSheet is the statement as spreadsheet rows, with a title that states the unit.
func statementSheet(name, title string) xsheet {
	rows := [][]any{{title}, {}, {"Particulars", "Note", "31 March 2026", "31 March 2025"}}
	num := func(s string) any {
		s = strings.ReplaceAll(s, ",", "")
		if strings.HasPrefix(s, "(") {
			return numTxt("-" + strings.Trim(s, "()"))
		}
		return numTxt(s)
	}
	for _, r := range statementRows {
		rows = append(rows, []any{r[0], num(r[1]), num(r[2]), num(r[3])})
	}
	return xsheet{name: name, rows: rows}
}

func newZip(b *bytes.Buffer) *zip.Writer { return zip.NewWriter(b) }
