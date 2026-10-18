package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
	"c23desk/internal/rules"
)

// Uploads (B4): a document is validated, fingerprinted and stored with its original bytes; a background
// worker sends it to the engine parser (with retries) and keeps the answer. People review the parsed rows,
// record corrections (the original value is always kept beside the corrected one), and confirm. Confirming
// sends the stored file plus the corrections to the engine, so even corrected numbers are summed by the
// engine, not here.

type Uploads interface {
	CreateUpload(ctx context.Context, in model.UploadIn) (*model.Upload, bool, error)
	GetUpload(ctx context.Context, id string) (*model.Upload, error)
	UploadContent(ctx context.Context, id string) (*model.UploadJob, error)
	ListUploads(ctx context.Context, limit int) ([]model.Upload, error)
	RetryUpload(ctx context.Context, id string) (*model.Upload, error)
	ConfirmUpload(ctx context.Context, id string, confirmed json.RawMessage) (*model.Upload, error)
	AddCorrection(ctx context.Context, in model.CorrectionIn) (*model.Correction, error)
	ListCorrections(ctx context.Context, uploadID string) ([]model.Correction, error)
}

const xlsxMIME = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
const xlsMIME = "application/vnd.ms-excel"
const pdfMIME = "application/pdf"
const docxMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

var (
	uploadKinds   = map[string]bool{"balance_sheet": true, "pilots": true}
	uploadUnits   = map[string]bool{"auto": true, "rupees": true, "thousands": true, "lakh": true, "crore": true}
	uploadPeriods = map[string]bool{"auto": true, "annual": true, "half_year": true, "quarterly": true, "monthly": true}
	// ParsePath / ConfirmPath per kind on the engine.
	ParsePath   = map[string]string{"balance_sheet": "/balance-sheet/parse", "pilots": "/pilots/import/parse"}
	ConfirmPath = map[string]string{"balance_sheet": "/balance-sheet/confirm", "pilots": "/pilots/import/confirm"}
)

// UploadFields turns stored upload options into the engine's form fields.
func UploadFields(options json.RawMessage) map[string]string {
	var o map[string]string
	_ = json.Unmarshal(options, &o)
	out := map[string]string{}
	for _, k := range []string{"unit", "period", "amount_column"} {
		if v := o[k]; v != "" {
			out[k] = v
		}
	}
	return out
}

// sniffFormat checks the bytes really are what the extension claims.
func sniffFormat(name string, b []byte) (format, contentType string, err error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		if !bytes.HasPrefix(b, []byte("%PDF-")) {
			return "", "", errors.New("the file is named .pdf but does not have a valid PDF signature")
		}
		return "pdf", pdfMIME, nil
	case ".xlsx":
		if !bytes.HasPrefix(b, []byte("PK\x03\x04")) {
			return "", "", errors.New("the file is named .xlsx but is not an XLSX workbook")
		}
		if !zipHasFile(b, "[Content_Types].xml") {
			return "", "", errors.New("the file is named .xlsx but is not an XLSX workbook")
		}
		return "xlsx", xlsxMIME, nil
	case ".xls":
		if !bytes.HasPrefix(b, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
			return "", "", errors.New("the file is named .xls but is not an XLS workbook")
		}
		return "xls", xlsMIME, nil
	case ".docx":
		if !bytes.HasPrefix(b, []byte("PK\x03\x04")) || !zipHasFile(b, "[Content_Types].xml") || !zipHasFile(b, "word/document.xml") {
			return "", "", errors.New("the file is named .docx but is not a DOCX document")
		}
		return "docx", docxMIME, nil
	case ".csv":
		body := bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
		if bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body) {
			return "", "", errors.New("the file is named .csv but is not UTF-8 text; save it as CSV UTF-8")
		}
		return "csv", "text/csv", nil
	}
	return "", "", errors.New("only PDF, XLSX, XLS, DOCX and CSV files are accepted")
}

func zipHasFile(b []byte, want string) bool {
	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return false
	}
	for _, f := range r.File {
		if f.Name == want {
			return true
		}
	}
	return false
}

