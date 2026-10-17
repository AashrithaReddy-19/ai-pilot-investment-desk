package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/config"
	"c23desk/internal/model"
)

func init() { gin.SetMode(gin.TestMode) }

// ---- fakes ----

type fakeRules struct {
	do        func(method, path string, body []byte) (*model.Upstream, error)
	pingErr   error
	lastBody  string
	lastReqID string
	lastPath  string
	lastCT    string
}

func (f *fakeRules) Do(_ context.Context, method, path string, body []byte, reqID string) (*model.Upstream, error) {
	f.lastBody, f.lastReqID, f.lastPath = string(body), reqID, path
	return f.do(method, path, body)
}

// DoUpload always posts with the caller's content-type; upstream errors flow through upstreamOK.
func (f *fakeRules) DoUpload(_ context.Context, path string, body []byte, contentType, reqID string) (*model.Upstream, error) {
	f.lastBody, f.lastReqID, f.lastPath, f.lastCT = string(body), reqID, path, contentType
	return f.do(http.MethodPost, path, body)
}

func (f *fakeRules) Ping(context.Context) error { return f.pingErr }

type fakeCache struct{}

func (fakeCache) Do(ctx context.Context, _ string, _ []byte, fn func(context.Context) (*model.Upstream, error)) (*model.Upstream, bool, error) {
	r, err := fn(ctx)
	return r, false, err
}
func (fakeCache) Ping(context.Context) error { return nil }
func (fakeCache) Enabled() bool              { return false }

// fakeStore embeds the interface: any method the test does not override panics, which flags unexpected DB use.
type fakeStore struct {
	Store
	pingErr error
}

func (f fakeStore) Ping(context.Context) error { return f.pingErr }
func (f fakeStore) AddObservation(_ context.Context, o model.ObservationIn, d model.DecisionIn) (*model.Observation, *model.Decision, error) {
	return &model.Observation{ID: 1, PilotID: o.PilotID}, &model.Decision{ID: 1, PilotID: d.PilotID, Decision: d.Decision}, nil
}

func ok200(body string) func(string, string, []byte) (*model.Upstream, error) {
	return func(string, string, []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 200, Body: []byte(body), ContentType: "application/json"}, nil
	}
}

func TestUploadForwardPassesBodyAndContentTypeThrough(t *testing.T) {
	f := &fakeRules{do: ok200(`{"ai_burn_to_date": 1550000}`)}
	multipart := "--XX\r\nContent-Disposition: form-data; name=\"file\"; filename=\"bs.csv\"\r\n" +
		"Content-Type: text/csv\r\n\r\nAccount,Amount\nAI software,12\r\n--XX--\r\n"
	r := newRouter(f, fakeStore{})
	w := do(r, "POST", "/api/v1/balance-sheet/parse", multipart,
		"Content-Type", "multipart/form-data; boundary=XX")
	if w.Code != 200 || w.Body.String() != `{"ai_burn_to_date": 1550000}` {
		t.Fatalf("upload failed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(f.lastCT, "multipart/form-data") || f.lastPath != "/balance-sheet/parse" {
		t.Fatalf("content-type/path lost: %q %q", f.lastCT, f.lastPath)
	}
	if !strings.Contains(f.lastBody, "AI software") {
		t.Fatalf("upload body not forwarded:%q", f.lastBody)
	}
}

func TestUploadOverLimitRejectedBeforeTheEngine(t *testing.T) {
	called := false
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		called = true
		return &model.Upstream{Status: 200, Body: []byte(`{}`), ContentType: "application/json"}, nil
	}}
	r := newRouter(f, fakeStore{})
	w := do(r, "POST", "/api/v1/balance-sheet/parse", strings.Repeat("x", 5000),
		"Content-Type", "text/csv")
	if w.Code != 413 || !strings.Contains(w.Body.String(), "payload_too_large") {
		t.Fatalf("expected 413, got %d %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("engine must not be called after the size limit rejects the upload")
	}
}

