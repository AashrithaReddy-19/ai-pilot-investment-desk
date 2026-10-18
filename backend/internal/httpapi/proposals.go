package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
)

var (
	uuidRe     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	proposalRe = regexp.MustCompile(`^[A-Za-z0-9_\- ]{1,80}$`)

	evidenceKinds = map[string]bool{
		"interview": true, "measurement": true, "document": true, "assumption": true,
	}
	evidenceQualities = map[string]bool{
		"observed": true, "assumed": true, "calculated": true, "fixture": true,
	}
)

func validUUID(s string) bool { return uuidRe.MatchString(s) }

// isJSONObject checks the bytes decode to a JSON object ({...}).
func isJSONObject(raw json.RawMessage) bool {
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil
}

// ---------------------------------------------------------------- proposals

func (h *Handlers) createProposal(c *gin.Context) {
	b, ok := h.readBody(c)
	if !ok {
		return
	}
	var in struct {
		Name   *string         `json:"name"`
		Inputs json.RawMessage `json:"inputs"`
		Notes  *string         `json:"notes"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "request body does not match the proposal shape", "")
		return
	}
	name := strings.TrimSpace(derefString(in.Name))
	if !validProposalName(name) {
		fail(c, http.StatusUnprocessableEntity, "invalid_name",
			"name must be 1-80 chars: letters, digits, spaces, hyphens or underscores", "name")
		return
	}
	inputs := in.Inputs
	if len(inputs) == 0 {
		inputs = json.RawMessage(`{}`)
	}
	if !isJSONObject(inputs) {
		fail(c, http.StatusUnprocessableEntity, "invalid_inputs", "inputs must be a JSON object", "inputs")
		return
	}
	if in.Notes != nil && len(*in.Notes) > 2000 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "notes must be at most 2000 characters", "notes")
		return
	}
	p, err := h.store.CreateProposal(c.Request.Context(), model.ProposalIn{Name: name, Inputs: inputs, Notes: in.Notes})
	if err != nil {
		if isUniqueViolation(err) {
			fail(c, http.StatusUnprocessableEntity, "duplicate_name", fmt.Sprintf("a proposal named %q already exists", name), "name")
			return
		}
		h.storeErr(c, err)
		return
	}
	h.record(c, "proposal.create", "proposal", p.ID, gin.H{"version": p.Version})
	c.JSON(http.StatusCreated, p)
}

func (h *Handlers) listProposals(c *gin.Context) {
	include := c.Query("include_archived") == "true"
	list, err := h.store.ListProposals(c.Request.Context(), include)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"proposals": list})
}

func (h *Handlers) getProposal(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	p, err := h.store.GetProposal(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (h *Handlers) updateProposal(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	b, ok := h.readBody(c)
	if !ok {
		return
	}
	var in struct {
		Name   *string         `json:"name"`
		Inputs json.RawMessage `json:"inputs"`
		Notes  *string         `json:"notes"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "request body does not match the proposal shape", "")
		return
	}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if !validProposalName(*in.Name) {
			fail(c, http.StatusUnprocessableEntity, "invalid_name",
				"name must be 1-80 chars: letters, digits, spaces, hyphens or underscores", "name")
			return
		}
	}
	if in.Inputs != nil {
		if !isJSONObject(in.Inputs) {
			fail(c, http.StatusUnprocessableEntity, "invalid_inputs", "inputs must be a JSON object", "inputs")
			return
		}
	}
	if in.Notes != nil && len(*in.Notes) > 2000 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "notes must be at most 2000 characters", "notes")
		return
	}
	p, err := h.store.UpdateProposal(c.Request.Context(), id, model.ProposalIn{
		Name: derefString(in.Name), Inputs: in.Inputs, Notes: in.Notes,
	})
	if err != nil {
		if isUniqueViolation(err) {
			fail(c, http.StatusUnprocessableEntity, "duplicate_name", "another proposal already uses that name", "name")
			return
		}
		h.storeErr(c, err)
		return
	}
	changed := []string{}
	for field, set := range map[string]bool{"inputs": in.Inputs != nil, "name": in.Name != nil, "notes": in.Notes != nil} {
		if set {
			changed = append(changed, field)
		}
	}
	sort.Strings(changed)
	h.record(c, "proposal.update", "proposal", p.ID, gin.H{"fields": changed, "version": p.Version})
	c.JSON(http.StatusOK, p)
}

func (h *Handlers) archiveProposal(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	restore := c.FullPath() == "/api/v1/proposals/:id/restore"
	p, err := h.store.ArchiveProposal(c.Request.Context(), id, restore)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	action := "proposal.archive"
	if restore {
		action = "proposal.restore"
	}
	h.record(c, action, "proposal", p.ID, nil)
	c.JSON(http.StatusOK, p)
}

func (h *Handlers) deleteProposal(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	if err := h.store.DeleteProposal(c.Request.Context(), id); err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "proposal.delete", "proposal", id, nil)
	c.Status(http.StatusNoContent)
}

func validProposalName(s string) bool {
	return len(s) >= 1 && len(s) <= 80 && proposalRe.MatchString(s)
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------- evidence

func (h *Handlers) addEvidence(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	b, ok := h.readBody(c)
	if !ok {
		return
	}
	var in struct {
		Kind    *string `json:"kind"`
		Source  *string `json:"source"`
		Quality *string `json:"quality"`
		Note    *string `json:"note"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		fail(c, http.StatusBadRequest, "invalid_json", "request body does not match the evidence shape", "")
		return
	}
	switch {
	case in.Kind == nil || !evidenceKinds[strings.TrimSpace(*in.Kind)]:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "kind must be one of interview, measurement, document, assumption", "kind")
		return
	case in.Source == nil || strings.TrimSpace(*in.Source) == "" || len(*in.Source) > 200:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "source is required (max 200 chars)", "source")
		return
	case in.Quality != nil && !evidenceQualities[strings.TrimSpace(*in.Quality)]:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "quality must be one of observed, assumed, calculated, fixture", "quality")
		return
	case in.Note != nil && len(*in.Note) > 2000:
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "note must be at most 2000 characters", "note")
		return
	}
	kind, quality := strings.TrimSpace(*in.Kind), (*string)(nil)
	if in.Quality != nil {
		q := strings.TrimSpace(*in.Quality)
		quality = &q
	}
	e, err := h.store.AddEvidence(c.Request.Context(), model.EvidenceIn{
		ProposalID: id, Kind: kind, Source: strings.TrimSpace(*in.Source), Quality: quality, Note: in.Note,
	})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "evidence.add", "evidence", strconv.FormatInt(e.ID, 10), gin.H{"proposal_id": id, "kind": kind})
	c.JSON(http.StatusCreated, e)
}

func (h *Handlers) listEvidence(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	list, err := h.store.ListEvidence(c.Request.Context(), id)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evidence": list})
}

// ---------------------------------------------------------------- helpers shared with handlers.go

func isUniqueViolation(err error) bool {
	// pgx returns *pgconn.PgError with Code == "23505"; string match keeps the fake store tests simple.
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505")
}

func isFKViolation(err error) bool {
	return err != nil && (errors.Is(err, model.ErrUnknownProposal) || strings.Contains(err.Error(), "SQLSTATE 23503"))
}