// createUpload — POST /api/v1/uploads (multipart: file, kind, unit?, period?, amount_column?).
func (h *Handlers) createUpload(c *gin.Context) {
	limit := h.cfg.UploadMaxBytes
	if limit <= 0 {
		limit = 2_000_000
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+64<<10) // file + form overhead
	fh, err := c.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes", limit), "file")
			return
		}
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "send the document as multipart field \"file\"", "file")
		return
	}
	kind := c.PostForm("kind")
	if !uploadKinds[kind] {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "kind must be balance_sheet or pilots", "kind")
		return
	}
	opts := map[string]string{}
	for field, allowed := range map[string]map[string]bool{"unit": uploadUnits, "period": uploadPeriods} {
		if v := c.PostForm(field); v != "" {
			if !allowed[v] {
				fail(c, http.StatusUnprocessableEntity, "invalid_value", fmt.Sprintf("%s %q is not supported", field, v), field)
				return
			}
			opts[field] = v
		}
	}
	if v := c.PostForm("amount_column"); v != "" {
		if len(v) > 100 || kind != "balance_sheet" {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "amount_column is a balance-sheet header of at most 100 characters", "amount_column")
			return
		}
		opts["amount_column"] = v
	}
	name := filepath.Base(strings.ReplaceAll(fh.Filename, "\\", "/"))
	if name == "" || name == "." || len(name) > 200 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "the file needs a name of at most 200 characters", "file")
		return
	}
	if fh.Size <= 0 || fh.Size > limit {
		fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("uploads must be 1 to %d bytes", limit), "file")
		return
	}
	f, err := fh.Open()
	if err != nil {
		fail(c, http.StatusBadRequest, "bad_request", "could not read the uploaded file", "file")
		return
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(content)) > limit {
		fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("uploads are limited to %d bytes", limit), "file")
		return
	}
	format, ctype, err := sniffFormat(name, content)
	if err != nil {
		fail(c, http.StatusUnsupportedMediaType, "unsupported_file", err.Error(), "file")
		return
	}
	sum := sha256.Sum256(content)
	optJSON, _ := json.Marshal(opts)
	u, created, err := h.uploads.CreateUpload(c.Request.Context(), model.UploadIn{
		Kind: kind, Filename: name, ContentType: ctype, Format: format, SHA256: hex.EncodeToString(sum[:]),
		Content: content, Options: optJSON,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if created {
		h.record(c, "upload.create", "upload", u.ID, gin.H{"kind": kind, "format": format, "size_bytes": u.SizeBytes, "sha256": u.SHA256})
		if h.notify != nil {
			h.notify()
		}
	}
	status := http.StatusAccepted
	if !created {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"upload": u, "duplicate": !created})
}

func (h *Handlers) listUploads(c *gin.Context) {
	list, err := h.uploads.ListUploads(c.Request.Context(), queryLimit(c, 50))
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"uploads": list})
}

func (h *Handlers) uploadParam(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return "", false
	}
	return id, true
}

// getUpload — GET /api/v1/uploads/:id: the job, its corrections and the review rows (original beside corrected).
func (h *Handlers) getUpload(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	corr, err := h.uploads.ListCorrections(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"upload": u, "corrections": corr, "review": reviewRows(u, corr)})
}

// downloadUpload — GET /api/v1/uploads/:id/file: the original bytes, as an attachment, workspace-scoped.
func (h *Handlers) downloadUpload(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	var legacyJob *model.UploadJob
	filename := u.Filename
	if filename == "" {
		legacyJob, err = h.uploads.UploadContent(c.Request.Context(), id)
		if err != nil {
			h.storeErr(c, err)
			return
		}
		filename = legacyJob.Filename
	}
	safe := strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, filename)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safe))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, no-store")
	h.record(c, "upload.download", "upload", id, nil)
	if u.StorageProvider == "s3" && u.ObjectKey != nil {
		if h.objects == nil {
			fail(c, http.StatusServiceUnavailable, "not_configured", "S3 document storage is not configured on this gateway", "")
			return
		}
		body, err := h.objects.Open(c.Request.Context(), *u.ObjectKey)
		if err != nil {
			fail(c, http.StatusBadGateway, "storage_unavailable", "document storage is unavailable", "")
			return
		}
		defer body.Close()
		c.DataFromReader(http.StatusOK, int64(u.SizeBytes), u.ContentType, body, nil)
		return
	}
	if legacyJob == nil {
		legacyJob, err = h.uploads.UploadContent(c.Request.Context(), id)
		if err != nil {
			h.storeErr(c, err)
			return
		}
	}
	c.Data(http.StatusOK, legacyJob.ContentType, legacyJob.Content)
}

func (h *Handlers) retryUpload(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	u, err := h.uploads.RetryUpload(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusConflict, "not_retryable", "only a failed upload of this workspace can be retried", "id")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "upload.retry", "upload", id, nil)
	if h.notify != nil {
		h.notify()
	}
	c.JSON(http.StatusOK, gin.H{"upload": u})
}

// ---------------------------------------------------------------- review and corrections

type parsedBalance struct {
	Candidates []struct {
		Row      int      `json:"row"`
		Label    string   `json:"label"`
		AmountIN *float64 `json:"amount_inr"`
	} `json:"candidates"`
}

type parsedPilots struct {
	Pilots     []map[string]json.RawMessage `json:"pilots"`
	Extensions []map[string]json.RawMessage `json:"extensions"`
}

