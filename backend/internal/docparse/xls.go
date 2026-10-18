package docparse

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

// A BIFF8 (Excel 97-2003) reader written for this project. The published Go readers decode the RK number
// type incorrectly (they drop the "divide by 100" flag on integers and mis-sign negatives), which would
// silently turn 245.30 into 24530, so this one does the arithmetic exactly and keeps numbers as text.

const (
	recBOF       = 0x0809
	recEOF       = 0x000A
	recContinue  = 0x003C
	recFilepass  = 0x002F
	recCodepage  = 0x0042
	recDatemode  = 0x0022
	recFormat    = 0x041E
	recXF        = 0x00E0
	recBoundsh   = 0x0085
	recSST       = 0x00FC
	recLabelSST  = 0x00FD
	recLabel     = 0x0204
	recRString   = 0x00D6
	recNumber    = 0x0203
	recRK        = 0x027E
	recMulRK     = 0x00BD
	recFormula   = 0x0006
	recString    = 0x0207
	recBoolErr   = 0x0205
	maxBIFFSheet = 300000
)

type biffRec struct {
	id   int
	data []byte
}

// biffReader walks the records of a workbook stream.
type biffReader struct {
	b   []byte
	pos int
}

func (r *biffReader) next() (biffRec, bool) {
	if r.pos+4 > len(r.b) {
		return biffRec{}, false
	}
	id, n := int(binary.LittleEndian.Uint16(r.b[r.pos:])), int(binary.LittleEndian.Uint16(r.b[r.pos+2:]))
	if r.pos+4+n > len(r.b) {
		return biffRec{}, false
	}
	rec := biffRec{id: id, data: r.b[r.pos+4 : r.pos+4+n]}
	r.pos += 4 + n
	return rec, true
}

// ---- strings ----

func decodeChars(b []byte, utf16Enc bool) string {
	if !utf16Enc {
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c) // compressed strings are Latin-1
		}
		return string(rs)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

// readXLString reads an XLUnicodeString (2-byte length) or, with short=true, a ShortXLUnicodeString.
func readXLString(b []byte, short bool) (string, int, bool) {
	hdr := 3
	if short {
		hdr = 2
	}
	if len(b) < hdr {
		return "", 0, false
	}
	var cch int
	var flags byte
	if short {
		cch, flags = int(b[0]), b[1]
	} else {
		cch, flags = le16(b), b[2]
	}
	p := hdr
	runs, ext := 0, 0
	if flags&0x08 != 0 {
		if len(b) < p+2 {
			return "", 0, false
		}
		runs, p = le16(b[p:]), p+2
	}
	if flags&0x04 != 0 {
		if len(b) < p+4 {
			return "", 0, false
		}
		ext, p = int(le32(b[p:])), p+4
	}
	size := cch
	if flags&0x01 != 0 {
		size = cch * 2
	}
	if len(b) < p+size {
		return "", 0, false
	}
	s := decodeChars(b[p:p+size], flags&0x01 != 0)
	return s, p + size + runs*4 + ext, true
}

// sstReader reads the shared string table, whose strings may straddle CONTINUE records. At such a
// boundary, a string's characters resume after a fresh 1-byte encoding flag.
type sstReader struct {
	segs [][]byte
	si   int
	pos  int
}

func (r *sstReader) cur() []byte {
	if r.si >= len(r.segs) {
		return nil
	}
	return r.segs[r.si]
}

func (r *sstReader) advance() bool {
	r.si++
	r.pos = 0
	return r.si < len(r.segs)
}

func (r *sstReader) take(n int) ([]byte, bool) { // n bytes that never straddle a boundary
	for r.pos >= len(r.cur()) {
		if !r.advance() {
			return nil, false
		}
	}
	seg := r.cur()
	if r.pos+n > len(seg) {
		return nil, false
	}
	out := seg[r.pos : r.pos+n]
	r.pos += n
	return out, true
}

func (r *sstReader) skip(n int) bool {
	for n > 0 {
		for r.pos >= len(r.cur()) {
			if !r.advance() {
				return false
			}
		}
		k := len(r.cur()) - r.pos
		if k > n {
			k = n
		}
		r.pos += k
		n -= k
	}
	return true
}

