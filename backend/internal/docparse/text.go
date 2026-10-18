package docparse

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

type TextPage struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}

// ExtractText reads text-layer PDF pages without attempting OCR. Scanned pages are returned empty.
func ExtractText(ctx context.Context, b []byte) ([]TextPage, error) {
	rd, err := pdf.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	out := make([]TextPage, 0, rd.NumPage())
	for page := 1; page <= rd.NumPage(); page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pg := rd.Page(page)
		if pg.V.IsNull() {
			out = append(out, TextPage{Page: page})
			continue
		}
		var parts []string
		for _, item := range pg.Content().Text {
			s := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(item.S, "\n", " "), "\r", " "))
			if s != "" {
				parts = append(parts, s)
			}
		}
		out = append(out, TextPage{Page: page, Text: strings.Join(parts, " ")})
	}
	return out, nil
}