var pilotFieldRules = map[string]string{ // field -> value rule
	"task": "text200", "setup_cost_inr": "money", "annual_benefit_inr": "money", "annual_running_cost_inr": "money_or_null",
	"benefit_type": "benefit_type", "overlap_group": "text40", "ramp_months": "months", "delay_months": "months",
	"adoption_pct": "pct", "readiness": "readiness",
}
var extensionFields = map[string]bool{"adoption_pct": true, "readiness": true}

func checkValue(rule string, raw json.RawMessage) error {
	if isNull(raw) {
		if rule == "money_or_null" {
			return nil
		}
		return errors.New("value must not be null")
	}
	var f float64
	num := json.Unmarshal(raw, &f) == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	var s string
	str := json.Unmarshal(raw, &s) == nil
	switch rule {
	case "money", "money_or_null":
		if !num || f < 0 {
			return errors.New("value must be a non-negative number of rupees")
		}
	case "pct":
		if !num || f < 0 || f > 100 {
			return errors.New("value must be a percentage from 0 to 100")
		}
	case "months":
		if !num || f < 0 || f > 120 || f != math.Trunc(f) {
			return errors.New("value must be a whole number of months from 0 to 120")
		}
	case "text200", "text40":
		max := 200
		if rule == "text40" {
			max = 40
		}
		if !str || strings.TrimSpace(s) == "" || len(s) > max {
			return fmt.Errorf("value must be text of 1 to %d characters", max)
		}
	case "benefit_type":
		if !str || !map[string]bool{"cashable": true, "cashable assumption": true, "time freed": true, "non-cashable": true, "unknown": true}[s] {
			return errors.New("value must be cashable, time freed, non-cashable or unknown")
		}
	case "readiness":
		if !str || (s != "low" && s != "medium" && s != "high") {
			return errors.New("value must be low, medium or high")
		}
	}
	return nil
}

// latestCorrections keeps the newest correction per (row, field).
func latestCorrections(list []model.Correction) map[string]map[string]model.Correction {
	out := map[string]map[string]model.Correction{}
	for _, c := range list {
		if out[c.RowRef] == nil {
			out[c.RowRef] = map[string]model.Correction{}
		}
		out[c.RowRef][c.Field] = c
	}
	return out
}

// reviewRows lays the corrections over the parsed rows for display. Presentation only: confirm sends
// the corrections to the engine, which produces the authoritative numbers.
func reviewRows(u *model.Upload, corr []model.Correction) any {
	if len(u.Result) == 0 {
		return nil
	}
	latest := latestCorrections(corr)
	switch u.Kind {
	case "balance_sheet":
		var pb parsedBalance
		if json.Unmarshal(u.Result, &pb) != nil {
			return nil
		}
		rows := []gin.H{}
		for _, cnd := range pb.Candidates {
			ref := strconv.Itoa(cnd.Row)
			row := gin.H{"row": cnd.Row, "label": cnd.Label, "amount_inr": cnd.AmountIN, "corrected": false}
			if cc, ok := latest[ref]["amount_inr"]; ok {
				row["corrected"], row["corrected_amount_inr"], row["note"] = true, cc.CorrectedValue, cc.Note
			}
			rows = append(rows, row)
		}
		return gin.H{"kind": "balance_sheet", "rows": rows}
	default:
		var pp parsedPilots
		if json.Unmarshal(u.Result, &pp) != nil {
			return nil
		}
		rows := []gin.H{}
		for i, p := range pp.Pilots {
			var id string
			_ = json.Unmarshal(p["id"], &id)
			values := gin.H{}
			for k, v := range p {
				values[k] = v
			}
			if i < len(pp.Extensions) {
				for k, v := range pp.Extensions[i] {
					if k != "id" {
						values[k] = v
					}
				}
			}
			fixes := gin.H{}
			for field, cc := range latest[id] {
				fixes[field] = gin.H{"original": cc.OriginalValue, "corrected": cc.CorrectedValue, "note": cc.Note}
			}
			rows = append(rows, gin.H{"id": id, "values": values, "corrections": fixes})
		}
		return gin.H{"kind": "pilots", "rows": rows}
	}
}

