package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
)

// The audit trail: every successful edit and export is recorded after it succeeds. Recording is
// best-effort, like the evaluation history: a failed write is logged with the request id and never
// turns a successful change into a failed response. Details hold identifiers and status values only.

var auditEntityTypes = map[string]bool{
	"approval": true, "evaluation": true, "evidence": true, "export": true, "interview": true, "member": true,
	"observation": true, "plan": true, "portfolio": true, "proposal": true, "settings": true, "snapshot": true,
	"upload": true, "workspace": true,
}

// record writes one audit event for the current request. entityID may be empty.
func (h *Handlers) record(c *gin.Context, action, entityType, entityID string, details gin.H) {
	h.recordIn(c.Request.Context(), c, action, entityType, entityID, details)
}

// recordIn is record with an explicit context (e.g. the workspace a request just created).
func (h *Handlers) recordIn(ctx context.Context, c *gin.Context, action, entityType, entityID string, details gin.H) {
	var id *string
	if entityID != "" {
		id = &entityID
	}
	raw := json.RawMessage(`{}`)
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			raw = b
		}
	}
	err := h.audit.AddAuditEvent(ctx, model.AuditEventIn{
		Action: action, EntityType: entityType, EntityID: id, RequestID: reqID(c), Details: raw,
	})
	if err != nil {
		h.log.Warn("could not write audit event", "action", action, "err", err, "request_id", reqID(c))
	}
}

// listAudit — GET /api/v1/audit?entity_type=&entity_id=&limit= (limit clamped to 1–50, default 25).
func (h *Handlers) listAudit(c *gin.Context) {
	limit := queryLimit(c, 25)
	entityType, entityID := c.Query("entity_type"), c.Query("entity_id")
	if entityType != "" && !auditEntityTypes[entityType] {
		types := make([]string, 0, len(auditEntityTypes))
		for t := range auditEntityTypes {
			types = append(types, t)
		}
		sort.Strings(types)
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "entity_type must be one of: "+strings.Join(types, ", "), "entity_type")
		return
	}
	if entityID != "" && entityType == "" {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "entity_id needs entity_type", "entity_id")
		return
	}
	if len(entityID) > 100 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "entity_id must be at most 100 characters", "entity_id")
		return
	}
	events, err := h.audit.ListAuditEvents(c.Request.Context(), model.AuditFilter{EntityType: entityType, EntityID: entityID, Limit: limit})
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "count": len(events)})
}
