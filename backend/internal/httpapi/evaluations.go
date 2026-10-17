package httpapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// The evaluations audit log: every dual-view evaluation is recorded; these endpoints read it back.

func (h *Handlers) listEvaluations(c *gin.Context) {
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		limit = 20
	}
	list, derr := h.store.ListEvaluations(c.Request.Context(), limit)
	if derr != nil {
		h.storeErr(c, derr)
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
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "25"))
	if err != nil || limit < 1 {
		limit = 25
	}
	if limit > 50 {
		limit = 50
	}
	events, derr := h.store.HistoryTimeline(c.Request.Context(), limit)
	if derr != nil {
		h.storeErr(c, derr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "count": len(events)})
}
