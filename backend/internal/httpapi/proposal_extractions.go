package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
	"c23desk/internal/proposalextract"
)

func (h *Handlers) extractionDeps(c *gin.Context, id string) (*model.Upload, []byte, bool) {
	if h.extractions == nil {
		fail(c, http.StatusServiceUnavailable, "not_configured", "proposal extraction is not configured", "")
		return nil, nil, false
	}
	u, err := h.uploads.GetUpload(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "upload was not found in this workspace", "id")
		return nil, nil, false
	}
	if err != nil {
		h.storeErr(c, err)
		return nil, nil, false
	}
	job, err := h.uploads.UploadContent(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return nil, nil, false
	}
	if job.StorageProvider == "s3" {
		if h.objects == nil || job.ObjectKey == "" {
			fail(c, http.StatusServiceUnavailable, "not_configured", "document storage is not configured", "")
			return nil, nil, false
		}
		rc, err := h.objects.Open(c.Request.Context(), job.ObjectKey)
		if err != nil {
			h.storeErr(c, err)
			return nil, nil, false
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, int64(extractionMaxBytes)+1))
		if err != nil || len(b) > extractionMaxBytes {
			fail(c, http.StatusUnprocessableEntity, "document_too_large", "the document cannot be extracted within the processing limit", "id")
			return nil, nil, false
		}
		return u, b, true
	}
	return u, job.Content, true
}

func (h *Handlers) getProposalExtraction(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	if h.extractions == nil {
		fail(c, http.StatusServiceUnavailable, "not_configured", "proposal extraction is not configured", "")
		return
	}
	out, err := h.extractions.GetProposalExtraction(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "proposal extraction has not been started", "id")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) startProposalExtraction(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	if h.extractions == nil {
		fail(c, http.StatusServiceUnavailable, "not_configured", "proposal extraction is not configured", "")
		return
	}
	if existing, err := h.extractions.GetProposalExtraction(c.Request.Context(), id); err == nil && existing.Status != "failed" {
		c.JSON(http.StatusOK, existing)
		return
	}
	u, content, ok := h.extractionDeps(c, id)
	if !ok {
		return
	}
	processing, err := h.extractions.SaveProposalExtraction(c.Request.Context(), id, "processing", []byte(`{}`), []byte(`[]`), nil)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	workerCtx := context.WithoutCancel(c.Request.Context())
	go func() {
		mode := "deterministic"
		extract := proposalextract.Extract
		if h.aiExtractor != nil {
			mode = "bedrock"
			extract = h.aiExtractor.Extract
		}
		result, extractErr := extract(workerCtx, u.Filename, content)
		if extractErr != nil {
			message := extractErr.Error()
			if _, saveErr := h.extractions.SaveProposalExtraction(workerCtx, id, "failed", []byte(`{}`), []byte(`[]`), &message); saveErr != nil && h.log != nil {
				h.log.Error("proposal extraction failure could not be persisted", "upload_id", id, "error", saveErr)
			}
			return
		}
		result.Warnings = append(result.Warnings, "extraction_mode: "+mode)
		fields, _ := json.Marshal(result.Fields)
		warnings, _ := json.Marshal(result.Warnings)
		if _, saveErr := h.extractions.SaveProposalExtraction(workerCtx, id, "review", fields, warnings, nil); saveErr != nil && h.log != nil {
			h.log.Error("proposal extraction result could not be persisted", "upload_id", id, "error", saveErr)
		}
	}()
	c.JSON(http.StatusAccepted, processing)
}

