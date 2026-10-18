package docparse

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"path"
	"strconv"
	"strings"
)

// A small XLSX reader on the standard library: a workbook is a ZIP of XML parts. Numbers are kept as the
// exact text the workbook stores (so 245.30 stays 245.30), dates are rendered as dates, formulas use
// their stored result, and every part is read through a size limit.

const (
	maxXLSXPart     = 128 << 20 // largest single XML part once decompressed
	maxXLSXTotal    = 512 << 20 // all parts together (zip-bomb guard)
	maxSheetRows    = 300000    // rows scanned in one sheet
	maxSheetCols    = 2000      // columns kept per row
	maxSharedString = 2_000_000
)

type sheetData struct {
	Name           string
	Hidden         bool
	Rows           []gridRow
	FormulaNoValue int // cells whose formula has no stored result
}

type xlsxSheetRef struct {
	name, rid string
	hidden    bool
}

func readXLSX(ctx context.Context, b []byte) ([]sheetData, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fail(CodeCorrupt, "file", "the XLSX workbook is damaged and cannot be opened; open it in Excel and save it again")
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range zr.File {
		files[strings.ToLower(f.Name)] = f
		total += f.UncompressedSize64
	}
	if total > maxXLSXTotal {
		return nil, fail(CodeTooLarge, "file", "the workbook expands to more than %d MB; split it and try again", maxXLSXTotal>>20)
	}
	open := func(name string) ([]byte, error) {
		f := files[strings.ToLower(name)]
		if f == nil {
			return nil, nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fail(CodeCorrupt, "file", "the workbook is damaged (%s cannot be read)", name)
		}
		defer rc.Close()
		data, err := io.ReadAll(io.LimitReader(rc, maxXLSXPart+1))
		if err != nil {
			return nil, fail(CodeCorrupt, "file", "the workbook is damaged (%s cannot be read)", name)
		}
		if len(data) > maxXLSXPart {
			return nil, fail(CodeTooLarge, "file", "a part of the workbook is larger than %d MB; split it and try again", maxXLSXPart>>20)
		}
		return data, nil
	}

	wbXML, err := open("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	if wbXML == nil {
		return nil, fail(CodeUnsupportedFormat, "file", "this ZIP file is not an Excel workbook")
	}
	refs, date1904, err := parseWorkbook(wbXML)
	if err != nil {
		return nil, err
	}
	relsXML, err := open("xl/_rels/workbook.xml.rels")
	if err != nil {
		return nil, err
	}
	targets := parseRels(relsXML)

	var shared []string
	if data, err := open("xl/sharedStrings.xml"); err != nil {
		return nil, err
	} else if data != nil {
		if shared, err = parseSharedStrings(data); err != nil {
			return nil, err
		}
	}
	var dateXF map[int]bool
	if data, err := open("xl/styles.xml"); err != nil {
		return nil, err
	} else if data != nil {
		dateXF = parseStyles(data)
	}

	var sheets []sheetData
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, fail(CodeTimeout, "", "reading the workbook took too long")
		}
		tg, ok := targets[ref.rid]
		if !ok || !strings.HasSuffix(tg.typ, "/worksheet") {
			continue // chart sheets and dialog sheets have no cells
		}
		p := tg.target
		if strings.HasPrefix(p, "/") {
			p = p[1:]
		} else {
			p = path.Join("xl", p)
		}
		data, err := open(p)
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, fail(CodeCorrupt, "file", "the workbook is damaged (sheet %q is missing)", ref.name)
		}
		sd, err := parseSheet(ctx, ref.name, data, shared, dateXF, date1904)
		if err != nil {
			return nil, err
		}
		sd.Hidden = ref.hidden
		sheets = append(sheets, sd)
	}
	if len(sheets) == 0 {
		return nil, fail(CodeNoTable, "file", "the workbook has no worksheets with cells")
	}
	return sheets, nil
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func parseWorkbook(data []byte) (refs []xlsxSheetRef, date1904 bool, err error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, e := d.Token()
		if e == io.EOF {
			return refs, date1904, nil
		}
		if e != nil {
			return nil, false, fail(CodeCorrupt, "file", "the workbook is damaged (its sheet list cannot be read)")
		}
		if se, ok := tok.(xml.StartElement); ok {
			switch se.Name.Local {
			case "workbookPr":
				v := strings.ToLower(attr(se, "date1904"))
				date1904 = v == "1" || v == "true"
			case "sheet":
				st := strings.ToLower(attr(se, "state"))
				refs = append(refs, xlsxSheetRef{name: attr(se, "name"), rid: attr(se, "id"), hidden: st == "hidden" || st == "veryhidden"})
			}
		}
	}
}

type relTarget struct{ target, typ string }

func parseRels(data []byte) map[string]relTarget {
	out := map[string]relTarget{}
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err != nil {
			return out
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			out[attr(se, "Id")] = relTarget{target: attr(se, "Target"), typ: attr(se, "Type")}
		}
	}
}

func parseSharedStrings(data []byte) ([]string, error) {
	var out []string
	d := xml.NewDecoder(bytes.NewReader(data))
	var cur strings.Builder
	inSI, inT, skip := false, false, 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, fail(CodeCorrupt, "file", "the workbook is damaged (its text table cannot be read)")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI, cur = true, strings.Builder{}
			case "rPh", "phoneticPr":
				skip++
			case "t":
				inT = inSI && skip == 0
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSI = false
				if len(out) > maxSharedString {
					return nil, fail(CodeTooLarge, "file", "the workbook has too many distinct text values")
				}
			case "rPh", "phoneticPr":
				if skip > 0 {
					skip--
				}
			case "t":
				inT = false
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
	}
}

