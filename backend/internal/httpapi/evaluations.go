package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// The evaluations audit log: every dual-view evaluation is recorded; these endpoints read it back.

// queryLimit reads ?limit= clamped to 1–50: missing, invalid, zero or negative values become def.
func queryLimit(c *gin.Context, def int) int {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", strconv.Itoa(def)))
	if err != nil || limit < 1 {
		limit = def
	}
	if limit > 50 {
		limit = 50
	}
	return limit
}

// listEvaluations — limit is clamped to 1–50 (default 20), matching the store.
func (h *Handlers) listEvaluations(c *gin.Context) {
	list, derr := h.store.ListEvaluations(c.Request.Context(), queryLimit(c, 20))
	if derr != nil {
		h.storeErr(c, derr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evaluations": list})
}

// listProposalEvaluations — GET /api/v1/proposals/:id/evaluations: the evaluations run for one
// proposal with the inputs version each used. limit is clamped to 1–50 (default 20).
func (h *Handlers) listProposalEvaluations(c *gin.Context) {
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	if _, err := h.store.GetProposal(c.Request.Context(), id); err != nil {
		h.storeErr(c, err) // unknown proposal → 404
		return
	}
	list, err := h.store.ListProposalEvaluations(c.Request.Context(), id, queryLimit(c, 20))
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"evaluations": list})
}

func (h *Handlers) getEvaluation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a positive integer", "id")
		return
	}
	e, derr := h.store.GetEvaluation(c.Request.Context(), id)
	if derr != nil {
		h.storeErr(c, derr)
		return
	}
	c.JSON(http.StatusOK, e)
}

// timeline merges the evaluation log and the stop-rule decisions into one global timeline
// (distinct from the per-pilot /history/:id view). limit is clamped to 1–50 (default 25).
func (h *Handlers) timeline(c *gin.Context) {
	events, derr := h.store.HistoryTimeline(c.Request.Context(), queryLimit(c, 25))
	if derr != nil {
		h.storeErr(c, derr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "count": len(events)})
}
