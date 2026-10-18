package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

// fakePlatform: two organisations. Alice is admin of ws A; Victor is a viewer of A; Ana an analyst of A;
// Bob is admin of ws B only; Multi belongs to both. Every store call records the workspace it ran in.
const (
	wsA = "aaaaaaaa-0000-0000-0000-000000000001"
	wsB = "bbbbbbbb-0000-0000-0000-000000000002"
)

type fakePlatform struct {
	Platform
	sawWorkspace []string
	settings     json.RawMessage
	snapshots    map[string]*model.Snapshot
	proposals    []model.Proposal
	versions     map[string]int
	approvals    []model.ApprovalIn
}

var people = map[string]struct {
	user  string
	email string
	roles map[string]string
}{
	"tok-alice":  {"u-alice", "alice@a.test", map[string]string{wsA: "admin"}},
	"tok-victor": {"u-victor", "victor@a.test", map[string]string{wsA: "viewer"}},
	"tok-ana":    {"u-ana", "ana@a.test", map[string]string{wsA: "analyst"}},
	"tok-bob":    {"u-bob", "bob@b.test", map[string]string{wsB: "admin"}},
	"tok-multi":  {"u-multi", "multi@x.test", map[string]string{wsA: "analyst", wsB: "viewer"}},
}

func (f *fakePlatform) AuthenticateToken(_ context.Context, raw string) (*model.TokenAuth, error) {
	p, ok := people[raw]
	if !ok {
		return nil, model.ErrNotFound
	}
	return &model.TokenAuth{TokenID: "t-" + p.user, UserID: p.user, Email: p.email}, nil
}

func (f *fakePlatform) MemberRole(_ context.Context, ws, user string) (string, error) {
	for _, p := range people {
		if p.user == user {
			if r, ok := p.roles[ws]; ok {
				return r, nil
			}
		}
	}
	return "", model.ErrNotFound
}

func (f *fakePlatform) UserWorkspaces(_ context.Context, user string) ([]model.Workspace, error) {
	out := []model.Workspace{}
	for _, p := range people {
		if p.user == user {
			for _, ws := range []string{wsA, wsB} {
				if r, ok := p.roles[ws]; ok {
					out = append(out, model.Workspace{ID: ws, Name: "ws " + ws[:1], Role: r})
				}
			}
		}
	}
	return out, nil
}

// workspaceStore answers proposals/evidence calls and remembers which workspace each call ran in.
type workspaceStore struct {
	fakeStore
	seen *[]string
}

func (s workspaceStore) ListProposals(ctx context.Context, _ bool) ([]model.ProposalSummary, error) {
	*s.seen = append(*s.seen, model.WorkspaceID(ctx))
	return []model.ProposalSummary{}, nil
}
func (s workspaceStore) CreateProposal(ctx context.Context, in model.ProposalIn) (*model.Proposal, error) {
	*s.seen = append(*s.seen, model.WorkspaceID(ctx))
	return &model.Proposal{ID: "11111111-1111-1111-1111-111111111111", Name: in.Name, Status: "active", Version: 1}, nil
}
func (s workspaceStore) DeleteProposal(ctx context.Context, _ string) error {
	*s.seen = append(*s.seen, model.WorkspaceID(ctx))
	return nil
}

