package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
)

type ObjectUploads interface {
	CreateObjectUpload(ctx context.Context, in model.ObjectUploadIn) (*model.Upload, bool, error)
	CompleteObjectUpload(ctx context.Context, id string) (*model.Upload, error)
}

type objectUploadRequest struct {
	Filename     string `json:"filename"`
	Kind         string `json:"kind"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentType  string `json:"content_type"`
	Unit         string `json:"unit,omitempty"`
	Period       string `json:"period,omitempty"`
	AmountColumn string `json:"amount_column,omitempty"`
	SHA256       string `json:"sha256"`
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (h *Handlers) createObjectUpload(c *gin.Context) {
	var req objectUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "request body must be valid JSON", "")
		return
	}
	if !uploadKinds[req.Kind] {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "kind must be balance_sheet or pilots", "kind")
		return
	}
	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(req.Filename), "\\", "/"))
	if name == "" || name == "." || len(name) > 200 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "the file needs a name of at most 200 characters", "filename")
		return
	}
	if req.SizeBytes <= 0 || req.SizeBytes > h.cfg.UploadMaxBytes {
		fail(c, http.StatusRequestEntityTooLarge, "payload_too_large", fmt.Sprintf("uploads must be 1 to %d bytes", h.cfg.UploadMaxBytes), "size_bytes")
		return
	}
	format, expectedType, err := formatFromName(name)
	if err != nil {
		fail(c, http.StatusUnsupportedMediaType, "unsupported_file", err.Error(), "filename")
		return
	}
	opts := map[string]string{}
	if req.Unit != "" {
		if !uploadUnits[req.Unit] {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "unit is not supported", "unit")
			return
		}
		opts["unit"] = req.Unit
	}
	if req.Period != "" {
		if !uploadPeriods[req.Period] {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "period is not supported", "period")
			return
		}
		opts["period"] = req.Period
	}
	if req.AmountColumn != "" {
		if len(req.AmountColumn) > 100 || req.Kind != "balance_sheet" {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "amount_column is a balance-sheet header of at most 100 characters", "amount_column")
			return
		}
		opts["amount_column"] = req.AmountColumn
	}
	contentType := expectedType
	sha := strings.ToLower(strings.TrimSpace(req.SHA256))
	if len(sha) != 64 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "sha256 must be a 64-character hexadecimal checksum", "sha256")
		return
	}
	if _, err := hex.DecodeString(sha); err != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "sha256 must be hexadecimal", "sha256")
		return
	}
	id, err := newUUID()
	if err != nil {
		fail(c, http.StatusInternalServerError, "id_generation_failed", "could not create the upload id", "")
		return
	}
	key := fmt.Sprintf("workspaces/%s/documents/%s/original", model.WorkspaceID(c.Request.Context()), id)
	optJSON, _ := json.Marshal(opts)
	u, created, err := h.objectUploads.CreateObjectUpload(c.Request.Context(), model.ObjectUploadIn{
		ID: id, Kind: req.Kind, Filename: name, ContentType: contentType, Format: format,
		SizeBytes: int(req.SizeBytes), SHA256: sha, ObjectKey: key, Options: optJSON,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusOK, gin.H{"upload": u, "duplicate": true})
		return
	}
	url, headers, err := h.objects.PresignPut(c.Request.Context(), key, contentType, sha, req.SizeBytes, h.cfg.PresignedURLExpiry)
	if err != nil {
		h.log.Error("could not presign document upload", "err", err, "request_id", reqID(c))
		fail(c, http.StatusBadGateway, "storage_unavailable", "document storage is unavailable", "")
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"upload": u, "duplicate": false, "upload_url": url, "headers": headers})
}

func (h *Handlers) completeObjectUpload(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if u.ObjectKey == nil || u.StorageProvider != "s3" {
		fail(c, http.StatusConflict, "legacy_upload", "this upload uses legacy database storage", "")
		return
	}
	info, err := h.objects.Head(c.Request.Context(), *u.ObjectKey)
	if err != nil || info.Size != int64(u.SizeBytes) || info.ContentType != u.ContentType ||
		strings.ToLower(info.Metadata["sha256"]) != strings.ToLower(u.SHA256) {
		fail(c, http.StatusUnprocessableEntity, "upload_verification_failed", "the uploaded object is missing or its metadata does not match", "")
		return
	}
	body, err := h.objects.Open(c.Request.Context(), *u.ObjectKey)
	if err != nil {
		fail(c, http.StatusBadGateway, "storage_unavailable", "document storage is unavailable", "")
		return
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, int64(u.SizeBytes)+1))
	if err != nil || int64(len(content)) != int64(u.SizeBytes) {
		fail(c, http.StatusUnprocessableEntity, "upload_verification_failed", "the uploaded object size could not be verified", "")
		return
	}
	format, _, err := sniffFormat(u.Filename, content)
	if err != nil {
		fail(c, http.StatusUnsupportedMediaType, "unsupported_file", err.Error(), "file")
		return
	}
	sum := sha256.Sum256(content)
	if hex.EncodeToString(sum[:]) != u.SHA256 || format != u.Format {
		fail(c, http.StatusUnprocessableEntity, "upload_verification_failed", "the uploaded object checksum or format does not match", "")
		return
	}
	completed, err := h.objectUploads.CompleteObjectUpload(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		// A prior successful completion is idempotent; a missing pending row is not.
		if u.Status == "received" || u.Status == "processing" || u.Status == "parsed" || u.Status == "confirmed" {
			c.JSON(http.StatusOK, gin.H{"upload": u, "duplicate": true})
			return
		}
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if h.notify != nil {
		h.notify()
	}
	c.JSON(http.StatusOK, gin.H{"upload": completed, "duplicate": false})
}

func formatFromName(name string) (string, string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return "pdf", pdfMIME, nil
	case ".xlsx":
		return "xlsx", xlsxMIME, nil
	case ".xls":
		return "xls", xlsMIME, nil
	case ".docx":
		return "docx", docxMIME, nil
	case ".csv":
		return "csv", "text/csv", nil
	default:
		return "", "", errors.New("only PDF, XLSX, XLS, DOCX and CSV files are accepted")
	}
}
