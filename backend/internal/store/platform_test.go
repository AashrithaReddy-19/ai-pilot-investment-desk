package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"c23desk/internal/model"
)

// Cross-organisation isolation, tokens, memberships, settings, snapshots, approvals and the upload queue,
// against a real Postgres (skipped without TEST_DATABASE_URL, like the other store tests).

func twoWorkspaces(t *testing.T, s *Store) (ctxA, ctxB context.Context, userA, userB *model.User) {
	t.Helper()
	ctx := context.Background()
	stamp := time.Now().UnixNano()
	var err error
	if userA, err = s.EnsureUser(ctx, fmt.Sprintf("a%d@a.test", stamp), nil); err != nil {
		t.Fatal(err)
	}
	if userB, err = s.EnsureUser(ctx, fmt.Sprintf("b%d@b.test", stamp), nil); err != nil {
		t.Fatal(err)
	}
	wa, err := s.CreateWorkspace(ctx, "Org A", userA.ID)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := s.CreateWorkspace(ctx, "Org B", userB.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(ctx, `DELETE FROM workspaces WHERE id IN ($1::uuid, $2::uuid)`, wa.ID, wb.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1::uuid, $2::uuid)`, userA.ID, userB.ID)
	})
	pa := &model.Principal{UserID: userA.ID, Email: userA.Email}
	pb := &model.Principal{UserID: userB.ID, Email: userB.Email}
	return model.WithPrincipal(model.WithWorkspace(ctx, wa.ID), pa), model.WithPrincipal(model.WithWorkspace(ctx, wb.ID), pb), userA, userB
}

func TestCrossWorkspaceIsolation(t *testing.T) {
	s := newTestStore(t)
	ctxA, ctxB, _, _ := twoWorkspaces(t, s)

	// the same proposal name, portfolio name and pilot plan id may exist in both organisations
	pa, err := s.CreateProposal(ctxA, model.ProposalIn{Name: "Invoice extraction", Inputs: json.RawMessage(`{"setup_cost_inr":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProposal(ctxB, model.ProposalIn{Name: "Invoice extraction", Inputs: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("names are unique per workspace, not globally: %v", err)
	}
	if _, err := s.GetProposal(ctxB, pa.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not read A's proposal: %v", err)
	}
	if _, err := s.UpdateProposal(ctxB, pa.ID, model.ProposalIn{Name: "hijack"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not edit A's proposal: %v", err)
	}
	if err := s.DeleteProposal(ctxB, pa.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not delete A's proposal: %v", err)
	}
	if _, err := s.AddEvidence(ctxB, model.EvidenceIn{ProposalID: pa.ID, Kind: "document", Source: "x"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not attach evidence to A's proposal: %v", err)
	}
	if ev, err := s.ListEvidence(ctxB, pa.ID); err != nil || len(ev) != 0 {
		t.Fatalf("B sees no evidence of A: %v %v", ev, err)
	}
	listB, _ := s.ListProposals(ctxB, true)
	for _, p := range listB {
		if p.ID == pa.ID {
			t.Fatal("A's proposal leaked into B's list")
		}
	}
	// linking B's observation to A's proposal is refused, A's own link works
	if _, _, err := s.AddObservation(ctxB, model.ObservationIn{PilotID: "AI1", ProposalID: &pa.ID, AdoptionPct: f(50)},
		model.DecisionIn{PilotID: "AI1", Decision: "GO", Metric: "none", Reason: "x"}); !errors.Is(err, model.ErrUnknownProposal) {
		t.Fatalf("cross-workspace link must be refused: %v", err)
	}
	if _, _, err := s.AddObservation(ctxA, model.ObservationIn{PilotID: "AI1", ProposalID: &pa.ID, AdoptionPct: f(71), QualityPct: f(88)},
		model.DecisionIn{PilotID: "AI1", Decision: "GO", Metric: "none", Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if o, err := s.LatestProposalObservation(ctxA, pa.ID); err != nil || *o.AdoptionPct != 71 {
		t.Fatalf("latest linked observation: %+v %v", o, err)
	}
	if h, _ := s.History(ctxB, "AI1", 10); len(h.Observations) != 0 {
		t.Fatalf("B must not see A's observations: %+v", h.Observations)
	}
	// plans: same pilot id in both, independent
	for _, c := range []context.Context{ctxA, ctxB} {
		if _, err := s.UpsertPlan(c, model.PilotPlan{PilotID: "AI1", Objective: "o", DurationDays: 30, StopRule: "s", Status: "planned"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SetPlanStatus(ctxA, "AI1", "active"); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.PilotPlan(ctxB, "AI1"); p.Status != "planned" {
		t.Fatalf("A's status change leaked into B: %s", p.Status)
	}
	// evaluations and audit stay in their workspace
	id, err := s.SaveEvaluation(ctxA, model.EvaluationIn{Request: json.RawMessage(`{}`), Independent: json.RawMessage(`{"selected":["AI1"]}`), Conservative: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvaluation(ctxB, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not read A's evaluation: %v", err)
	}
	if _, err := s.LatestEvaluationWithPilot(ctxB, "AI1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("reallocation source must not cross workspaces: %v", err)
	}
	_ = s.AddAuditEvent(ctxA, model.AuditEventIn{Action: "proposal.create", EntityType: "proposal", RequestID: "r1"})
	evA, _ := s.ListAuditEvents(ctxA, model.AuditFilter{Limit: 10})
	evB, _ := s.ListAuditEvents(ctxB, model.AuditFilter{Limit: 10})
	if len(evA) != 1 || len(evB) != 0 || evA[0].Actor == nil {
		t.Fatalf("audit rows must stay in their workspace and name the actor: A=%v B=%v", evA, evB)
	}
}

func TestTokensAndMemberships(t *testing.T) {
	s := newTestStore(t)
	ctxA, _, ua, ub := twoWorkspaces(t, s)
	ctx := context.Background()
	tok, err := s.CreateToken(ctx, ua.ID, "laptop", "raw-token-a")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.AuthenticateToken(ctx, "raw-token-a")
	if err != nil || a.UserID != ua.ID {
		t.Fatalf("token auth: %+v %v", a, err)
	}
	var stored string
	_ = s.pool.QueryRow(ctx, `SELECT token_sha256 FROM api_tokens WHERE id = $1::uuid`, tok.ID).Scan(&stored)
	if stored == "raw-token-a" || stored != TokenHash("raw-token-a") {
		t.Fatal("only the token hash may be stored")
	}
	if err := s.RevokeToken(ctx, ub.ID, tok.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a user cannot revoke someone else's token: %v", err)
	}
	if err := s.RevokeToken(ctx, ua.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateToken(ctx, "raw-token-a"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("a revoked token must not authenticate: %v", err)
	}

	wsA := model.WorkspaceID(ctxA)
	if r, _ := s.MemberRole(ctx, wsA, ua.ID); r != "admin" {
		t.Fatalf("creator is admin, got %q", r)
	}
	if _, err := s.MemberRole(ctx, wsA, ub.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B is not a member of A: %v", err)
	}
	if _, err := s.SetMember(ctxA, ub.Email, "viewer"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.UserWorkspaces(ctx, ub.ID); len(list) != 2 {
		t.Fatalf("B now belongs to two workspaces: %+v", list)
	}
	if err := s.RemoveMember(ctxA, ua.ID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("the last admin cannot be removed: %v", err)
	}
	if err := s.RemoveMember(ctxA, ub.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsSnapshotsAndApprovals(t *testing.T) {
	s := newTestStore(t)
	ctxA, ctxB, _, _ := twoWorkspaces(t, s)
	if _, err := s.PutSettings(ctxA, json.RawMessage(`{"budget":700000}`)); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.GetSettings(ctxB); string(b) != "{}" {
		t.Fatalf("settings are per workspace: %s", b)
	}
	p, _ := s.CreateProposal(ctxA, model.ProposalIn{Name: "Triage", Inputs: json.RawMessage(`{"setup_cost_inr":1}`)})
	snap, err := s.SaveSnapshot(ctxA, model.SnapshotIn{Kind: "dashboard", Request: json.RawMessage(`{"a":1}`),
		Response: json.RawMessage(`{"b":2}`), Proposals: []model.SnapshotProposal{{ID: p.ID, Name: p.Name, Version: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnapshot(ctxB, snap.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not read A's snapshot: %v", err)
	}
	if got, _ := s.GetSnapshot(ctxA, snap.ID); string(got.Response) != `{"b": 2}` || got.CreatedBy == nil {
		t.Fatalf("snapshot round trip: %s %v", got.Response, got.CreatedBy)
	}
	if list, _ := s.ListSnapshots(ctxA, "dashboard", 10); len(list) != 1 || list[0].Request != nil {
		t.Fatalf("snapshot list (no bodies): %+v", list)
	}
	ap, err := s.AddApproval(ctxA, model.ApprovalIn{ProposalID: p.ID, Decision: "approved", SnapshotID: &snap.ID})
	if err != nil || ap.ProposalVersion != 1 {
		t.Fatalf("approval: %+v %v", ap, err)
	}
	if _, err := s.AddApproval(ctxB, model.ApprovalIn{ProposalID: p.ID, Decision: "approved"}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B cannot approve A's proposal: %v", err)
	}
	if _, err := s.UpdateProposal(ctxA, p.ID, model.ProposalIn{Inputs: json.RawMessage(`{"setup_cost_inr":2}`)}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListApprovals(ctxA, p.ID); len(list) != 1 || !list[0].Stale {
		t.Fatalf("an approval goes stale when the inputs change: %+v", list)
	}
	if v, _ := s.ProposalVersions(ctxA, []string{p.ID}); v[p.ID] != 2 {
		t.Fatalf("versions: %v", v)
	}
	if _, err := s.ProposalsForEngine(ctxB, []string{p.ID}); !errors.Is(err, model.ErrUnknownProposal) {
		t.Fatalf("B cannot build an engine request from A's proposals: %v", err)
	}
}

func TestUploadQueueLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctxA, ctxB, _, _ := twoWorkspaces(t, s)
	ctx := context.Background()
	_, _ = s.pool.Exec(ctx, `UPDATE uploads SET status = 'confirmed' WHERE status IN ('received','failed','processing')`) // isolate from leftovers
	in := model.UploadIn{Kind: "balance_sheet", Filename: "bs.csv", ContentType: "text/csv", Format: "csv",
		SHA256: TokenHash(fmt.Sprint(time.Now().UnixNano())), Content: []byte("Account,Amount\nAI,1\n"), Options: json.RawMessage(`{"unit":"lakh"}`)}
	u, created, err := s.CreateUpload(ctxA, in)
	if err != nil || !created || u.Status != "received" || u.CreatedBy == nil {
		t.Fatalf("create: %+v %v %v", u, created, err)
	}
	if again, created, _ := s.CreateUpload(ctxA, in); created || again.ID != u.ID {
		t.Fatal("the same file twice is one upload")
	}
	if _, created, _ := s.CreateUpload(ctxB, in); !created {
		t.Fatal("another workspace uploading the same bytes gets its own upload")
	}
	if _, err := s.GetUpload(ctxB, u.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not read A's upload: %v", err)
	}
	if _, err := s.UploadContent(ctxB, u.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B must not download A's file: %v", err)
	}

	// claim → transient failure → retry → success
	j, err := s.ClaimUploadJob(ctx)
	if err != nil || j == nil || j.Attempts != 1 || string(j.Content) != "Account,Amount\nAI,1\n" {
		t.Fatalf("claim: %+v %v", j, err)
	}
	if err := s.FinishUploadJob(ctx, j.ID, nil, "engine down", false, 0); err != nil {
		t.Fatal(err)
	}
	j2, _ := s.ClaimUploadJob(ctx)
	for j2 != nil && j2.ID != j.ID { // the other workspace's copy may be claimed first
		_ = s.FinishUploadJob(ctx, j2.ID, json.RawMessage(`{}`), "", false, 0)
		j2, _ = s.ClaimUploadJob(ctx)
	}
	if j2 == nil || j2.Attempts != 2 {
		t.Fatalf("a failed job is retried with its attempt count: %+v", j2)
	}
	if err := s.FinishUploadJob(ctx, j2.ID, json.RawMessage(`{"candidates":[{"row":2}]}`), "", false, 0); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetUpload(ctxA, u.ID)
	if got.Status != "parsed" || got.LastError != nil {
		t.Fatalf("parsed: %+v", got)
	}
	c, err := s.AddCorrection(ctxA, model.CorrectionIn{UploadID: u.ID, RowRef: "2", Field: "amount_inr",
		OriginalValue: json.RawMessage(`1`), CorrectedValue: json.RawMessage(`2`)})
	if err != nil || string(c.OriginalValue) != "1" {
		t.Fatalf("correction: %+v %v", c, err)
	}
	if _, err := s.AddCorrection(ctxB, model.CorrectionIn{UploadID: u.ID, RowRef: "2", Field: "amount_inr", CorrectedValue: json.RawMessage(`9`)}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("B cannot correct A's upload: %v", err)
	}
	if done, err := s.ConfirmUpload(ctxA, u.ID, json.RawMessage(`{"ai_burn_to_date":2}`)); err != nil || done.Status != "confirmed" {
		t.Fatalf("confirm: %+v %v", done, err)
	}

	// a rejected file fails permanently and can be retried by hand
	in2 := in
	in2.SHA256 = TokenHash(fmt.Sprint("x", time.Now().UnixNano()))
	u2, _, _ := s.CreateUpload(ctxA, in2)
	for j3, _ := s.ClaimUploadJob(ctx); j3 != nil; j3, _ = s.ClaimUploadJob(ctx) {
		_ = s.FinishUploadJob(ctx, j3.ID, nil, "bad file", true, 0)
	}
	if got, _ := s.GetUpload(ctxA, u2.ID); got.Status != "failed" || got.Attempts != got.MaxAttempts {
		t.Fatalf("permanent failure uses up the attempts: %+v", got)
	}
	if j, _ := s.ClaimUploadJob(ctx); j != nil {
		t.Fatalf("a permanently failed upload is not claimed again: %+v", j)
	}
	if r, err := s.RetryUpload(ctxA, u2.ID); err != nil || r.Status != "received" || r.Attempts != 0 {
		t.Fatalf("manual retry: %+v %v", r, err)
	}
}
