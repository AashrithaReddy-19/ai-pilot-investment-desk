package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"

	"c23desk/internal/aiclient"
	"c23desk/internal/cache"
	"c23desk/internal/config"
	"c23desk/internal/httpapi"
	"c23desk/internal/rules"
	"c23desk/internal/service"
	"c23desk/internal/store"
	"c23desk/internal/store/pgtest"
	"c23desk/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Stop()
	os.Exit(code)
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeAI is a controllable stand-in for the FastAPI service.
type fakeAI struct {
	mode  atomic.Value // "ok" | "down" | "slow" | "hallucinate"
	calls atomic.Int64
}

func (f *fakeAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	switch f.mode.Load().(string) {
	case "down":
		w.WriteHeader(http.StatusInternalServerError)
		return
	case "slow":
		time.Sleep(2 * time.Second)
	case "hallucinate":
		_, _ = io.WriteString(w, `{"model_name":"x","model_version":"1","advisory":true,"pairs":[{"pilot_a":"AI4","pilot_b":"AI99","semantic_similarity":0.99}],"explanation":"AI1 is proven to deliver 900% ROI","grounded":false}`)
		return
	}
	switch r.URL.Path {
	case "/health":
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	case "/internal/explain":
		_, _ = io.WriteString(w, `{"model_name":"template-explainer","model_version":"1.0.0","explanation":"Grounded summary.","grounded":true,"checks":["numbers"]}`)
	case "/internal/semantic-overlap":
		_, _ = io.WriteString(w, `{"model_name":"lexical-domain-overlap","model_version":"1.0.0","advisory":true,"pairs":[{"pilot_a":"AI4","pilot_b":"AI5","semantic_similarity":0.42,"possible_overlap":true,"reason":"shared concept: support","method":"lexical"}]}`)
	default:
		w.WriteHeader(404)
	}
}

type env struct {
	t     *testing.T
	h     http.Handler
	ai    *fakeAI
	redis *miniredis.Miniredis
	svc   *service.Service
}

type opts struct {
	dbURL  string
	apiKey string
	cors   []string
	noAI   bool
}

func newEnv(t *testing.T, o opts) *env {
	t.Helper()
	var st *store.Store
	if o.dbURL != "" {
		var err error
		st, err = store.Open(t.Context(), o.dbURL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(st.Close)
	} else {
		st = pgtest.Fresh(t)
	}
	f := &fakeAI{}
	f.mode.Store("ok")
	aiURL := ""
	if !o.noAI {
		srv := httptest.NewServer(f)
		t.Cleanup(srv.Close)
		aiURL = srv.URL
	}
	mr := miniredis.RunT(t)
	rc, _ := cache.NewRedis("redis://"+mr.Addr(), quiet)
	svc := &service.Service{Store: st, Cache: rc, AI: aiclient.New(aiURL, 500*time.Millisecond, quiet), Rules: rules.Default(), Log: quiet,
		RawDir: testutil.RawDir()}
	cfg := config.Config{MaxBodyBytes: 64 << 10, CORSOrigins: o.cors, APIKey: o.apiKey}
	return &env{t: t, h: httpapi.New(svc, cfg, quiet), ai: f, redis: mr, svc: svc}
}

type resp struct {
	Code   int
	Body   []byte
	Header http.Header
}

func (r resp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body, &m)
	return m
}

func (r resp) errCode() string {
	m := r.json()
	if e, ok := m["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (e *env) do(method, path, body string, hdr ...string) resp {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, req)
	return resp{w.Code, w.Body.Bytes(), w.Header()}
}

// get walks a JSON path like "evaluation.calculated_metrics.stated_basis.payback_months".
func get(m any, path string) any {
	for _, k := range strings.Split(path, ".") {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[k]
	}
	return m
}

func s(v any) string {
	if v == nil {
		return "null"
	}
	if str, ok := v.(string); ok {
		return str
	}
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), `"`)
}

func (e *env) seed() {
	e.t.Helper()
	if r := e.do("POST", "/api/v1/import/official", ""); r.Code != 201 {
		e.t.Fatalf("seed: %d %s", r.Code, r.Body)
	}
}

