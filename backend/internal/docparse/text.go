package docparse

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PageText is the readable text of one PDF page or one worksheet, for a reader that looks beyond the
// description-and-amount table (the gateway's AI reader). Nothing here is interpreted: lines are copied
// as printed, cells separated by " | ".
type PageText struct {
	Page  int    `json:"page"`            // PDF page, or the 1-based sheet number for a workbook or CSV
	Sheet string `json:"sheet,omitempty"` // worksheet name
	Text  string `json:"text"`
}

// PageCount is the number of pages in a PDF.
func PageCount(b []byte) (n int, err error) {
	defer func() {
		if recover() != nil {
			n, err = 0, fail(CodeCorrupt, "file", "the PDF is damaged and could not be read; open it in a PDF viewer, print it to a new PDF and try again")
		}
	}()
	rd, rerr := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if rerr != nil {
		if rerr == pdf.ErrInvalidPassword {
			return 0, fail(CodeEncrypted, "file", "the PDF is password-protected; remove the password and upload it again")
		}
		return 0, fail(CodeCorrupt, "file", "the PDF is damaged or incomplete and could not be opened (%s)", shortErr(rerr))
	}
	return rd.NumPage(), nil
}

// SelectPages is the page list opt.Pages names for a document of total pages (every page when empty).
func SelectPages(spec string, total int) ([]int, error) { return parsePages(spec, total) }

// Texts returns the text of the selected PDF pages (pages without readable text are left out), of every
// worksheet of an XLSX or XLS, or of a CSV as one page. ctx is checked between pages and sheets.
func Texts(ctx context.Context, format string, b []byte, opt Options) (out []PageText, err error) {
	switch format {
	case FormatCSV:
		text := strings.ReplaceAll(string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))), "\r\n", "\n")
		return []PageText{{Page: 1, Text: text}}, nil
	case FormatXLSX, FormatXLS:
		var sheets []sheetData
		if format == FormatXLSX {
			sheets, err = readXLSX(ctx, b)
		} else {
			sheets, err = readXLS(ctx, b)
		}
		if err != nil {
			return nil, err
		}
		for i, sd := range sheets {
			if opt.Sheet != "" && sd.Name != opt.Sheet {
				continue
			}
			var sb strings.Builder
			for _, r := range sd.Rows {
				line := joinCells(r.Cells)
				if line == "" {
					continue
				}
				sb.WriteString("[row " + strconv.Itoa(r.Src.Row) + "] " + line + "\n")
			}
			if sb.Len() > 0 {
				out = append(out, PageText{Page: i + 1, Sheet: sd.Name, Text: sb.String()})
			}
		}
		if opt.Sheet != "" && len(out) == 0 {
			return nil, fail(CodeSheetNotFound, "sheet", "the workbook has no sheet named %q with any text", opt.Sheet)
		}
		return out, nil
	case FormatPDF:
		return pdfTexts(ctx, b, opt)
	}
	return nil, fail(CodeUnsupportedFormat, "file", "%q documents are not read here", format)
}

func pdfTexts(ctx context.Context, b []byte, opt Options) (out []PageText, err error) {
	defer func() {
		if recover() != nil {
			out, err = nil, fail(CodeCorrupt, "file", "the PDF is damaged and could not be read; open it in a PDF viewer, print it to a new PDF and try again")
		}
	}()
	rd, rerr := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if rerr != nil {
		if rerr == pdf.ErrInvalidPassword {
			return nil, fail(CodeEncrypted, "file", "the PDF is password-protected; remove the password and upload it again")
		}
		return nil, fail(CodeCorrupt, "file", "the PDF is damaged or incomplete and could not be opened (%s)", shortErr(rerr))
	}
	pages, perr := parsePages(opt.Pages, rd.NumPage())
	if perr != nil {
		return nil, perr
	}
	if len(pages) > opt.maxPages() {
		return nil, fail(CodeTooManyPages, "pages", "the document has %d pages and at most %d can be read at once; choose the pages to read, for example pages=120-140", len(pages), opt.maxPages())
	}
	for _, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil, fail(CodeTimeout, "", "reading the PDF took too long; choose fewer pages")
		}
		lines, ok, _ := readPDFPage(rd, p)
		if !ok {
			continue
		}
		var sb strings.Builder
		for _, l := range lines {
			texts := make([]string, len(l.cells))
			for k, c := range l.cells {
				texts[k] = c.text
			}
			if line := joinCells(texts); line != "" {
				sb.WriteString(line + "\n")
			}
		}
		out = append(out, PageText{Page: p, Text: sb.String()})
	}
	return out, nil
}

// joinCells is a row as one line: cells trimmed, empty ones dropped, " | " between them.
func joinCells(cells []string) string {
	var parts []string
	for _, c := range cells {
		if c = strings.Join(strings.Fields(normSpaces(c)), " "); c != "" {
			parts = append(parts, c)
		}
	}
	return strings.Join(parts, " | ")
}
