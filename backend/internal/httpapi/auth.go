package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/model"
)

// Security model (B5):
//   - AUTH_MODE=off: no sign-in, every request works in the default workspace as admin (local development,
//     the integration script and the browser checks).
//   - AUTH_MODE=required: every /api/v1 call carries "Authorization: Bearer <token>". The token resolves to a
//     user; X-Workspace-ID picks the workspace (optional when the user belongs to exactly one). The user's
//     role there gates the route: viewer < analyst < admin. Every store call is scoped to that workspace.
//   - Rate limiting applies per client IP in both modes.

var roleRank = map[string]int{"viewer": 1, "analyst": 2, "admin": 3}

const (
	ctxRole        = "role"
	ctxHasWorkspce = "has_workspace"
)

// Platform is the store surface for workspaces, people, settings, snapshots and approvals.
type Platform interface {
	AuthenticateToken(ctx context.Context, raw string) (*model.TokenAuth, error)
	MemberRole(ctx context.Context, workspaceID, userID string) (string, error)
	UserWorkspaces(ctx context.Context, userID string) ([]model.Workspace, error)
	CreateWorkspace(ctx context.Context, name, adminUserID string) (*model.Workspace, error)
	ListMembers(ctx context.Context) ([]model.Member, error)
	SetMember(ctx context.Context, email, role string) (*model.Member, error)
	RemoveMember(ctx context.Context, userID string) error
	CreateToken(ctx context.Context, userID, label, raw string) (*model.APIToken, error)
	ListTokens(ctx context.Context, userID string) ([]model.APIToken, error)
	RevokeToken(ctx context.Context, userID, tokenID string) error
	GetSettings(ctx context.Context) (json.RawMessage, error)
	PutSettings(ctx context.Context, settings json.RawMessage) (json.RawMessage, error)
	SaveSnapshot(ctx context.Context, in model.SnapshotIn) (*model.Snapshot, error)
	GetSnapshot(ctx context.Context, id string) (*model.Snapshot, error)
	ListSnapshots(ctx context.Context, kind string, limit int) ([]model.Snapshot, error)
	AddApproval(ctx context.Context, in model.ApprovalIn) (*model.Approval, error)
	ListApprovals(ctx context.Context, proposalID string) ([]model.Approval, error)
	ProposalsForEngine(ctx context.Context, ids []string) ([]model.Proposal, error)
	ProposalVersions(ctx context.Context, ids []string) (map[string]int, error)
	LatestProposalObservation(ctx context.Context, proposalID string) (*model.Observation, error)
}

func bearer(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// authenticate resolves the caller and the workspace for every /api/v1 request.
func (h *Handlers) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.cfg.AuthMode != "required" {
			c.Request = c.Request.WithContext(model.WithWorkspace(c.Request.Context(), model.DefaultWorkspaceID))
			c.Set(ctxRole, "admin")
			c.Set(ctxHasWorkspce, true)
			c.Next()
			return
		}
		raw := bearer(c.GetHeader("Authorization"))
		if raw == "" {
			c.Header("WWW-Authenticate", `Bearer realm="c23"`)
			fail(c, http.StatusUnauthorized, "auth_required", "sign in: send Authorization: Bearer <token>", "")
			return
		}
		if h.platform == nil {
			fail(c, http.StatusServiceUnavailable, "auth_unavailable", "authentication is not configured", "")
			return
		}
		ta, err := h.platform.AuthenticateToken(c.Request.Context(), raw)
		if errors.Is(err, model.ErrNotFound) {
			c.Header("WWW-Authenticate", `Bearer realm="c23", error="invalid_token"`)
			fail(c, http.StatusUnauthorized, "invalid_token", "the token is unknown or revoked", "")
			return
		}
		if err != nil {
			h.storeErr(c, err)
			return
		}
		p := &model.Principal{UserID: ta.UserID, Email: ta.Email, TokenID: ta.TokenID}
		ctx := model.WithPrincipal(c.Request.Context(), p)

		ws := strings.TrimSpace(c.GetHeader("X-Workspace-ID"))
		role := ""
		if ws == "" {
			list, err := h.platform.UserWorkspaces(ctx, ta.UserID)
			if err != nil {
				h.storeErr(c, err)
				return
			}
			if len(list) == 1 {
				ws, role = list[0].ID, list[0].Role
			}
		} else {
			if !validUUID(ws) {
				fail(c, http.StatusBadRequest, "invalid_workspace", "X-Workspace-ID must be a workspace UUID", "X-Workspace-ID")
				return
			}
			role, err = h.platform.MemberRole(ctx, ws, ta.UserID)
			if errors.Is(err, model.ErrNotFound) {
				// Same answer whether the workspace exists or not: no probing other organisations' ids.
				fail(c, http.StatusForbidden, "not_a_member", "you are not a member of that workspace", "X-Workspace-ID")
				return
			}
			if err != nil {
				h.storeErr(c, err)
				return
			}
		}
		if ws != "" {
			ctx = model.WithWorkspace(ctx, ws)
			p.Role = role
			c.Set(ctxRole, role)
			c.Set(ctxHasWorkspce, true)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// requireWorkspace stops calls that need a workspace when none could be chosen.
func requireWorkspace() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !c.GetBool(ctxHasWorkspce) {
			fail(c, http.StatusBadRequest, "workspace_required",
				"choose a workspace: send X-Workspace-ID (GET /api/v1/workspaces lists yours)", "X-Workspace-ID")
			return
		}
		c.Next()
	}
}