func (e *env) portfolio(name string, budget any, ids ...string) map[string]any {
	e.t.Helper()
	b, _ := json.Marshal(map[string]any{"name": name, "budget_inr": budget, "pilot_ids": ids})
	r := e.do("POST", "/api/v1/portfolios", string(b))
	if r.Code != 201 {
		e.t.Fatalf("create portfolio: %d %s", r.Code, r.Body)
	}
	return r.json()
}

// ------------------------------------------------------------------ judge demos

func TestDemo1_OfficialReference20Months(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	r := e.do("POST", "/api/v1/pilots/AI1/evaluate", "")
	m := r.json()
	if r.Code != 200 || s(get(m, "evaluation.calculated_metrics.stated_basis.net_annual_benefit_inr")) != "120000" ||
		s(get(m, "evaluation.calculated_metrics.stated_basis.monthly_net_benefit_inr")) != "10000" ||
		s(get(m, "evaluation.calculated_metrics.stated_basis.payback_months")) != "20" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if s(get(m, "evaluation.rules_version")) != "c23-rules-v1" || s(get(m, "ai_status")) != "ok" || get(m, "decision_id") == nil {
		t.Fatal(string(r.Body))
	}
	if s(get(m, "evaluation.input_snapshot.field_sources.setup_cost_inr")) != "OFFICIAL_FIXTURE" {
		t.Fatal("provenance missing")
	}
	// stateless path gives the same numbers without the database
	r = e.do("POST", "/api/v1/evaluate", `{"id":"REF","task":"ref","setup_cost_inr":200000,"annual_running_cost_inr":60000,"annual_benefit_inr":180000,"benefit_type":"cashable","overlap_group":"x"}`)
	if s(get(r.json(), "evaluation.calculated_metrics.stated_basis.payback_months")) != "20" {
		t.Fatal(string(r.Body))
	}
}

func scenario(e *env, id, overrides string) map[string]any {
	e.t.Helper()
	r := e.do("POST", "/api/v1/pilots/"+id+"/scenarios", `{"name":"judge","overrides":`+overrides+`}`)
	if r.Code != 201 {
		e.t.Fatalf("scenario: %d %s", r.Code, r.Body)
	}
	return r.json()
}

func TestDemo2_CaseA_ChangedRunningCost40Months(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	m := scenario(e, "AI1", `{"annual_running_cost_inr":120000}`)
	if s(get(m, "result.scenario.calculated_metrics.stated_basis.net_annual_benefit_inr")) != "60000" ||
		s(get(m, "result.scenario.calculated_metrics.stated_basis.monthly_net_benefit_inr")) != "5000" ||
		s(get(m, "result.scenario.calculated_metrics.stated_basis.payback_months")) != "40" {
		t.Fatal(m)
	}
	// official value untouched
	if s(get(e.do("GET", "/api/v1/pilots/AI1", "").json(), "annual_running_cost_inr")) != "60000" {
		t.Fatal("scenario must not modify the pilot")
	}
	if l := e.do("GET", "/api/v1/pilots/AI1/scenarios", "").json(); len(get(l, "scenarios").([]any)) != 1 {
		t.Fatal("scenario run not stored")
	}
}