func TestUploadBadEngineAnswerIsClean(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 422, Body: []byte(`{"detail":"No id column found"}`), ContentType: "application/json"}, nil
	}}
	r := newRouter(f, fakeStore{})
	w := do(r, "POST", "/api/v1/pilots/import/confirm", "x", "Content-Type", "text/plain")
	if w.Code != 422 || !strings.Contains(w.Body.String(), "rules_rejected") || !strings.Contains(w.Body.String(), "No id column") {
		t.Fatalf("engine 422 must pass through cleanly: %d %s", w.Code, w.Body.String())
	}
}

func newRouter(f *fakeRules, st Store) http.Handler {
	return NewRouter(Deps{
		Cfg: config.Config{
			AllowedOrigins: []string{"http://localhost:5173"},
			RequestTimeout: 2 * time.Second,
			MaxBodyBytes:   1024,
		},
		Store: st, Rules: f, Cache: fakeCache{},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func do(h http.Handler, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// ---- tests ----

func TestHealth(t *testing.T) {
	w := do(newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{}), "GET", "/health", "")
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
}

func TestReady(t *testing.T) {
	good := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{})
	if w := do(good, "GET", "/ready", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"redis":"disabled"`) {
		t.Fatalf("want 200 with redis disabled, got %d %s", w.Code, w.Body)
	}
	rulesDown := newRouter(&fakeRules{do: ok200(`{}`), pingErr: errors.New("down")}, fakeStore{})
	if w := do(rulesDown, "GET", "/ready", ""); w.Code != 503 {
		t.Fatalf("rules engine down must give 503, got %d", w.Code)
	}
	dbDown := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{pingErr: errors.New("down")})
	if w := do(dbDown, "GET", "/ready", ""); w.Code != 503 {
		t.Fatalf("postgres down must give 503, got %d", w.Code)
	}
}

func TestPortfolioPassThroughKeepsBodyAndRequestID(t *testing.T) {
	f := &fakeRules{do: ok200(`{"ok":true,"selected":["AI1"]}`)}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/portfolio", `{"budget":100000}`, "X-Request-ID", "abc-123")
	if w.Code != 200 || w.Body.String() != `{"ok":true,"selected":["AI1"]}` {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body)
	}
	if f.lastBody != `{"budget":100000}` || f.lastReqID != "abc-123" || f.lastPath != "/portfolio" {
		t.Fatalf("not forwarded correctly: body=%q id=%q path=%q", f.lastBody, f.lastReqID, f.lastPath)
	}
	if w.Header().Get("X-Request-ID") != "abc-123" {
		t.Fatal("request id must be echoed back")
	}
}

func TestEmptyBodyBecomesEmptyObject(t *testing.T) {
	f := &fakeRules{do: ok200(`{}`)}
	do(newRouter(f, fakeStore{}), "POST", "/api/v1/pilots", "", "")
	if f.lastBody != "{}" {
		t.Fatalf("empty body must be forwarded as {}, got %q", f.lastBody)
	}
}

func TestUnsafeRequestIDIsReplaced(t *testing.T) {
	w := do(newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{}), "GET", "/health", "", "X-Request-ID", "bad id\r\nX: y")
	if got := w.Header().Get("X-Request-ID"); got == "" || strings.ContainsAny(got, " \r\n") {
		t.Fatalf("unsafe id must be replaced, got %q", got)
	}
}

func TestRulesRejectionKeepsClientStatus(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 422, Body: []byte(`{"detail":"mode must be independent or conservative"}`)}, nil
	}}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/portfolio", `{"mode":"x"}`)
	if w.Code != 422 || !strings.Contains(w.Body.String(), `"code":"rules_rejected"`) {
		t.Fatalf("want 422 rules_rejected, got %d %s", w.Code, w.Body)
	}
}

func TestRulesEngineDownGives502(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) { return nil, errors.New("connection refused") }}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/portfolio", `{}`)
	if w.Code != 502 || !strings.Contains(w.Body.String(), `"code":"rules_unavailable"`) {
		t.Fatalf("want 502 rules_unavailable, got %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "connection refused") {
		t.Fatal("internal error details must not leak to clients")
	}
}

func TestRulesEngineTimeoutGives504(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) { return nil, context.DeadlineExceeded }}
	if w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/portfolio", `{}`); w.Code != 504 {
		t.Fatalf("want 504, got %d", w.Code)
	}
}

func TestRulesEngine500Gives502(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 500, Body: []byte("Traceback ...")}, nil
	}}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/portfolio", `{}`)
	if w.Code != 502 || strings.Contains(w.Body.String(), "Traceback") {
		t.Fatalf("want 502 without upstream details, got %d %s", w.Code, w.Body)
	}
}

func TestBadBodies(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{})
	if w := do(h, "POST", "/api/v1/portfolio", `{bad`); w.Code != 400 {
		t.Fatalf("invalid JSON must give 400, got %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/portfolio", `{"x":"`+strings.Repeat("a", 2000)+`"}`); w.Code != 413 {
		t.Fatalf("oversize body must give 413, got %d", w.Code)
	}
}

func TestExportsSetDownloadHeaders(t *testing.T) {
	f := &fakeRules{do: func(string, string, []byte) (*model.Upstream, error) {
		return &model.Upstream{Status: 200, Body: []byte("id,task\nAI1,x\n"), ContentType: "text/plain; charset=utf-8"}, nil
	}}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/export/csv", `{}`)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "pilots.csv") {
		t.Fatalf("unexpected export response: %d %v", w.Code, w.Header())
	}
}

func TestInvalidNamesAreRejectedBeforeAnyQuery(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{}) // embedded nil Store would panic on any query
	for _, path := range []string{"/api/v1/portfolios/bad.name", "/api/v1/pilots/bad%20id/history"} {
		if w := do(h, "GET", path, ""); w.Code != 422 {
			t.Errorf("%s: want 422, got %d", path, w.Code)
		}
	}
	if w := do(h, "POST", "/api/v1/portfolios", `{"name":"../etc"}`); w.Code != 422 {
		t.Errorf("save with bad name: want 422, got %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/portfolios", `{}`); w.Code != 422 {
		t.Errorf("save without name: want 422, got %d", w.Code)
	}
}

func TestObservationValidation(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{})
	for name, body := range map[string]string{
		"adoption over 100": `{"observed_adoption_pct":150}`,
		"quality negative":  `{"observed_quality_pct":-1}`,
		"not a number":      `{"observed_adoption_pct":"lots"}`,
		"nothing measured":  `{}`,
	} {
		if w := do(h, "POST", "/api/v1/pilots/AI1/observations", body); w.Code != 422 {
			t.Errorf("%s: want 422, got %d %s", name, w.Code, w.Body)
		}
	}
}

func TestObservationStoresDecisionFromRulesEngine(t *testing.T) {
	f := &fakeRules{do: ok200(`{"decision":"STOP","reasons":["Adoption 20% is below breakeven 33.3%"],"metric":"adoption","threshold":33.3,"actual":20}`)}
	w := do(newRouter(f, fakeStore{}), "POST", "/api/v1/pilots/AI1/observations",
		`{"observed_adoption_pct":20,"observed_quality_pct":90,"source":"ops lead"}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"decision":"STOP"`) {
		t.Fatalf("want 201 with STOP, got %d %s", w.Code, w.Body)
	}
	if strings.Contains(f.lastBody, "source") || f.lastPath != "/stoprule/AI1" {
		t.Fatalf("source must not be forwarded; path=%q body=%q", f.lastPath, f.lastBody)
	}
}

func TestBuildRowsMapsSelectedRejectedAndAssumedRunCost(t *testing.T) {
	pilots := `{"pilots":[
	  {"id":"AI1","group":"finance","setup":200000,"run":60000,"flags":[],"independent":{"net":120000,"payback_months":20,"multi_period":{"profit_inr":160000,"roi_pct":80,"breakeven_month":20}},"conservative":{"net":80000,"payback_months":30,"multi_period":{"profit_inr":80000,"roi_pct":40,"breakeven_month":30}}},
	  {"id":"AI5","group":"support","setup":200000,"run":40000,"flags":[{"code":"RUN_COST_ASSUMED"}],"independent":{"net":140000,"payback_months":17.1},"conservative":{"net":-1,"payback_months":null}}
	]}`
	port := `{"budget":600000,"selected":["AI1"],"rejected":[{"id":"AI5","reason":"Overlap: shares benefit group \"support\" with AI4"}]}`
	rows, budget, err := buildRows([]byte(pilots), []byte(port), "independent")
	if err != nil || len(rows) != 2 || budget == nil || *budget != 600000 {
		t.Fatalf("unexpected: rows=%v budget=%v err=%v", rows, budget, err)
	}
	if !rows[0].Selected || rows[0].Reason != nil || *rows[0].PaybackMonths != 20 {
		t.Errorf("AI1 row wrong: %+v", rows[0])
	}
	// multi-period outcomes ride along per mode (independent here)
	if rows[0].ProfitINR == nil || *rows[0].ProfitINR != 160000 || rows[0].ROIPct == nil || *rows[0].ROIPct != 80 {
		t.Errorf("AI1 multi-period wrong: %+v", rows[0])
	}
	if rows[1].Selected || !rows[1].RunAssumed || !strings.Contains(*rows[1].Reason, "Overlap") {
		t.Errorf("AI5 row wrong: %+v", rows[1])
	}
	if rows[1].ProfitINR != nil || rows[1].ROIPct != nil {
		t.Errorf("absent multi-period must stay NULL, got %+v", rows[1])
	}
	conservative, _, _ := buildRows([]byte(pilots), []byte(port), "conservative")
	if conservative[1].PaybackMonths != nil || *conservative[1].NetINR != -1 {
		t.Errorf("conservative mode must use the conservative view and keep 'never pays back' as nil: %+v", conservative[1])
	}
	if conservative[0].ProfitINR == nil || *conservative[0].ProfitINR != 80000 {
		t.Errorf("conservative multi-period must come from the conservative view: %+v", conservative[0])
	}
}

func TestExtConventions(t *testing.T) {
	h, d := extConventions([]byte(`{"defaults":{"horizon_months":24,"discount_rate":0.12}}`))
	if h == nil || *h != 24 || d == nil || *d != 0.12 {
		t.Fatalf("conventions lost: %v %v", h, d)
	}
	// explicit nulls and a missing defaults block both stay NULL
	if h, d := extConventions([]byte(`{"defaults":{"horizon_months":null}}`)); h != nil || d != nil {
		t.Fatalf("null must stay NULL: %v %v", h, d)
	}
	if h, d := extConventions([]byte(`{}`)); h != nil || d != nil {
		t.Fatalf("absent must stay NULL: %v %v", h, d)
	}
}

func TestCORS(t *testing.T) {
	h := newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{})
	w := do(h, "OPTIONS", "/api/v1/portfolio", "", "Origin", "http://localhost:5173")
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("allowed origin must pass preflight: %d %v", w.Code, w.Header())
	}
	w = do(h, "OPTIONS", "/api/v1/portfolio", "", "Origin", "http://evil.example")
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origin must not get CORS headers")
	}
}

func TestUnknownRouteUsesErrorShape(t *testing.T) {
	w := do(newRouter(&fakeRules{do: ok200(`{}`)}, fakeStore{}), "GET", "/nope", "")
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"code":"not_found"`) {
		t.Fatalf("got %d %s", w.Code, w.Body)
	}
}

func TestHistoryUsesClampedLimitAndFineStore(t *testing.T) {
	fake := fakeStore{}
	// embed a stub for the new method via pointer receiver on a wrapper struct
	hs := historyStub{fakeStore: fake, events: []model.HistoryEvent{{ID: 2, Kind: "evaluation"}, {ID: 1, Kind: "decision"}}}
	w := do(newRouter(&fakeRules{do: ok200(`{}`)}, hs), "GET", "/api/v1/history?limit=999", "")
	if w.Code != 200 {
		t.Fatalf("history failed: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Events []model.HistoryEvent `json:"events"`
		Count  int                  `json:"count"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Count != 2 || len(out.Events) != 2 {
		t.Fatalf("unexpected history body: %s", w.Body.String())
	}
}

type historyStub struct {
	fakeStore
	events []model.HistoryEvent
}

func (s historyStub) HistoryTimeline(_ context.Context, limit int) ([]model.HistoryEvent, error) {
	if limit != 25 && limit != 50 && (limit < 1 || limit > 50) {
		return nil, fmt.Errorf("limit %d left the gateway unclamped", limit)
	}
	return s.events, nil
}