func newAuthRouter(st Store, p Platform, cfg func(*config.Config)) http.Handler {
	c := config.Config{AllowedOrigins: []string{"http://localhost:5173"}, RequestTimeout: 2 * time.Second,
		MaxBodyBytes: 1 << 16, AuthMode: "required"}
	if cfg != nil {
		cfg(&c)
	}
	return NewRouter(Deps{Cfg: c, Store: st, Rules: &fakeRules{do: ok200(`{"ok":true}`)}, Cache: fakeCache{},
		Platform: p, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestAuthRequiredRejectsMissingAndUnknownTokens(t *testing.T) {
	seen := []string{}
	r := newAuthRouter(workspaceStore{seen: &seen}, &fakePlatform{}, nil)
	w := do(r, "GET", "/api/v1/proposals", "")
	if w.Code != 401 || !contains(w.Body.String(), "auth_required") || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d %s", w.Code, w.Body.String())
	}
	w = do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer nope")
	if w.Code != 401 || !contains(w.Body.String(), "invalid_token") {
		t.Fatalf("bad token: %d %s", w.Code, w.Body.String())
	}
	if len(seen) != 0 {
		t.Fatal("the store must not be reached without a valid token")
	}
	// /health and /ready stay public for probes
	if w := do(r, "GET", "/health", ""); w.Code != 200 {
		t.Fatalf("health must be public: %d", w.Code)
	}
}

func TestWorkspaceIsChosenAndIsolated(t *testing.T) {
	seen := []string{}
	r := newAuthRouter(workspaceStore{seen: &seen}, &fakePlatform{}, nil)
	// one membership: the workspace is implied
	if w := do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-bob"); w.Code != 200 {
		t.Fatalf("single-workspace user: %d %s", w.Code, w.Body.String())
	}
	// two memberships and no header: must choose
	w := do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-multi")
	if w.Code != 400 || !contains(w.Body.String(), "workspace_required") {
		t.Fatalf("multi-workspace user without header: %d %s", w.Code, w.Body.String())
	}
	if w := do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-multi", "X-Workspace-ID", wsA); w.Code != 200 {
		t.Fatalf("multi with header: %d %s", w.Code, w.Body.String())
	}
	// cross-organisation access: Bob asking for workspace A is refused before any store call
	before := len(seen)
	w = do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-bob", "X-Workspace-ID", wsA)
	if w.Code != 403 || !contains(w.Body.String(), "not_a_member") || len(seen) != before {
		t.Fatalf("cross-workspace access must be 403 with no store call: %d %s", w.Code, w.Body.String())
	}
	// unknown workspace looks exactly like a forbidden one (no probing)
	w2 := do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-bob", "X-Workspace-ID", "cccccccc-0000-0000-0000-000000000003")
	if w2.Code != 403 || !contains(w2.Body.String(), "not_a_member") {
		t.Fatalf("unknown workspace: %d %s", w2.Code, w2.Body.String())
	}
	if w := do(r, "GET", "/api/v1/proposals", "", "Authorization", "Bearer tok-bob", "X-Workspace-ID", "not-a-uuid"); w.Code != 400 {
		t.Fatalf("malformed workspace id: %d", w.Code)
	}
	if strings.Join(seen, ",") != wsB+","+wsA {
		t.Fatalf("store calls ran in the wrong workspaces: %v", seen)
	}
}

func TestRolesGateRoutes(t *testing.T) {
	seen := []string{}
	r := newAuthRouter(workspaceStore{seen: &seen}, &fakePlatform{}, nil)
	body := `{"name":"Invoice extraction"}`
	cases := []struct {
		token, method, path, body string
		want                      int
	}{
		{"tok-victor", "GET", "/api/v1/proposals", "", 200},
		{"tok-victor", "POST", "/api/v1/proposals", body, 403},                                   // viewer cannot create
		{"tok-ana", "POST", "/api/v1/proposals", body, 201},                                      // analyst can
		{"tok-ana", "DELETE", "/api/v1/proposals/11111111-1111-1111-1111-111111111111", "", 403}, // delete is admin
		{"tok-alice", "DELETE", "/api/v1/proposals/11111111-1111-1111-1111-111111111111", "", 204},
		{"tok-victor", "POST", "/api/v1/pilots", `{}`, 200},  // calculations are open to viewers
		{"tok-victor", "PUT", "/api/v1/settings", `{}`, 403}, // settings are admin
	}
	for _, tc := range cases {
		w := do(r, tc.method, tc.path, tc.body, "Authorization", "Bearer "+tc.token, "Content-Type", "application/json")
		if w.Code != tc.want {
			t.Errorf("%s %s as %s: want %d, got %d %s", tc.method, tc.path, tc.token, tc.want, w.Code, w.Body.String())
		}
	}
}