// parseStyles returns, per cell-format index, whether the format is a date or time.
func parseStyles(data []byte) map[int]bool {
	custom := map[int]string{}
	dateXF := map[int]bool{}
	d := xml.NewDecoder(bytes.NewReader(data))
	inCellXfs := false
	idx := 0
	for {
		tok, err := d.Token()
		if err != nil {
			return dateXF
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				if id, err := strconv.Atoi(attr(t, "numFmtId")); err == nil {
					custom[id] = attr(t, "formatCode")
				}
			case "cellXfs":
				inCellXfs, idx = true, 0
			case "xf":
				if inCellXfs {
					if id, err := strconv.Atoi(attr(t, "numFmtId")); err == nil {
						if code, ok := custom[id]; ok {
							dateXF[idx] = isDateFormat(code)
						} else {
							dateXF[idx] = builtinDate(id)
						}
					}
					idx++
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				inCellXfs = false
			}
		}
	}
}

// colIndex turns the letters of a cell reference ("BC12") into a 0-based column.
func colIndex(ref string) int {
	n := 0
	seen := false
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z':
			n, seen = n*26+int(r-'A')+1, true
		case r >= 'a' && r <= 'z':
			n, seen = n*26+int(r-'a')+1, true
		default:
			if seen {
				return n - 1
			}
			return -1
		}
	}
	if seen {
		return n - 1
	}
	return -1
}

func parseSheet(ctx context.Context, name string, data []byte, shared []string, dateXF map[int]bool, date1904 bool) (sheetData, error) {
	sd := sheetData{Name: name}
	d := xml.NewDecoder(bytes.NewReader(data))
	var (
		rowNum, scanned int
		cells           []string
		curCol          int
		cType, cVal     string
		cStyle          int
		cInline         strings.Builder
		hasFormula      bool
		inRow, inCell   bool
		inV, inT        bool
		inIS            bool
	)
	flush := func() {
		var maxc int = -1
		for j, c := range cells {
			if strings.TrimSpace(c) != "" {
				maxc = j
			}
		}
		if maxc >= 0 {
			sd.Rows = append(sd.Rows, gridRow{Src: Source{Sheet: name, Row: rowNum}, Cells: append([]string{}, cells[:maxc+1]...)})
		}
	}
	for {
		if scanned%2000 == 0 {
			if err := ctx.Err(); err != nil {
				return sd, fail(CodeTimeout, "", "reading the workbook took too long")
			}
		}
		tok, err := d.Token()
		if err == io.EOF {
			return sd, nil
		}
		if err != nil {
			return sd, fail(CodeCorrupt, "file", "the workbook is damaged (sheet %q cannot be read)", name)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				scanned++
				if scanned > maxSheetRows {
					return sd, fail(CodeTooLarge, "file", "sheet %q has more than %d rows; split it and try again", name, maxSheetRows)
				}
				if r, err := strconv.Atoi(attr(t, "r")); err == nil && r > 0 {
					rowNum = r
				} else {
					rowNum++
				}
				cells, curCol, inRow = cells[:0], -1, true
			case "c":
				if !inRow {
					continue
				}
				inCell, cType, cVal, hasFormula, cInline = true, attr(t, "t"), "", false, strings.Builder{}
				cStyle, _ = strconv.Atoi(attr(t, "s"))
				if c := colIndex(attr(t, "r")); c >= 0 {
					curCol = c
				} else {
					curCol++
				}
			case "v":
				inV = inCell
			case "f":
				if inCell {
					hasFormula = true
				}
			case "is":
				inIS = inCell
			case "t":
				inT = inIS
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "t":
				inT = false
			case "is":
				inIS = false
			case "c":
				if !inCell {
					continue
				}
				inCell = false
				if curCol < 0 || curCol >= maxSheetCols {
					continue
				}
				text := cellText(cType, cVal, cInline.String(), cStyle, shared, dateXF, date1904)
				if text == "" && hasFormula && cVal == "" {
					sd.FormulaNoValue++
				}
				for len(cells) <= curCol {
					cells = append(cells, "")
				}
				cells[curCol] = text
			case "row":
				if inRow {
					flush()
				}
				inRow = false
			}
		case xml.CharData:
			switch {
			case inV:
				cVal += string(t)
			case inT:
				cInline.Write(t)
			}
		}
	}
}

func cellText(t, v, inline string, style int, shared []string, dateXF map[int]bool, date1904 bool) string {
	switch t {
	case "s":
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i >= 0 && i < len(shared) {
			return strings.TrimSpace(shared[i])
		}
		return ""
	case "inlineStr":
		return strings.TrimSpace(inline)
	case "str":
		return strings.TrimSpace(v)
	case "b":
		if strings.TrimSpace(v) == "1" {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		return strings.TrimSpace(v)
	case "d": // ISO 8601 date stored as text
		return strings.TrimSpace(v)
	}
	if strings.TrimSpace(v) == "" {
		return ""
	}
	if dateXF[style] {
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return serialToText(f, date1904)
		}
	}
	return xmlNumberText(v)
}