// need allows the route for the given role and above.
func need(role string) gin.HandlerFunc {
	min := roleRank[role]
	return func(c *gin.Context) {
		if roleRank[c.GetString(ctxRole)] < min {
			fail(c, http.StatusForbidden, "forbidden", "this action needs the "+role+" role in this workspace", "")
			return
		}
		c.Next()
	}
}

// ---------------------------------------------------------------- rate limiting

// rateLimiter is a token bucket per client IP: perMin tokens a minute, up to burst at once.
type rateLimiter struct {
	mu      sync.Mutex
	perSec  float64
	burst   float64
	buckets map[string]*bucket
	calls   int
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMin, burst int) *rateLimiter {
	return &rateLimiter{perSec: float64(perMin) / 60, burst: float64(burst), buckets: map[string]*bucket{}, now: time.Now}
}

// allow takes one token for key; when empty it returns how long until the next token.
func (r *rateLimiter) allow(key string) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.calls++
	if r.calls%1000 == 0 { // forget clients idle for 10 minutes
		for k, b := range r.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(r.buckets, k)
			}
		}
	}
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	b.tokens = math.Min(r.burst, b.tokens+now.Sub(b.last).Seconds()*r.perSec)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / r.perSec * float64(time.Second))
}

func rateLimit(r *rateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if r == nil || c.Request.URL.Path == "/health" || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		ok, wait := r.allow(c.ClientIP())
		if !ok {
			secs := int(math.Ceil(wait.Seconds()))
			if secs < 1 {
				secs = 1
			}
			c.Header("Retry-After", strconv.Itoa(secs))
			fail(c, http.StatusTooManyRequests, "rate_limited", "too many requests; try again in "+strconv.Itoa(secs)+"s", "")
			return
		}
		c.Next()
	}
}

// ---------------------------------------------------------------- account, workspaces, members, tokens

var emailRe = regexp.MustCompile(`^[^@\s]{1,100}@[^@\s]{1,100}$`)

func newRawToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return "c23_" + base64.RawURLEncoding.EncodeToString(b)
}

// me — GET /api/v1/me: who is calling, in which workspace, with which role.
func (h *Handlers) me(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		c.JSON(http.StatusOK, gin.H{"auth_mode": "off", "user": nil, "role": "admin",
			"workspace":  gin.H{"id": model.DefaultWorkspaceID, "name": "Default workspace"},
			"workspaces": []gin.H{{"id": model.DefaultWorkspaceID, "name": "Default workspace", "role": "admin"}}})
		return
	}
	p := model.PrincipalFrom(c.Request.Context())
	list, err := h.platform.UserWorkspaces(c.Request.Context(), p.UserID)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	var current any
	if c.GetBool(ctxHasWorkspce) {
		ws := model.WorkspaceID(c.Request.Context())
		for _, w := range list {
			if w.ID == ws {
				current = w
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"auth_mode": "required", "user": gin.H{"id": p.UserID, "email": p.Email},
		"role": c.GetString(ctxRole), "workspace": current, "workspaces": list})
}

func (h *Handlers) listWorkspaces(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		c.JSON(http.StatusOK, gin.H{"workspaces": []gin.H{{"id": model.DefaultWorkspaceID, "name": "Default workspace", "role": "admin"}}})
		return
	}
	list, err := h.platform.UserWorkspaces(c.Request.Context(), model.PrincipalFrom(c.Request.Context()).UserID)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"workspaces": list})
}