func TestMeReportsUserWorkspaceAndRole(t *testing.T) {
	r := newAuthRouter(fakeStore{}, &fakePlatform{}, nil)
	w := do(r, "GET", "/api/v1/me", "", "Authorization", "Bearer tok-multi", "X-Workspace-ID", wsB)
	var me struct {
		AuthMode   string            `json:"auth_mode"`
		Role       string            `json:"role"`
		User       map[string]string `json:"user"`
		Workspaces []model.Workspace `json:"workspaces"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if w.Code != 200 || me.AuthMode != "required" || me.Role != "viewer" || me.User["email"] != "multi@x.test" || len(me.Workspaces) != 2 {
		t.Fatalf("me: %d %s", w.Code, w.Body.String())
	}
	// /me works without choosing a workspace
	if w := do(r, "GET", "/api/v1/me", "", "Authorization", "Bearer tok-multi"); w.Code != 200 {
		t.Fatalf("me without workspace: %d", w.Code)
	}
}

func TestAuthOffUsesTheDefaultWorkspaceAsAdmin(t *testing.T) {
	seen := []string{}
	r := newAuthRouter(workspaceStore{seen: &seen}, &fakePlatform{}, func(c *config.Config) { c.AuthMode = "off" })
	if w := do(r, "DELETE", "/api/v1/proposals/11111111-1111-1111-1111-111111111111", ""); w.Code != 204 {
		t.Fatalf("auth off acts as admin: %d %s", w.Code, w.Body.String())
	}
	if seen[0] != model.DefaultWorkspaceID {
		t.Fatalf("auth off must use the default workspace, got %v", seen)
	}
	w := do(r, "GET", "/api/v1/me", "")
	if !contains(w.Body.String(), `"auth_mode":"off"`) {
		t.Fatalf("me with auth off: %s", w.Body.String())
	}
}

func TestRateLimitReturns429WithRetryAfter(t *testing.T) {
	r := newAuthRouter(fakeStore{}, &fakePlatform{}, func(c *config.Config) {
		c.AuthMode, c.RateLimitPerMin, c.RateLimitBurst = "off", 60, 3
	})
	codes := []int{}
	for range 5 {
		codes = append(codes, do(r, "GET", "/api/v1/me", "").Code)
	}
	if codes[2] != 200 || codes[3] != 429 {
		t.Fatalf("burst of 3 then 429 expected, got %v", codes)
	}
	w := do(r, "GET", "/api/v1/me", "")
	if w.Header().Get("Retry-After") == "" || !contains(w.Body.String(), "rate_limited") {
		t.Fatalf("429 must carry Retry-After and the error envelope: %v %s", w.Header(), w.Body.String())
	}
	if do(r, "GET", "/health", "").Code != 200 {
		t.Fatal("health is never rate limited")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	now := time.Unix(0, 0)
	rl := newRateLimiter(60, 1) // one per second
	rl.now = func() time.Time { return now }
	if ok, _ := rl.allow("ip"); !ok {
		t.Fatal("first call must pass")
	}
	ok, wait := rl.allow("ip")
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("second call must wait up to 1s, got %v %v", ok, wait)
	}
	now = now.Add(time.Second)
	if ok, _ := rl.allow("ip"); !ok {
		t.Fatal("a token refills after a second")
	}
	if ok, _ := rl.allow("other-ip"); !ok {
		t.Fatal("buckets are per client")
	}
}

func TestCORSAllowsAuthHeaders(t *testing.T) {
	r := newAuthRouter(fakeStore{}, &fakePlatform{}, nil)
	w := do(r, "OPTIONS", "/api/v1/proposals", "", "Origin", "http://localhost:5173")
	allow := w.Header().Get("Access-Control-Allow-Headers")
	if !contains(allow, "Authorization") || !contains(allow, "X-Workspace-ID") || !contains(w.Header().Get("Access-Control-Allow-Methods"), "PUT") {
		t.Fatalf("preflight must allow the auth headers and PUT: %v", w.Header())
	}
}
