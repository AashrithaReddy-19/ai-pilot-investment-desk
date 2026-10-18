// Package docparse turns the financial documents people actually have (PDF, XLSX, XLS) into the
// plain CSV the rules engine's balance-sheet parser already understands.
//
// The engine stays the only place that reads money: this package copies amounts as the document prints
// them (never multiplies, never rounds), says where every line came from, and refuses, with a reason a
// person can act on, whenever it cannot read a file. It never returns an empty or "successful" result
// for a document it could not read.
//
// Nothing here stores anything and nothing here logs document contents.
package docparse

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Format names.
const (
	FormatCSV  = "csv"
	FormatXLSX = "xlsx"
	FormatXLS  = "xls"
	FormatPDF  = "pdf"
)

// Error codes. Each maps to a message a person can act on.
const (
	CodeEmptyFile         = "empty_file"
	CodeUnsupportedFormat = "unsupported_format"
	CodeEncrypted         = "file_encrypted"
	CodeCorrupt           = "file_corrupt"
	CodeScanned           = "scanned_document"
	CodeNoTable           = "no_table_found"
	CodeSheetNotFound     = "sheet_not_found"
	CodeInvalidPages      = "invalid_pages"
	CodeTooManyPages      = "too_many_pages"
	CodeTooLarge          = "document_too_large"
	CodeCurrency          = "currency_not_supported"
	CodeUnit              = "unit_not_supported"
	CodeTimeout           = "extraction_timeout"
)

// Error is a refusal with a stable code, a plain-language message and (optionally) the form field to blame.
type Error struct {
	Code    string
	Message string
	Field   string
}

func (e *Error) Error() string { return e.Message }

func fail(code, field, format string, a ...any) *Error {
	return &Error{Code: code, Field: field, Message: fmt.Sprintf(format, a...)}
}

// Source says where a converted line came from, so a reviewer can find it in the original.
type Source struct {
	Sheet string `json:"sheet,omitempty"`
	Row   int    `json:"row,omitempty"`  // 1-based spreadsheet row
	Page  int    `json:"page,omitempty"` // 1-based PDF page
	Text  string `json:"text,omitempty"` // the line as it was read
}

// Flag is something the reviewer must look at before trusting the numbers.
type Flag struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SheetInfo describes one worksheet, for the sheet picker.
type SheetInfo struct {
	Name      string `json:"name"`
	Hidden    bool   `json:"hidden"`
	Rows      int    `json:"rows"`      // non-empty rows read
	Usable    bool   `json:"usable"`    // has a description-and-amount table
	Suggested bool   `json:"suggested"` // the sheet used when none was chosen
}

// PageInfo describes a PDF's pages for the page picker.
type PageInfo struct {
	Total      int            `json:"total"`
	Selected   []int          `json:"selected"`    // pages that were read
	WithTables []int          `json:"with_tables"` // selected pages where a table was found
	NoText     []int          `json:"no_text"`     // selected pages with no readable text (scanned or empty)
	Units      map[int]string `json:"units"`       // unit stated on each table page ("" = none stated)
}

// DroppedColumn records a column left out of the converted CSV and why.
type DroppedColumn struct {
	Header string `json:"header"`
	Reason string `json:"reason"`
}

// Result is a converted document.
type Result struct {
	Format     string          `json:"format"`
	CSV        []byte          `json:"-"`
	Lines      []Source        `json:"-"` // Lines[i] is where CSV line i+1 came from; Lines[0] is the header line
	Sheet      string          `json:"sheet,omitempty"`
	Sheets     []SheetInfo     `json:"sheets,omitempty"`
	Pages      *PageInfo       `json:"pages,omitempty"`
	Dropped    []DroppedColumn `json:"dropped_columns,omitempty"`
	Currency   string          `json:"currency,omitempty"` // "INR" when the document says so; empty when it does not say
	Unit       string          `json:"unit,omitempty"`     // unit the document states ("" when it states none)
	UnitSource string          `json:"unit_source,omitempty"`
	Rows       int             `json:"rows"`
	Notes      []string        `json:"notes,omitempty"`
	Flags      []Flag          `json:"-"`
}