func (r *sstReader) readString() (string, bool) {
	h, ok := r.take(3)
	if !ok {
		return "", false
	}
	cch, flags := le16(h), h[2]
	runs, ext := 0, 0
	if flags&0x08 != 0 {
		b, ok := r.take(2)
		if !ok {
			return "", false
		}
		runs = le16(b)
	}
	if flags&0x04 != 0 {
		b, ok := r.take(4)
		if !ok {
			return "", false
		}
		ext = int(le32(b))
	}
	wide := flags&0x01 != 0
	var sb strings.Builder
	for remaining := cch; remaining > 0; {
		if r.pos >= len(r.cur()) { // a CONTINUE record: the characters resume after an encoding flag
			if !r.advance() || len(r.cur()) == 0 {
				return "", false
			}
			wide, r.pos = r.cur()[0]&0x01 != 0, 1
		}
		seg := r.cur()
		unit := 1
		if wide {
			unit = 2
		}
		avail := (len(seg) - r.pos) / unit
		if avail == 0 {
			r.pos = len(seg)
			continue
		}
		k := remaining
		if k > avail {
			k = avail
		}
		sb.WriteString(decodeChars(seg[r.pos:r.pos+k*unit], wide))
		r.pos += k * unit
		remaining -= k
	}
	if !r.skip(runs*4 + ext) {
		return "", false
	}
	return sb.String(), true
}