// addCorrection — POST /api/v1/uploads/:id/corrections {row_ref, field, value, note?}.
func (h *Handlers) addCorrection(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if u.Status != "parsed" && u.Status != "confirmed" {
		fail(c, http.StatusConflict, "not_reviewable", "corrections need a parsed upload (status is "+u.Status+")", "id")
		return
	}
	var rowRef, field string
	if json.Unmarshal(in["row_ref"], &rowRef) != nil {
		var n int
		if json.Unmarshal(in["row_ref"], &n) != nil {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "row_ref is the balance-sheet row number or the pilot id", "row_ref")
			return
		}
		rowRef = strconv.Itoa(n)
	}
	if json.Unmarshal(in["field"], &field) != nil || field == "" {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "field is required", "field")
		return
	}
	value, present := in["value"]
	if !present {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "value is required (the corrected value)", "value")
		return
	}
	var note *string
	if raw, ok := in["note"]; ok && !isNull(raw) {
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) > 500 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "note must be at most 500 characters", "note")
			return
		}
		note = &s
	}
	var original json.RawMessage
	switch u.Kind {
	case "balance_sheet":
		if field != "amount_inr" {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "balance-sheet rows can correct amount_inr only", "field")
			return
		}
		if err := checkValue("money", value); err != nil {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "value")
			return
		}
		var pb parsedBalance
		_ = json.Unmarshal(u.Result, &pb)
		found := false
		for _, cnd := range pb.Candidates {
			if strconv.Itoa(cnd.Row) == rowRef {
				found, original = true, mustJSON(cnd.AmountIN)
			}
		}
		if !found {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "row_ref is not a candidate row of this upload", "row_ref")
			return
		}
	default:
		rule, okf := pilotFieldRules[field]
		if !okf {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "field cannot be corrected here", "field")
			return
		}
		if err := checkValue(rule, value); err != nil {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "value")
			return
		}
		var pp parsedPilots
		_ = json.Unmarshal(u.Result, &pp)
		found := false
		for i, p := range pp.Pilots {
			var pid string
			_ = json.Unmarshal(p["id"], &pid)
			if pid != rowRef {
				continue
			}
			found = true
			src := p
			if extensionFields[field] && i < len(pp.Extensions) {
				src = pp.Extensions[i]
			}
			if v, ok := src[field]; ok {
				original = v
			}
		}
		if !found {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "row_ref is not a parsed pilot id of this upload", "row_ref")
			return
		}
	}
	saved, err := h.uploads.AddCorrection(c.Request.Context(), model.CorrectionIn{
		UploadID: id, RowRef: rowRef, Field: field, OriginalValue: original, CorrectedValue: value, Note: note,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "upload.correct", "upload", id, gin.H{"row_ref": rowRef, "field": field})
	c.JSON(http.StatusCreated, saved)
}

// confirmUpload — POST /api/v1/uploads/:id/confirm: balance sheet {rows: [n...]}, pilots {keep: [id...]}.
// The engine re-reads the stored file and applies the corrections.
func (h *Handlers) confirmUpload(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	j, err := h.uploads.UploadContent(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if u.Status != "parsed" && u.Status != "confirmed" {
		fail(c, http.StatusConflict, "not_reviewable", "only a parsed upload can be confirmed (status is "+u.Status+")", "id")
		return
	}
	corr, err := h.uploads.ListCorrections(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	fields := UploadFields(j.Options)
	latest := latestCorrections(corr)
	switch j.Kind {
	case "balance_sheet":
		var rows []int
		if json.Unmarshal(in["rows"], &rows) != nil || len(rows) == 0 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "rows must list the candidate row numbers that are AI spend", "rows")
			return
		}
		parts := make([]string, len(rows))
		for i, r := range rows {
			parts[i] = strconv.Itoa(r)
		}
		fields["rows"] = strings.Join(parts, ",")
		ov := map[string]json.RawMessage{}
		for ref, fs := range latest {
			if cc, ok := fs["amount_inr"]; ok {
				ov[ref] = cc.CorrectedValue
			}
		}
		if len(ov) > 0 {
			fields["overrides"] = string(mustJSON(ov))
		}
		if raw, ok := in["defaults"]; ok && !isNull(raw) {
			fields["defaults"] = string(raw)
		}
	default:
		var keep []string
		if json.Unmarshal(in["keep"], &keep) != nil || len(keep) == 0 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "keep must list the pilot ids to import", "keep")
			return
		}
		fields["keep"] = strings.Join(keep, ",")
		ov := map[string]map[string]json.RawMessage{}
		for ref, fs := range latest {
			for f, cc := range fs {
				if ov[ref] == nil {
					ov[ref] = map[string]json.RawMessage{}
				}
				ov[ref][f] = cc.CorrectedValue
			}
		}
		if len(ov) > 0 {
			fields["overrides"] = string(mustJSON(ov))
		}
	}
	body, ctype, err := rules.Multipart(fields, j.Filename, j.ContentType, j.Content)
	if err != nil {
		fail(c, http.StatusInternalServerError, "internal_error", "could not prepare the upload for the engine", "")
		return
	}
	r, err := h.rules.DoUpload(c.Request.Context(), ConfirmPath[j.Kind], body, ctype, reqID(c))
	if !h.upstreamOK(c, r, err) {
		return
	}
	saved, err := h.uploads.ConfirmUpload(c.Request.Context(), id, r.Body)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "upload.confirm", "upload", id, gin.H{"kind": j.Kind, "corrections": len(corr)})
	c.JSON(http.StatusOK, gin.H{"upload": saved, "result": json.RawMessage(r.Body)})
}