// Options narrows a conversion.
type Options struct {
	Sheet    string // spreadsheet: the sheet to read (empty = the best one)
	Pages    string // PDF: pages to read, e.g. "3-5,9" (empty = all)
	MaxPages int    // PDF: most pages one conversion reads (0 = 500)
	MaxRows  int    // most data rows in the converted table (0 = 20000, the engine's own limit)
}

func (o Options) maxPages() int {
	if o.MaxPages > 0 {
		return o.MaxPages
	}
	return 500
}

func (o Options) maxRows() int {
	if o.MaxRows > 0 {
		return o.MaxRows
	}
	return 20000
}

// Detect decides the format from the bytes themselves; the file name only sharpens the error message.
func Detect(name string, b []byte) (string, error) {
	ext := strings.ToLower(extOf(name))
	named := map[string]string{".pdf": "a PDF", ".xlsx": "an XLSX workbook", ".xls": "an XLS workbook"}[ext]
	if len(b) == 0 {
		return "", fail(CodeEmptyFile, "file", "the uploaded file is empty")
	}
	head := b
	if len(head) > 1024 {
		head = head[:1024]
	}
	switch {
	case bytes.Contains(head, []byte("%PDF-")):
		return FormatPDF, nil
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			return "", fail(CodeCorrupt, "file", "the file is damaged and cannot be opened as a workbook; open it in Excel and save it again")
		}
		for _, f := range zr.File {
			if f.Name == "xl/workbook.xml" {
				return FormatXLSX, nil
			}
		}
		return "", fail(CodeUnsupportedFormat, "file", "this is a ZIP-based file (for example Word or PowerPoint) but not an Excel workbook; upload a PDF, XLSX, XLS or CSV")
	case bytes.HasPrefix(b, oleMagic):
		if bytes.Contains(b, utf16le("EncryptedPackage")) {
			return "", fail(CodeEncrypted, "file", "the workbook is password-protected; remove the password in Excel and upload it again")
		}
		return FormatXLS, nil
	}
	body := bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	sample := body
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if bytes.IndexByte(sample, 0) >= 0 || !utf8.Valid(trimToRune(sample)) {
		if named != "" {
			return "", fail(CodeCorrupt, "file", "the file name says it is %s but the contents are not (the file may be damaged); open it in its application and save it again", named)
		}
		return "", fail(CodeUnsupportedFormat, "file", "this is not a PDF, Excel workbook or UTF-8 CSV file; save it as CSV UTF-8, XLSX or PDF and try again")
	}
	if named != "" {
		return "", fail(CodeCorrupt, "file", "the file name says it is %s but its contents are plain text; check the file, or save it as CSV and upload that", named)
	}
	return FormatCSV, nil
}

// Convert reads a PDF, XLSX or XLS document into engine-ready CSV. ctx is checked between pages and sheets.
func Convert(ctx context.Context, format string, b []byte, opt Options) (*Result, error) {
	switch format {
	case FormatXLSX:
		sheets, err := readXLSX(ctx, b)
		if err != nil {
			return nil, err
		}
		return convertSheets(ctx, FormatXLSX, sheets, opt)
	case FormatXLS:
		sheets, err := readXLS(ctx, b)
		if err != nil {
			return nil, err
		}
		return convertSheets(ctx, FormatXLS, sheets, opt)
	case FormatPDF:
		return convertPDF(ctx, b, opt)
	}
	return nil, fail(CodeUnsupportedFormat, "file", "%q documents are not converted here", format)
}

func extOf(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	return name[i:]
}

// trimToRune cuts an incomplete trailing UTF-8 sequence so a sample taken mid-character is not "invalid".
func trimToRune(b []byte) []byte {
	for i := 0; i < 3 && len(b) > 0; i++ {
		r, size := utf8.DecodeLastRune(b)
		if r == utf8.RuneError && size <= 1 {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}

func utf16le(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