func (h *Handlers) correctProposalExtraction(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	if h.extractions == nil {
		fail(c, http.StatusServiceUnavailable, "not_configured", "proposal extraction is not configured", "")
		return
	}
	current, err := h.extractions.GetProposalExtraction(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "proposal extraction has not been started", "id")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if current.Status != "review" {
		fail(c, http.StatusConflict, "not_reviewable", "extraction corrections are available only when the extraction is in review", "id")
		return
	}
	var in struct {
		Field string          `json:"field"`
		Value json.RawMessage `json:"value"`
		Note  string          `json:"note"`
	}
	if b, good := h.readBody(c); good {
		if json.Unmarshal(b, &in) != nil {
			fail(c, http.StatusBadRequest, "invalid_json", "request body does not match the correction shape", "")
			return
		}
	}
	allowed := map[string]bool{"id": true, "task": true, "setup_cost_inr": true, "annual_benefit_inr": true, "annual_running_cost_inr": true, "benefit_type": true, "overlap_group": true, "adoption_pct": true, "readiness": true, "ramp_months": true, "delay_months": true}
	if !allowed[in.Field] || len(in.Value) == 0 {
		fail(c, http.StatusUnprocessableEntity, "invalid_field", "field must be one of the extracted proposal fields and value is required", "field")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(current.Fields, &fields); err != nil {
		fail(c, http.StatusConflict, "invalid_extraction", "stored extraction fields are invalid", "")
		return
	}
	var field map[string]any
	if raw, exists := fields[in.Field]; !exists || json.Unmarshal(raw, &field) != nil || field == nil {
		fail(c, http.StatusConflict, "invalid_extraction", "the requested extraction field is invalid", "field")
		return
	}
	var value any
	if json.Unmarshal(in.Value, &value) != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "value must be valid JSON", "value")
		return
	}
	if err := proposalextract.ValidateFieldValue(in.Field, value); err != nil {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", err.Error(), "value")
		return
	}
	field["value"], field["status"], field["explanation"] = value, "explicit", strings.TrimSpace(in.Note)
	evidence, _ := field["evidence"].([]any)
	evidence = append(evidence, map[string]any{"source": "human correction", "excerpt": strings.TrimSpace(in.Note), "original": ""})
	field["evidence"] = evidence
	raw, _ := json.Marshal(field)
	fields[in.Field] = raw
	all, _ := json.Marshal(fields)
	out, err := h.extractions.SaveProposalExtraction(c.Request.Context(), id, "review", all, current.Warnings, nil)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	if current.Status != "review" && current.Status != "confirmed" {
		fail(c, http.StatusConflict, "not_confirmable", "extraction must be in review before confirmation", "id")
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) confirmProposalExtraction(c *gin.Context) {
	id, ok := h.uploadParam(c)
	if !ok {
		return
	}
	if h.extractions == nil {
		fail(c, http.StatusServiceUnavailable, "not_configured", "proposal extraction is not configured", "")
		return
	}
	current, err := h.extractions.GetProposalExtraction(c.Request.Context(), id)
	if errors.Is(err, model.ErrNotFound) {
		fail(c, http.StatusNotFound, "not_found", "proposal extraction has not been started", "id")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	var fields map[string]proposalextract.Field
	if json.Unmarshal(current.Fields, &fields) != nil {
		fail(c, http.StatusConflict, "invalid_extraction", "stored extraction fields are invalid", "")
		return
	}
	if err := proposalextract.ValidateForConfirmation(fields); err != nil {
		field := ""
		if parts := strings.SplitN(err.Error(), " ", 2); len(parts) > 0 {
			field = parts[0]
		}
		fail(c, http.StatusConflict, "review_required", err.Error(), field)
		return
	}
	inputs := map[string]any{}
	for _, name := range []string{"setup_cost_inr", "annual_benefit_inr", "annual_running_cost_inr", "benefit_type", "overlap_group", "adoption_pct", "readiness", "ramp_months", "delay_months"} {
		key := name
		if name == "annual_running_cost_inr" {
			key = "ai_running_cost_inr"
		}
		inputs[key] = fields[name].Value
	}
	fieldsRaw, _ := json.Marshal(fields)
	out, err := h.extractions.SaveProposalExtraction(c.Request.Context(), id, "confirmed", fieldsRaw, current.Warnings, nil)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"extraction": out, "proposal": gin.H{"id": fields["id"].Value, "name": fields["task"].Value, "inputs": inputs}})
}

const extractionMaxBytes = 2 << 20