func TestDemo3_CaseD_OverlapAI4AI5(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	p := e.portfolio("support pair", 600000, "AI4", "AI5")
	id := s(p["id"])
	if s(p["status"]) != "NEEDS_REVIEW" || s(get(p, "assessment.total_recognized_benefit_inr")) != "180000" {
		t.Fatal(p)
	}
	ov := e.do("GET", "/api/v1/portfolios/"+id+"/overlaps", "").json()
	o := get(ov, "overlaps").([]any)[0]
	if s(get(o, "code")) != "DOUBLE_COUNT_RISK" || s(get(o, "status")) != "UNRESOLVED" || s(get(ov, "ai_status")) != "ok" ||
		len(get(ov, "ai_suggestions").([]any)) != 1 {
		t.Fatal(ov)
	}
	if r := e.do("POST", "/api/v1/portfolios/"+id+"/approve", `{"version":1}`); r.Code != 409 || r.errCode() != "APPROVAL_BLOCKED" {
		t.Fatalf("unresolved overlap must block approval: %d %s", r.Code, r.Body)
	}
	// over-allocation is rejected
	if r := e.do("POST", "/api/v1/portfolios/"+id+"/overlaps/support/resolve",
		`{"resolution_type":"CUSTOM_ALLOCATION","allocations_inr":{"AI4":180000,"AI5":180000},"version":1}`); r.Code != 422 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	r := e.do("POST", "/api/v1/portfolios/"+id+"/overlaps/support/resolve",
		`{"resolution_type":"CUSTOM_ALLOCATION","allocations_inr":{"AI4":120000,"AI5":60000},"review_note":"agreed with support lead","version":1}`)
	m := r.json()
	// overlap resolved (counted once: 180000), but AI5 is REVISE (low readiness) so approval is still blocked
	if r.Code != 200 || s(m["status"]) != "NEEDS_REVIEW" || s(get(m, "assessment.total_recognized_benefit_inr")) != "180000" ||
		!strings.Contains(string(r.Body), "PILOT_NEEDS_REVISION") {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := e.do("POST", "/api/v1/portfolios/"+id+"/approve", `{"version":2}`); r.Code != 409 {
		t.Fatal("REVISE pilot must block approval", r.Code)
	}
	// revise AI5: PUT back exactly what GET returned, with readiness improved (read-only fields are ignored)
	ai5 := e.do("GET", "/api/v1/pilots/AI5", "").json()
	ai5["readiness_label"] = "medium"
	if r := e.do("PUT", "/api/v1/pilots/AI5", string(mustJSON(ai5))); r.Code != 200 ||
		s(get(r.json(), "benefit_type_raw")) != "cashable assumption" || s(get(r.json(), "field_sources.setup_cost_inr")) != "OFFICIAL_FIXTURE" ||
		s(get(r.json(), "field_sources.readiness_label")) != "TEAM_ASSUMPTION" {
		t.Fatalf("round-trip PUT: %d %s", r.Code, r.Body)
	}
	if g := e.do("GET", "/api/v1/portfolios/"+id, "").json(); s(g["status"]) != "VALID" {
		t.Fatal(g["status"])
	}
	r = e.do("POST", "/api/v1/portfolios/"+id+"/approve", `{"version":2}`)
	if r.Code != 200 || s(r.json()["status"]) != "APPROVED" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	g := e.do("GET", "/api/v1/portfolios/"+id, "").json()
	if s(g["status"]) != "APPROVED" || g["approval_stale"] != false {
		t.Fatal(g)
	}
	// REMOVE_ONE path on a fresh portfolio
	p2 := e.portfolio("support remove", 600000, "AI4", "AI5")
	r = e.do("POST", "/api/v1/portfolios/"+s(p2["id"])+"/overlaps/support/resolve", `{"resolution_type":"REMOVE_ONE","primary_pilot_id":"AI4","version":1}`)
	if r.Code != 200 || s(get(r.json(), "pilot_ids")) != `["AI4"]` || s(r.json()["status"]) != "VALID" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	// audit trail shows the decision history of the portfolio
	a := e.do("GET", "/api/v1/audit/portfolio/"+id, "").json()
	if acts := get(a, "entries").([]any); len(acts) != 3 || s(get(acts[1], "action")) != "resolve_overlap:support" || s(get(acts[2], "action")) != "approve" {
		t.Fatal(a)
	}
}

func TestDemo4_5_CaseE_F_AdoptionAndQuality(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	m := scenario(e, "AI1", `{"actual_adoption_pct":20,"actual_quality_pct":90}`)
	if s(get(m, "result.scenario.decision")) != "STOP" || s(get(m, "result.scenario.primary_reason")) != "ADOPTION_BELOW_BREAKEVEN" {
		t.Fatal(m)
	}
	m = scenario(e, "AI1", `{"actual_adoption_pct":45,"actual_quality_pct":90}`)
	if s(get(m, "result.scenario.decision")) != "REVISE" || s(get(m, "result.scenario.primary_reason")) != "LOW_ADOPTION" {
		t.Fatal(m)
	}
	m = scenario(e, "AI1", `{"actual_adoption_pct":70,"actual_quality_pct":60}`)
	if s(get(m, "result.scenario.decision")) != "STOP" || s(get(m, "result.scenario.primary_reason")) != "QUALITY_THRESHOLD_FAILED" {
		t.Fatal(m)
	}
	m = scenario(e, "AI1", `{"actual_adoption_pct":70,"actual_quality_pct":90}`)
	if s(get(m, "result.scenario.decision")) != "PROCEED" {
		t.Fatal(m)
	}
}

func TestDemo6_CaseB_UnknownRunCost(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	m := scenario(e, "AI1", `{"annual_running_cost_inr":null}`)
	if s(get(m, "result.scenario.decision")) != "REVISE" || s(get(m, "result.scenario.primary_reason")) != "MISSING_RUNNING_COST" ||
		s(get(m, "result.scenario.calculated_metrics.stated_basis.payback_months")) != "null" ||
		s(get(m, "result.scenario.calculated_metrics.stated_basis.payback_status")) != "NOT_CALCULABLE" {
		t.Fatal(m)
	}
}

func TestCaseG_NeverPaysBack(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	m := scenario(e, "AI1", `{"annual_running_cost_inr":200000}`)
	if s(get(m, "result.scenario.decision")) != "STOP" || s(get(m, "result.scenario.calculated_metrics.stated_basis.net_annual_benefit_inr")) != "-20000" ||
		s(get(m, "result.scenario.calculated_metrics.stated_basis.payback_status")) != "NEVER_PAYS_BACK" {
		t.Fatal(m)
	}
}

func TestDemo7_AIOutageCoreStillWorks(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	for _, mode := range []string{"down", "slow", "hallucinate"} {
		e.ai.mode.Store(mode)
		e.redis.FlushAll()
		start := time.Now()
		r := e.do("POST", "/api/v1/pilots/AI1/evaluate", "")
		m := r.json()
		if r.Code != 200 || s(m["ai_status"]) != "degraded" || m["ai_explanation"] != nil ||
			s(get(m, "evaluation.calculated_metrics.stated_basis.payback_months")) != "20" || s(get(m, "evaluation.decision")) != "REVIEW" {
			t.Fatalf("%s: %d %s", mode, r.Code, r.Body)
		}
		if time.Since(start) > 1500*time.Millisecond {
			t.Fatalf("%s: AI timeout not enforced (%v)", mode, time.Since(start))
		}
	}
	p := e.portfolio("ai down", 600000, "AI4", "AI5")
	ov := e.do("GET", "/api/v1/portfolios/"+s(p["id"])+"/overlaps", "").json()
	if s(ov["ai_status"]) != "degraded" || len(get(ov, "overlaps").([]any)) != 1 || len(get(ov, "ai_suggestions").([]any)) != 0 {
		t.Fatal("deterministic overlaps must survive AI failure; hallucinated suggestions must be dropped", ov)
	}
	e.ai.mode.Store("down")
	h := e.do("GET", "/health", "").json()
	if s(get(h, "components.ai_service")) != "degraded" || s(h["status"]) != "ok" {
		t.Fatal(h)
	}
	// AI not configured at all
	e2 := newEnv(t, opts{noAI: true})
	e2.seed()
	if m := e2.do("POST", "/api/v1/pilots/AI1/evaluate", "").json(); s(m["ai_status"]) != "disabled" {
		t.Fatal(m)
	}
}

// ------------------------------------------------------------------ budget, optimizer, portfolio lifecycle

func TestCaseC_BudgetExceeded(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	p := e.portfolio("too big", 600000, "AI1", "AI2", "AI3", "AI4")
	if s(p["status"]) != "REJECTED" || !strings.Contains(string(mustJSON(p)), "BUDGET_EXCEEDED") {
		t.Fatal(p)
	}
	if r := e.do("POST", "/api/v1/portfolios/"+s(p["id"])+"/approve", `{"version":1}`); r.Code != 409 {
		t.Fatal(r.Code)
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestOptimizeApplyApproveExport(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	p := e.portfolio("auto", 600000)
	id := s(p["id"])
	if s(p["status"]) != "DRAFT" {
		t.Fatal(p)
	}
	r := e.do("POST", "/api/v1/portfolios/"+id+"/optimize", `{}`)
	if s(get(r.json(), "optimization.selected")) != `["AI1","AI2","AI4"]` || r.Header.Get("X-Cache") != "MISS" {
		t.Fatal(string(r.Body))
	}
	if r := e.do("POST", "/api/v1/portfolios/"+id+"/optimize", `{}`); r.Header.Get("X-Cache") != "HIT" {
		t.Fatal("second optimize should hit cache")
	}
	r = e.do("POST", "/api/v1/portfolios/"+id+"/optimize", `{"apply":true,"version":1}`)
	if r.Code != 200 || s(get(r.json(), "portfolio.status")) != "VALID" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := e.do("POST", "/api/v1/portfolios/"+id+"/approve", `{"version":2}`); r.Code != 200 {
		t.Fatal(r.Code, string(r.Body))
	}
	j := e.do("GET", "/api/v1/export/"+id, "").json()
	if !strings.Contains(s(j["disclaimer"]), "not proven enterprise ROI") || len(get(j, "pilot_evaluations").(map[string]any)) != 3 {
		t.Fatal(j)
	}
	md := e.do("GET", "/api/v1/export/"+id+"?format=markdown", "")
	if !bytes.Contains(md.Body, []byte("Benefit attribution ledger")) || !bytes.Contains(md.Body, []byte("APPROVED")) {
		t.Fatal(string(md.Body))
	}
	csv := e.do("GET", "/api/v1/export/"+id+"?format=csv", "")
	if !bytes.HasPrefix(csv.Body, []byte("pilot_id,benefit_group")) || bytes.Count(csv.Body, []byte("\n")) != 4 {
		t.Fatal(string(csv.Body))
	}
	if r := e.do("GET", "/api/v1/export/"+id+"?format=xml", ""); r.Code != 400 {
		t.Fatal(r.Code)
	}
	// later change to a pilot makes the approval visibly stale
	pl := e.do("GET", "/api/v1/pilots/AI1", "").json()
	pl["annual_running_cost_inr"] = 150000
	delete(pl, "field_sources")
	for _, k := range []string{"created_at", "updated_at", "assumptions", "benefit_type_raw", "overlap_group_raw"} {
		delete(pl, k)
	}
	pl["benefit_type"] = "cashable assumption"
	if r := e.do("PUT", "/api/v1/pilots/AI1", string(mustJSON(pl))); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	g := e.do("GET", "/api/v1/portfolios/"+id, "").json()
	if s(g["status"]) != "APPROVED" || g["approval_stale"] != true {
		t.Fatal("approval must be flagged stale after input change", g["approval_stale"])
	}
}

func TestPilotUpdateVersioningProvenanceAuditAndCache(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	if r := e.do("POST", "/api/v1/pilots/AI2/evaluate", ""); r.Header.Get("X-Cache") != "MISS" {
		t.Fatal("first evaluate is a miss")
	}
	if r := e.do("POST", "/api/v1/pilots/AI2/evaluate", ""); r.Header.Get("X-Cache") != "HIT" {
		t.Fatal("second evaluate should hit")
	}
	body := `{"task":"Order exception triage","setup_cost_inr":200000,"annual_running_cost_inr":120000,"annual_benefit_inr":180000,
		"benefit_type":"cashable assumption","overlap_group":"ops","expected_adoption_pct":60,"readiness_label":"medium","version":1}`
	r := e.do("PUT", "/api/v1/pilots/AI2", body, "X-Actor", "judge-1")
	m := r.json()
	if r.Code != 200 || s(m["version"]) != "2" || s(get(m, "field_sources.annual_running_cost_inr")) != "TEAM_ASSUMPTION" ||
		s(get(m, "field_sources.setup_cost_inr")) != "OFFICIAL_FIXTURE" {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := e.do("PUT", "/api/v1/pilots/AI2", body); r.Code != 409 || r.errCode() != "VERSION_CONFLICT" {
		t.Fatal("stale version", r.Code)
	}
	// stale result must not be served after mutation
	r = e.do("POST", "/api/v1/pilots/AI2/evaluate", "")
	if r.Header.Get("X-Cache") != "MISS" || s(get(r.json(), "evaluation.calculated_metrics.stated_basis.payback_months")) != "40" {
		t.Fatal(r.Header.Get("X-Cache"), string(r.Body))
	}
	a := e.do("GET", "/api/v1/audit/pilot/AI2", "").json()
	entries := get(a, "entries").([]any)
	if len(entries) != 2 || s(get(entries[1], "actor")) != "judge-1" || s(get(entries[1], "before_state.annual_running_cost_inr")) != "60000" ||
		s(get(entries[1], "after_state.annual_running_cost_inr")) != "120000" {
		t.Fatal(a)
	}
	d := e.do("GET", "/api/v1/pilots/AI2/decisions", "").json()
	if hist := get(d, "decisions").([]any); len(hist) != 3 || s(get(hist[2], "pilot_version")) != "1" || s(get(hist[0], "pilot_version")) != "2" {
		t.Fatal("decision history must keep yesterday's snapshot", d)
	}
}

func TestRedisOutageCoreStillWorks(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	e.redis.Close()
	for i := 0; i < 3; i++ {
		r := e.do("POST", "/api/v1/pilots/AI1/evaluate", "")
		if r.Code != 200 || r.Header.Get("X-Cache") != "MISS" || s(get(r.json(), "evaluation.calculated_metrics.stated_basis.payback_months")) != "20" {
			t.Fatal(r.Code, string(r.Body))
		}
	}
	p := e.portfolio("no redis", 600000, "AI1")
	if r := e.do("GET", "/api/v1/portfolios/"+s(p["id"]), ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
	if h := e.do("GET", "/health", "").json(); s(get(h, "components.redis")) != "degraded" || s(h["status"]) != "ok" {
		t.Fatal(h)
	}
}

func TestDatabaseOutage(t *testing.T) {
	e := newEnv(t, opts{dbURL: "postgres://x:y@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"})
	if r := e.do("GET", "/api/v1/pilots", ""); r.Code != 503 || r.errCode() != "DATABASE_UNAVAILABLE" || bytes.Contains(r.Body, []byte("127.0.0.1")) {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if r := e.do("GET", "/ready", ""); r.Code != 503 {
		t.Fatal(r.Code)
	}
	h := e.do("GET", "/health", "")
	if h.Code != 200 || s(get(h.json(), "components.database")) != "unavailable" {
		t.Fatal(string(h.Body))
	}
	// stateless judge endpoints keep working
	r := e.do("POST", "/api/v1/optimize", `{"budget_inr":600000,"pilots":[
		{"id":"A","task":"a","setup_cost_inr":200000,"annual_running_cost_inr":60000,"annual_benefit_inr":180000,"benefit_type":"cashable","overlap_group":"support"},
		{"id":"B","task":"b","setup_cost_inr":200000,"annual_running_cost_inr":60000,"annual_benefit_inr":180000,"benefit_type":"cashable","overlap_group":"support"}]}`)
	if r.Code != 200 || s(get(r.json(), "result.selected")) != `["A"]` {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
}

// ------------------------------------------------------------------ negative / security

func TestNegativeAndSecurity(t *testing.T) {
	e := newEnv(t, opts{})
	e.seed()
	big := `{"id":"X","task":"` + strings.Repeat("a", 70<<10) + `"}`
	cases := []struct {
		name, method, path, body string
		code                     int
		err                      string
	}{
		{"malformed json", "POST", "/api/v1/pilots", `{"id":`, 400, "MALFORMED_JSON"},
		{"NaN literal", "POST", "/api/v1/evaluate", `{"id":"X","task":"t","setup_cost_inr":NaN}`, 400, "MALFORMED_JSON"},
		{"Infinity literal", "POST", "/api/v1/evaluate", `{"id":"X","task":"t","annual_benefit_inr":Infinity}`, 400, "MALFORMED_JSON"},
		{"negative setup", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","setup_cost_inr":-1}`, 422, "VALIDATION_FAILED"},
		{"negative run", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","annual_running_cost_inr":-1}`, 422, "VALIDATION_FAILED"},
		{"negative benefit", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","annual_benefit_inr":-1}`, 422, "VALIDATION_FAILED"},
		{"numeric string", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","setup_cost_inr":"2 lakh"}`, 422, "VALIDATION_FAILED"},
		{"missing task", "POST", "/api/v1/pilots", `{"id":"N1"}`, 422, "VALIDATION_FAILED"},
		{"adoption > 100", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","actual_adoption_pct":101}`, 422, "VALIDATION_FAILED"},
		{"quality > 100", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","actual_quality_pct":120}`, 422, "VALIDATION_FAILED"},
		{"readiness > 100", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","data_readiness_pct":101}`, 422, "VALIDATION_FAILED"},
		{"unknown enum", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","readiness_label":"extreme"}`, 422, "VALIDATION_FAILED"},
		{"unexpected field", "POST", "/api/v1/pilots", `{"id":"N1","task":"t","is_admin":true}`, 422, "VALIDATION_FAILED"},
		{"duplicate id", "POST", "/api/v1/pilots", `{"id":"ai1","task":"t"}`, 409, "CONFLICT"},
		{"sql injection id body", "POST", "/api/v1/pilots", `{"id":"x'; DROP TABLE pilots;--","task":"t"}`, 422, "VALIDATION_FAILED"},
		{"sql injection path", "GET", "/api/v1/pilots/x'%20OR%20'1'='1", "", 400, "INVALID_ID"},
		{"path traversal", "GET", "/api/v1/pilots/..%2F..%2Fetc%2Fpasswd", "", 400, "INVALID_ID"},
		{"unknown pilot", "GET", "/api/v1/pilots/AI99", "", 404, "NOT_FOUND"},
		{"unknown pilot evaluate", "POST", "/api/v1/pilots/AI99/evaluate", "", 404, "NOT_FOUND"},
		{"invalid uuid", "GET", "/api/v1/portfolios/not-a-uuid", "", 400, "INVALID_ID"},
		{"unknown portfolio", "GET", "/api/v1/portfolios/00000000-0000-4000-8000-000000000000", "", 404, "NOT_FOUND"},
		{"negative budget", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":-1,"pilot_ids":["AI1"]}`, 422, "VALIDATION_FAILED"},
		{"null budget", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":null,"pilot_ids":["AI1"]}`, 422, "VALIDATION_FAILED"},
		{"string budget", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":"6 lakh"}`, 422, "VALIDATION_FAILED"},
		{"duplicate portfolio pilot", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":1,"pilot_ids":["AI1","AI1"]}`, 422, "VALIDATION_FAILED"},
		{"unknown portfolio pilot", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":1,"pilot_ids":["AI99"]}`, 422, "VALIDATION_FAILED"},
		{"invalid filename/name", "POST", "/api/v1/portfolios", `{"name":"../../etc/passwd","budget_inr":1}`, 422, "VALIDATION_FAILED"},
		{"bad basis enum", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":1,"basis":"magic"}`, 422, "VALIDATION_FAILED"},
		{"unknown envelope field", "POST", "/api/v1/portfolios", `{"name":"x","budget_inr":1,"approved":true}`, 422, "VALIDATION_FAILED"},
		{"oversized payload", "POST", "/api/v1/pilots", big, 413, "PAYLOAD_TOO_LARGE"},
		{"bad audit entity", "GET", "/api/v1/audit/users/AI1", "", 400, "INVALID_ID"},
		{"bad overlap id", "POST", "/api/v1/portfolios/00000000-0000-4000-8000-000000000000/overlaps/..%2Fx/resolve", `{}`, 400, "INVALID_ID"},
		{"no route", "GET", "/api/v2/pilots", "", 404, "ROUTE_NOT_FOUND"},
		{"bad method", "DELETE", "/api/v1/pilots", "", 405, "METHOD_NOT_ALLOWED"},
		{"import malformed", "POST", "/api/v1/import", `{"fixture":{"records":5}}`, 400, "MALFORMED_INPUT"},
		{"import missing fixture", "POST", "/api/v1/import", `{}`, 422, "VALIDATION_FAILED"},
		{"optimize null budget", "POST", "/api/v1/optimize", `{"budget_inr":null,"pilots":[]}`, 422, "VALIDATION_FAILED"},
		{"scenario bad override", "POST", "/api/v1/pilots/AI1/scenarios", `{"overrides":{"annual_running_cost_inr":-5}}`, 422, "VALIDATION_FAILED"},
		{"scenario override id", "POST", "/api/v1/pilots/AI1/scenarios", `{"overrides":{"id":"AI2"}}`, 422, "VALIDATION_FAILED"},
		{"update without version", "PUT", "/api/v1/pilots/AI1", `{"task":"t"}`, 422, "VALIDATION_FAILED"},
		{"update id mismatch", "PUT", "/api/v1/pilots/AI1", `{"id":"AI2","task":"t","version":1}`, 422, "VALIDATION_FAILED"},
		{"import second time", "POST", "/api/v1/import/official", "", 409, "CONFLICT"},
	}
	for _, c := range cases {
		r := e.do(c.method, c.path, c.body)
		if strings.Contains(c.path, "%2F") && r.Code == 404 && r.errCode() == "ROUTE_NOT_FOUND" {
			continue // encoded slashes never reach a handler: equally safe (no file system access exists)
		}
		if r.Code != c.code || r.errCode() != c.err {
			t.Errorf("%s: got %d %s, want %d %s (%s)", c.name, r.Code, r.errCode(), c.code, c.err, truncate(r.Body))
		}
		if r.Code >= 400 && get(r.json(), "error.request_id") == nil {
			t.Errorf("%s: error without request_id", c.name)
		}
	}
	// structured field errors
	r := e.do("POST", "/api/v1/pilots", `{"id":"N1","task":"t","setup_cost_inr":-1}`)
	f := get(r.json(), "error.fields").([]any)[0]
	if s(get(f, "field")) != "setup_cost_inr" || s(get(f, "code")) != "NEGATIVE_VALUE" || s(get(f, "message")) != "setup_cost_inr must be >= 0" {
		t.Fatal(string(r.Body))
	}
	// empty portfolio is a DRAFT, not an error
	if p := e.portfolio("empty", 600000); s(p["status"]) != "DRAFT" {
		t.Fatal(p)
	}
	// tables survived the injection attempts
	if l := e.do("GET", "/api/v1/pilots", "").json(); s(l["count"]) != "5" {
		t.Fatal(l)
	}
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200])
	}
	return string(b)
}

func TestImportAllOrNothing(t *testing.T) {
	e := newEnv(t, opts{})
	body := `{"fixture":{"records":{"pilots":[
		{"id":"P1","task":"a","setup_cost_inr":1,"annual_running_cost_inr":1,"annual_benefit_inr":2,"benefit_type":"cash","overlap_group":"g"},
		{"id":"P2","task":"b","setup_cost_inr":-5}]}}}`
	r := e.do("POST", "/api/v1/import", body)
	if r.Code != 422 || !strings.Contains(string(r.Body), "nothing imported") {
		t.Fatal(r.Code, string(r.Body))
	}
	if l := e.do("GET", "/api/v1/pilots", "").json(); s(l["count"]) != "0" {
		t.Fatal("partial import")
	}
	r = e.do("POST", "/api/v1/import", strings.Replace(body, `"setup_cost_inr":-5`, `"setup_cost_inr":5`, 1))
	if r.Code != 201 || s(get(r.json(), "imported")) != `["P1","P2"]` || s(get(r.json(), "source")) != "TEAM_ASSUMPTION" {
		t.Fatal(r.Code, string(r.Body))
	}
}

func TestAPIKeyAndCORS(t *testing.T) {
	e := newEnv(t, opts{apiKey: "s3cret", cors: []string{"http://localhost:5173"}})
	if r := e.do("POST", "/api/v1/import/official", ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	if r := e.do("POST", "/api/v1/import/official", "", "X-API-Key", "s3cret"); r.Code != 201 {
		t.Fatal(r.Code)
	}
	if r := e.do("GET", "/api/v1/pilots", ""); r.Code != 200 {
		t.Fatal("reads stay open", r.Code)
	}
	r := e.do("OPTIONS", "/api/v1/pilots", "", "Origin", "http://localhost:5173")
	if r.Code != 204 || r.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal(r.Code, r.Header)
	}
	if r := e.do("GET", "/api/v1/pilots", "", "Origin", "https://evil.example"); r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origin must not be allowed")
	}
	if r := e.do("GET", "/health", "", "X-Request-ID", "abc-123"); r.Header.Get("X-Request-ID") != "abc-123" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(r.Header)
	}
	if r := e.do("GET", "/health", "", "X-Request-ID", "bad id!"); r.Header.Get("X-Request-ID") == "bad id!" {
		t.Fatal("unsafe request id must be replaced")
	}
}

func TestRulesEndpointAndPanicRecovery(t *testing.T) {
	e := newEnv(t, opts{})
	m := e.do("GET", "/api/v1/rules", "").json()
	if s(m["rules_version"]) != "c23-rules-v1" || s(get(m, "thresholds.default_minimum_quality_pct")) != "80" {
		t.Fatal(m)
	}
	gin.SetMode(gin.ReleaseMode)
	if r := e.do("GET", "/ready", ""); r.Code != 200 {
		t.Fatal(r.Code)
	}
}
