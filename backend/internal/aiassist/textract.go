package aiassist

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/textract"
	txtypes "github.com/aws/aws-sdk-go-v2/service/textract/types"

	"c23desk/internal/docparse"
)

// Textract is the OCR backed by Amazon Textract. A multi-page PDF can only be analysed from S3, so the
// scan is put in a private bucket for the length of the job and deleted as soon as it finishes (the
// bucket's one-day expiry rule is the backstop if the delete itself fails).
type Textract struct {
	s3     *s3.Client
	tx     *textract.Client
	bucket string
	poll   time.Duration
}

func NewTextract(ctx context.Context, region, bucket string) (*Textract, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &Textract{s3: s3.NewFromConfig(cfg), tx: textract.NewFromConfig(cfg), bucket: bucket, poll: 2 * time.Second}, nil
}

func (t *Textract) Name() string { return "Amazon Textract" }

func (t *Textract) Pages(ctx context.Context, pdf []byte) ([]docparse.PageText, error) {
	var rnd [12]byte
	_, _ = rand.Read(rnd[:])
	key := "scans/" + time.Now().UTC().Format("2006-01-02") + "/" + hex.EncodeToString(rnd[:]) + ".pdf"
	if _, err := t.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(t.bucket), Key: aws.String(key), Body: bytes.NewReader(pdf),
		ContentType: aws.String("application/pdf"), ServerSideEncryption: s3types.ServerSideEncryptionAes256,
	}); err != nil {
		return nil, fmt.Errorf("the scan could not be staged for OCR: %w", err)
	}
	defer func() { // delete even when the request was abandoned
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		_, _ = t.s3.DeleteObject(dctx, &s3.DeleteObjectInput{Bucket: aws.String(t.bucket), Key: aws.String(key)})
	}()

	start, err := t.tx.StartDocumentAnalysis(ctx, &textract.StartDocumentAnalysisInput{
		DocumentLocation: &txtypes.DocumentLocation{S3Object: &txtypes.S3Object{Bucket: aws.String(t.bucket), Name: aws.String(key)}},
		FeatureTypes:     []txtypes.FeatureType{txtypes.FeatureTypeTables},
	})
	if err != nil {
		return nil, fmt.Errorf("Textract would not start: %w", err)
	}
	var blocks []txtypes.Block
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(t.poll):
		}
		out, err := t.tx.GetDocumentAnalysis(ctx, &textract.GetDocumentAnalysisInput{JobId: start.JobId})
		if err != nil {
			return nil, fmt.Errorf("Textract status could not be read: %w", err)
		}
		switch out.JobStatus {
		case txtypes.JobStatusInProgress:
			continue
		case txtypes.JobStatusSucceeded, txtypes.JobStatusPartialSuccess:
			blocks = append(blocks, out.Blocks...)
			for next := out.NextToken; next != nil; {
				more, err := t.tx.GetDocumentAnalysis(ctx, &textract.GetDocumentAnalysisInput{JobId: start.JobId, NextToken: next})
				if err != nil {
					return nil, fmt.Errorf("Textract results could not be read: %w", err)
				}
				blocks = append(blocks, more.Blocks...)
				next = more.NextToken
			}
			return PagesFromBlocks(blocks), nil
		default:
			msg := "Textract could not read the document"
			if out.StatusMessage != nil {
				msg += ": " + *out.StatusMessage
			}
			return nil, errors.New(msg)
		}
	}
}

// PagesFromBlocks turns Textract blocks into page text: each table as rows of cells joined by " | ",
// and the lines outside tables as they were read.
func PagesFromBlocks(blocks []txtypes.Block) []docparse.PageText {
	byID := map[string]txtypes.Block{}
	for _, b := range blocks {
		if b.Id != nil {
			byID[*b.Id] = b
		}
	}
	children := func(b txtypes.Block) []string {
		var ids []string
		for _, r := range b.Relationships {
			if r.Type == txtypes.RelationshipTypeChild {
				ids = append(ids, r.Ids...)
			}
		}
		return ids
	}
	inTable := map[string]bool{} // WORD ids that belong to a table cell
	tables := map[int][]string{} // page -> rendered tables
	for _, b := range blocks {
		if b.BlockType != txtypes.BlockTypeTable {
			continue
		}
		grid := map[int]map[int]string{}
		maxCol := 0
		for _, cid := range children(b) {
			cell, ok := byID[cid]
			if !ok || cell.BlockType != txtypes.BlockTypeCell || cell.RowIndex == nil || cell.ColumnIndex == nil {
				continue
			}
			var words []string
			for _, wid := range children(cell) {
				if w, ok := byID[wid]; ok && w.Text != nil {
					words = append(words, *w.Text)
					inTable[wid] = true
				}
			}
			r, c := int(*cell.RowIndex), int(*cell.ColumnIndex)
			if grid[r] == nil {
				grid[r] = map[int]string{}
			}
			grid[r][c] = strings.Join(words, " ")
			maxCol = max(maxCol, c)
		}
		var rowsIdx []int
		for r := range grid {
			rowsIdx = append(rowsIdx, r)
		}
		sort.Ints(rowsIdx)
		var sb strings.Builder
		for _, r := range rowsIdx {
			var cells []string
			for c := 1; c <= maxCol; c++ {
				if v := strings.TrimSpace(grid[r][c]); v != "" {
					cells = append(cells, v)
				}
			}
			if len(cells) > 0 {
				sb.WriteString(strings.Join(cells, " | ") + "\n")
			}
		}
		page := 1
		if b.Page != nil {
			page = int(*b.Page)
		}
		tables[page] = append(tables[page], sb.String())
	}
	lines := map[int][]string{}
	pages := map[int]bool{}
	for _, b := range blocks {
		if b.BlockType != txtypes.BlockTypeLine || b.Text == nil {
			continue
		}
		page := 1
		if b.Page != nil {
			page = int(*b.Page)
		}
		pages[page] = true
		ids := children(b)
		all := len(ids) > 0
		for _, id := range ids {
			if !inTable[id] {
				all = false
				break
			}
		}
		if !all { // a line fully inside a table is already in the table's rows
			lines[page] = append(lines[page], *b.Text)
		}
	}
	for p := range tables {
		pages[p] = true
	}
	var nums []int
	for p := range pages {
		nums = append(nums, p)
	}
	sort.Ints(nums)
	out := make([]docparse.PageText, 0, len(nums))
	for _, p := range nums {
		text := strings.Join(lines[p], "\n")
		for _, t := range tables[p] {
			text += "\n" + t
		}
		out = append(out, docparse.PageText{Page: p, Text: strings.TrimSpace(text)})
	}
	return out
}