func parseSST(segs [][]byte) ([]string, bool) {
	if len(segs) == 0 || len(segs[0]) < 8 {
		return nil, false
	}
	unique := int(le32(segs[0][4:]))
	r := &sstReader{segs: segs, pos: 8}
	if unique < 0 || unique > maxSharedString {
		return nil, false
	}
	out := make([]string, 0, unique)
	for i := 0; i < unique; i++ {
		s, ok := r.readString()
		if !ok {
			return out, false
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, true
}

// ---- numbers ----

// rkText decodes an RK value exactly. Integer RKs are 30-bit signed; the low bit says "divide by 100".
func rkText(rk uint32, isDate, date1904 bool) string {
	div100, isInt := rk&1 != 0, rk&2 != 0
	if isInt {
		n := int64(int32(rk) >> 2) // arithmetic shift keeps the sign of the 30-bit value
		if isDate {
			f := float64(n)
			if div100 {
				f /= 100
			}
			return serialToText(f, date1904)
		}
		if !div100 {
			return strconv.FormatInt(n, 10)
		}
		sign, a := "", n
		if a < 0 {
			sign, a = "-", -a
		}
		whole, frac := a/100, a%100
		switch {
		case frac == 0:
			return sign + strconv.FormatInt(whole, 10)
		case frac%10 == 0:
			return fmt.Sprintf("%s%d.%d", sign, whole, frac/10)
		}
		return fmt.Sprintf("%s%d.%02d", sign, whole, frac)
	}
	f := math.Float64frombits(uint64(rk&0xFFFFFFFC) << 32)
	if div100 {
		f /= 100
	}
	if isDate {
		return serialToText(f, date1904)
	}
	return numberText(f)
}

func boolErrText(val, isErr byte) string {
	if isErr == 0 {
		if val != 0 {
			return "TRUE"
		}
		return "FALSE"
	}
	switch val {
	case 0x00:
		return "#NULL!"
	case 0x07:
		return "#DIV/0!"
	case 0x0F:
		return "#VALUE!"
	case 0x17:
		return "#REF!"
	case 0x1D:
		return "#NAME?"
	case 0x24:
		return "#NUM!"
	case 0x2A:
		return "#N/A"
	}
	return "#ERROR"
}

type xlsSheetRef struct {
	name   string
	pos    int
	hidden bool
	typ    byte
}

func readXLS(ctx context.Context, b []byte) ([]sheetData, error) {
	cf, err := openCFB(b)
	if err != nil {
		return nil, fail(CodeCorrupt, "file", "the XLS file is damaged and cannot be opened; open it in Excel and save it again")
	}
	wb, err := cf.stream("Workbook", "Book")
	if err != nil {
		return nil, fail(CodeCorrupt, "file", "the XLS file is damaged and cannot be opened; open it in Excel and save it again")
	}
	if wb == nil {
		return nil, fail(CodeUnsupportedFormat, "file", "this file does not contain an Excel workbook (it may be a Word or PowerPoint file)")
	}

	r := &biffReader{b: wb}
	first, ok := r.next()
	if !ok {
		return nil, fail(CodeCorrupt, "file", "the XLS workbook is empty or damaged")
	}
	switch {
	case first.id == recBOF && len(first.data) >= 4 && le16(first.data) == 0x0600:
	case first.id == recBOF || first.id == 0x0009 || first.id == 0x0209 || first.id == 0x0409:
		return nil, fail(CodeUnsupportedFormat, "file", "this is an old Excel format (Excel 5.0/95 or earlier); open it in Excel and save it as XLSX")
	default:
		return nil, fail(CodeCorrupt, "file", "the XLS workbook is damaged (it does not start like an Excel workbook)")
	}

	var (
		date1904 bool
		formats  = map[int]string{}
		xfFmt    []int
		sheets   []xlsSheetRef
		sst      []string
	)
	for {
		rec, ok := r.next()
		if !ok {
			return nil, fail(CodeCorrupt, "file", "the XLS workbook is damaged (its header ends unexpectedly)")
		}
		if rec.id == recEOF {
			break
		}
		switch rec.id {
		case recFilepass:
			return nil, fail(CodeEncrypted, "file", "the workbook is password-protected; remove the password in Excel and upload it again")
		case recDatemode:
			if len(rec.data) >= 2 {
				date1904 = le16(rec.data) == 1
			}
		case recFormat:
			if len(rec.data) >= 5 {
				if s, _, ok := readXLString(rec.data[2:], false); ok {
					formats[le16(rec.data)] = s
				}
			}
		case recXF:
			if len(rec.data) >= 4 {
				xfFmt = append(xfFmt, le16(rec.data[2:]))
			}
		case recBoundsh:
			if len(rec.data) >= 8 {
				name, _, _ := readXLString(rec.data[6:], true)
				sheets = append(sheets, xlsSheetRef{name: name, pos: int(le32(rec.data)), hidden: rec.data[4]&0x03 != 0, typ: rec.data[5]})
			}
		case recSST:
			segs := [][]byte{rec.data}
			for {
				save := r.pos
				nx, ok := r.next()
				if !ok || nx.id != recContinue {
					r.pos = save
					break
				}
				segs = append(segs, nx.data)
			}
			var complete bool
			if sst, complete = parseSST(segs); !complete {
				return nil, fail(CodeCorrupt, "file", "the XLS workbook is damaged (its text table is incomplete)")
			}
		}
	}
	dateXF := make([]bool, len(xfFmt))
	for i, f := range xfFmt {
		if code, ok := formats[f]; ok {
			dateXF[i] = isDateFormat(code)
		} else {
			dateXF[i] = builtinDate(f)
		}
	}
	isDate := func(xf int) bool { return xf >= 0 && xf < len(dateXF) && dateXF[xf] }

	var out []sheetData
	for _, sh := range sheets {
		if sh.typ != 0 { // worksheets only
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, fail(CodeTimeout, "", "reading the workbook took too long")
		}
		if sh.pos < 0 || sh.pos >= len(wb) {
			return nil, fail(CodeCorrupt, "file", "the XLS workbook is damaged (sheet %q points outside the file)", sh.name)
		}
		sd, err := readXLSSheet(ctx, &biffReader{b: wb, pos: sh.pos}, sh.name, sst, isDate, date1904)
		if err != nil {
			return nil, err
		}
		sd.Hidden = sh.hidden
		out = append(out, sd)
	}
	if len(out) == 0 {
		return nil, fail(CodeNoTable, "file", "the workbook has no worksheets with cells")
	}
	return out, nil
}

func readXLSSheet(ctx context.Context, r *biffReader, name string, sst []string, isDate func(int) bool, date1904 bool) (sheetData, error) {
	sd := sheetData{Name: name}
	rows := map[int]map[int]string{}
	put := func(row, col int, text string) {
		if col < 0 || col >= maxSheetCols || row < 0 {
			return
		}
		m := rows[row]
		if m == nil {
			m = map[int]string{}
			rows[row] = m
		}
		m[col] = text
	}
	depth, seen := 0, 0
	pendRow, pendCol := -1, -1 // a formula whose text result arrives in the next STRING record
	for {
		rec, ok := r.next()
		if !ok {
			return sd, fail(CodeCorrupt, "file", "the XLS workbook is damaged (sheet %q ends unexpectedly)", name)
		}
		if seen++; seen%5000 == 0 {
			if err := ctx.Err(); err != nil {
				return sd, fail(CodeTimeout, "", "reading the workbook took too long")
			}
		}
		if seen > 5_000_000 || len(rows) > maxBIFFSheet {
			return sd, fail(CodeTooLarge, "file", "sheet %q is too large to read; split it and try again", name)
		}
		d := rec.data
		switch rec.id {
		case recBOF:
			depth++
			continue
		case recEOF:
			depth--
			if depth <= 0 {
				return finishXLSSheet(sd, rows), nil
			}
			continue
		}
		if depth != 1 {
			continue // embedded sub-streams (charts) are not cells
		}
		switch rec.id {
		case recNumber:
			if len(d) >= 14 {
				v := math.Float64frombits(binary.LittleEndian.Uint64(d[6:]))
				if isDate(le16(d[4:])) {
					put(le16(d), le16(d[2:]), serialToText(v, date1904))
				} else {
					put(le16(d), le16(d[2:]), numberText(v))
				}
			}
		case recRK:
			if len(d) >= 10 {
				put(le16(d), le16(d[2:]), rkText(le32(d[6:]), isDate(le16(d[4:])), date1904))
			}
		case recMulRK:
			if len(d) >= 12 {
				row, col := le16(d), le16(d[2:])
				for off := 4; off+6 <= len(d)-2; off += 6 {
					put(row, col, rkText(le32(d[off+2:]), isDate(le16(d[off:])), date1904))
					col++
				}
			}
		case recLabelSST:
			if len(d) >= 10 {
				if i := int(le32(d[6:])); i >= 0 && i < len(sst) {
					put(le16(d), le16(d[2:]), sst[i])
				}
			}
		case recLabel, recRString:
			if len(d) >= 9 {
				if s, _, ok := readXLString(d[6:], false); ok {
					put(le16(d), le16(d[2:]), strings.TrimSpace(s))
				}
			}
		case recBoolErr:
			if len(d) >= 8 {
				put(le16(d), le16(d[2:]), boolErrText(d[6], d[7]))
			}
		case recFormula:
			if len(d) >= 14 {
				row, col := le16(d), le16(d[2:])
				res := d[6:14]
				if res[6] == 0xFF && res[7] == 0xFF {
					switch res[0] {
					case 0:
						pendRow, pendCol = row, col
					case 1:
						put(row, col, boolErrText(res[2], 0))
					case 2:
						put(row, col, boolErrText(res[2], 1))
					}
				} else {
					v := math.Float64frombits(binary.LittleEndian.Uint64(res))
					if isDate(le16(d[4:])) {
						put(row, col, serialToText(v, date1904))
					} else {
						put(row, col, numberText(v))
					}
				}
			}
		case recString:
			if pendRow >= 0 {
				if s, _, ok := readXLString(d, false); ok {
					put(pendRow, pendCol, strings.TrimSpace(s))
				}
				pendRow, pendCol = -1, -1
			}
		}
	}
}

func finishXLSSheet(sd sheetData, rows map[int]map[int]string) sheetData {
	nums := make([]int, 0, len(rows))
	for n := range rows {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	for _, n := range nums {
		m := rows[n]
		maxc := -1
		for c, v := range m {
			if strings.TrimSpace(v) != "" && c > maxc {
				maxc = c
			}
		}
		if maxc < 0 {
			continue
		}
		cells := make([]string, maxc+1)
		for c, v := range m {
			if c <= maxc {
				cells[c] = v
			}
		}
		sd.Rows = append(sd.Rows, gridRow{Src: Source{Sheet: sd.Name, Row: n + 1}, Cells: cells})
	}
	return sd
}