// createWorkspace — POST /api/v1/workspaces {name}: any signed-in user may start a workspace and is its admin.
func (h *Handlers) createWorkspace(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		fail(c, http.StatusConflict, "auth_off", "workspaces need AUTH_MODE=required; with auth off everything uses the default workspace", "")
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	var name string
	if json.Unmarshal(in["name"], &name) != nil || strings.TrimSpace(name) == "" || len(name) > 80 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "name is required (1-80 characters)", "name")
		return
	}
	p := model.PrincipalFrom(c.Request.Context())
	w, err := h.platform.CreateWorkspace(c.Request.Context(), name, p.UserID)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.recordIn(model.WithWorkspace(c.Request.Context(), w.ID), c, "workspace.create", "workspace", w.ID, nil)
	c.JSON(http.StatusCreated, w)
}

func (h *Handlers) listMembers(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		c.JSON(http.StatusOK, gin.H{"members": []any{}, "note": "auth is off: there are no members, everyone acts as admin"})
		return
	}
	list, err := h.platform.ListMembers(c.Request.Context())
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": list})
}

// setMember — POST /api/v1/members {email, role}: add someone (created when new) or change their role.
func (h *Handlers) setMember(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		fail(c, http.StatusConflict, "auth_off", "members need AUTH_MODE=required", "")
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	var email, role string
	if json.Unmarshal(in["email"], &email) != nil || !emailRe.MatchString(strings.TrimSpace(email)) {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "email must be an email address", "email")
		return
	}
	if json.Unmarshal(in["role"], &role) != nil || roleRank[role] == 0 {
		fail(c, http.StatusUnprocessableEntity, "invalid_value", "role must be viewer, analyst or admin", "role")
		return
	}
	m, err := h.platform.SetMember(c.Request.Context(), strings.TrimSpace(email), role)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "member.set", "member", m.UserID, gin.H{"role": role})
	c.JSON(http.StatusOK, m)
}

func (h *Handlers) removeMember(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		fail(c, http.StatusConflict, "auth_off", "members need AUTH_MODE=required", "")
		return
	}
	id := c.Param("user_id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "user_id must be a UUID", "user_id")
		return
	}
	err := h.platform.RemoveMember(c.Request.Context(), id)
	if errors.Is(err, model.ErrConflict) {
		fail(c, http.StatusConflict, "last_admin", "a workspace keeps at least one admin", "user_id")
		return
	}
	if err != nil {
		h.storeErr(c, err)
		return
	}
	h.record(c, "member.remove", "member", id, nil)
	c.Status(http.StatusNoContent)
}

// createToken — POST /api/v1/tokens {label}: a new token for the caller. The raw token is returned once.
func (h *Handlers) createToken(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		fail(c, http.StatusConflict, "auth_off", "tokens need AUTH_MODE=required", "")
		return
	}
	in, ok := h.readObject(c)
	if !ok {
		return
	}
	label := "api token"
	if raw, present := in["label"]; present {
		if json.Unmarshal(raw, &label) != nil || strings.TrimSpace(label) == "" || len(label) > 80 {
			fail(c, http.StatusUnprocessableEntity, "invalid_value", "label must be 1-80 characters", "label")
			return
		}
	}
	p := model.PrincipalFrom(c.Request.Context())
	raw := newRawToken()
	t, err := h.platform.CreateToken(c.Request.Context(), p.UserID, strings.TrimSpace(label), raw)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"token": raw, "id": t.ID, "label": t.Label, "created_at": t.CreatedAt,
		"note": "Store this token now: it is shown once and only its hash is kept."})
}

func (h *Handlers) listTokens(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		c.JSON(http.StatusOK, gin.H{"tokens": []any{}})
		return
	}
	list, err := h.platform.ListTokens(c.Request.Context(), model.PrincipalFrom(c.Request.Context()).UserID)
	if err != nil {
		h.storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tokens": list})
}

func (h *Handlers) revokeToken(c *gin.Context) {
	if h.cfg.AuthMode != "required" {
		fail(c, http.StatusConflict, "auth_off", "tokens need AUTH_MODE=required", "")
		return
	}
	id := c.Param("id")
	if !validUUID(id) {
		fail(c, http.StatusUnprocessableEntity, "invalid_id", "id must be a UUID", "id")
		return
	}
	if err := h.platform.RevokeToken(c.Request.Context(), model.PrincipalFrom(c.Request.Context()).UserID, id); err != nil {
		h.storeErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
