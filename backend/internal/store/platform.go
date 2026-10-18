package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"c23desk/internal/model"
)

// TokenHash is how API tokens are stored: hex SHA-256 of the raw token. The raw token is shown once.
func TokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func isUnique(err error) bool { return err != nil && strings.Contains(err.Error(), "SQLSTATE 23505") }

// ---------- users and tokens ----------

// EnsureUser returns the user with this email, creating it when absent (case-insensitive match).
func (s *Store) EnsureUser(ctx context.Context, email string, displayName *string) (*model.User, error) {
	var u model.User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name) VALUES ($1, $2)
		ON CONFLICT (lower(email)) DO UPDATE SET display_name = COALESCE(EXCLUDED.display_name, users.display_name)
		RETURNING id::text, email, display_name, created_at`, strings.TrimSpace(email), displayName,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateToken stores the hash of a new raw token for the user and returns its id.
func (s *Store) CreateToken(ctx context.Context, userID, label, raw string) (*model.APIToken, error) {
	var t model.APIToken
	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_tokens (user_id, token_sha256, label) VALUES ($1::uuid, $2, $3)
		RETURNING id::text, label, created_at, last_used_at, revoked_at`, userID, TokenHash(raw), label,
	).Scan(&t.ID, &t.Label, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// AuthenticateToken resolves a raw bearer token. Revoked or unknown tokens are ErrNotFound.
// last_used_at is refreshed at most once a minute to keep reads cheap.
func (s *Store) AuthenticateToken(ctx context.Context, raw string) (*model.TokenAuth, error) {
	var a model.TokenAuth
	err := s.pool.QueryRow(ctx, `
		SELECT t.id::text, u.id::text, u.email FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_sha256 = $1 AND t.revoked_at IS NULL`, TokenHash(raw),
	).Scan(&a.TokenID, &a.UserID, &a.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now()
		WHERE id = $1::uuid AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, a.TokenID)
	return &a, nil
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]model.APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, label, created_at, last_used_at, revoked_at FROM api_tokens
		WHERE user_id = $1::uuid ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.APIToken{}
	for rows.Next() {
		var t model.APIToken
		if err := rows.Scan(&t.ID, &t.Label, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken revokes one of the user's own tokens.
func (s *Store) RevokeToken(ctx context.Context, userID, tokenID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE api_tokens SET revoked_at = now()
		WHERE id::text = $1 AND user_id = $2::uuid AND revoked_at IS NULL`, tokenID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return nil
}

// ---------- workspaces and memberships ----------

// CreateWorkspace makes a workspace with the creator as its admin, in one transaction.
func (s *Store) CreateWorkspace(ctx context.Context, name, adminUserID string) (*model.Workspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var w model.Workspace
	err = tx.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ($1)
		RETURNING id::text, name, settings, created_at, updated_at`, strings.TrimSpace(name),
	).Scan(&w.ID, &w.Name, &w.Settings, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1::uuid, $2::uuid, 'admin')`,
		w.ID, adminUserID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	w.Role = "admin"
	return &w, nil
}

// UserWorkspaces lists the workspaces a user belongs to, with the user's role in each.
func (s *Store) UserWorkspaces(ctx context.Context, userID string) ([]model.Workspace, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT w.id::text, w.name, m.role, w.created_at, w.updated_at
		FROM memberships m JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.user_id = $1::uuid ORDER BY w.created_at, w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Workspace{}
	for rows.Next() {
		var w model.Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.Role, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// MemberRole is the user's role in the workspace, ErrNotFound when not a member.
func (s *Store) MemberRole(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := s.pool.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id::text = $1 AND user_id = $2::uuid`,
		workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", model.ErrNotFound
	}
	return role, err
}

// ListMembers lists the request workspace's members.
func (s *Store) ListMembers(ctx context.Context) ([]model.Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id::text, u.email, u.display_name, m.role, m.created_at
		FROM memberships m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1 ORDER BY m.created_at, u.email`, model.WorkspaceID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Member{}
	for rows.Next() {
		var m model.Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMember adds the user (by email, created when new) to the request workspace or changes their role.
func (s *Store) SetMember(ctx context.Context, email, role string) (*model.Member, error) {
	u, err := s.EnsureUser(ctx, email, nil)
	if err != nil {
		return nil, err
	}
	var m model.Member
	err = s.pool.QueryRow(ctx, `
		INSERT INTO memberships (workspace_id, user_id, role) VALUES ($1, $2::uuid, $3)
		ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role
		RETURNING created_at`, model.WorkspaceID(ctx), u.ID, role).Scan(&m.CreatedAt)
	if err != nil {
		return nil, err
	}
	m.UserID, m.Email, m.DisplayName, m.Role = u.ID, u.Email, u.DisplayName, role
	return &m, nil
}

// RemoveMember removes a member; the last admin cannot be removed (ErrConflict).
func (s *Store) RemoveMember(ctx context.Context, userID string) error {
	ws := model.WorkspaceID(ctx)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM memberships WHERE workspace_id = $1 AND user_id::text = $2 FOR UPDATE`, ws, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == "admin" {
		var admins int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM memberships WHERE workspace_id = $1 AND role = 'admin'`, ws).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return model.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM memberships WHERE workspace_id = $1 AND user_id::text = $2`, ws, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---------- workspace settings ----------

func (s *Store) GetSettings(ctx context.Context) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT settings FROM workspaces WHERE id = $1`, model.WorkspaceID(ctx)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	return raw, err
}

func (s *Store) PutSettings(ctx context.Context, settings json.RawMessage) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, `UPDATE workspaces SET settings = $2 WHERE id = $1 RETURNING settings`,
		model.WorkspaceID(ctx), []byte(settings)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	return raw, err
}

// ---------- snapshots ----------

func (s *Store) SaveSnapshot(ctx context.Context, in model.SnapshotIn) (*model.Snapshot, error) {
	props := in.Proposals
	if props == nil {
		props = []model.SnapshotProposal{}
	}
	pj, _ := json.Marshal(props)
	var out model.Snapshot
	var pRaw []byte
	err := s.pool.QueryRow(ctx, `
		INSERT INTO snapshots (workspace_id, kind, request, response, proposals, rules_version, contract_version, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid)
		RETURNING id::text, kind, proposals, rules_version, contract_version, created_by::text, created_at`,
		model.WorkspaceID(ctx), in.Kind, []byte(in.Request), []byte(in.Response), pj, in.RulesVersion, in.ContractVersion, model.ActorID(ctx),
	).Scan(&out.ID, &out.Kind, &pRaw, &out.RulesVersion, &out.ContractVersion, &out.CreatedBy, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(pRaw, &out.Proposals)
	return &out, nil
}

// GetSnapshot returns one snapshot of the request workspace, with request and response.
func (s *Store) GetSnapshot(ctx context.Context, id string) (*model.Snapshot, error) {
	var out model.Snapshot
	var pRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, kind, request, response, proposals, rules_version, contract_version, created_by::text, created_at
		FROM snapshots WHERE id::text = $1 AND workspace_id = $2`, id, model.WorkspaceID(ctx),
	).Scan(&out.ID, &out.Kind, &out.Request, &out.Response, &pRaw, &out.RulesVersion, &out.ContractVersion, &out.CreatedBy, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(pRaw, &out.Proposals)
	return &out, nil
}

// ListSnapshots lists snapshots (no bodies), newest first, optionally of one kind. limit is clamped to 1..50.
func (s *Store) ListSnapshots(ctx context.Context, kind string, limit int) ([]model.Snapshot, error) {
	if limit < 1 || limit > 50 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, kind, proposals, rules_version, contract_version, created_by::text, created_at
		FROM snapshots WHERE workspace_id = $1 AND ($2 = '' OR kind = $2)
		ORDER BY created_at DESC, id DESC LIMIT $3`, model.WorkspaceID(ctx), kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Snapshot{}
	for rows.Next() {
		var sn model.Snapshot
		var pRaw []byte
		if err := rows.Scan(&sn.ID, &sn.Kind, &pRaw, &sn.RulesVersion, &sn.ContractVersion, &sn.CreatedBy, &sn.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(pRaw, &sn.Proposals)
		out = append(out, sn)
	}
	return out, rows.Err()
}

// ---------- approvals ----------

// AddApproval records a decision on the proposal's current version (the version is read in the same
// statement, so an approval can never name a version the proposal did not have).
func (s *Store) AddApproval(ctx context.Context, in model.ApprovalIn) (*model.Approval, error) {
	var a model.Approval
	ws := model.WorkspaceID(ctx)
	if in.SnapshotID != nil {
		var ok bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM snapshots WHERE id::text = $1 AND workspace_id = $2)`,
			*in.SnapshotID, ws).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("snapshot: %w", model.ErrNotFound)
		}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO approvals (workspace_id, proposal_id, proposal_version, decision, snapshot_id, engine_outcome, comment, decided_by)
		SELECT p.workspace_id, p.id, p.version, $3, $4::uuid, $5, $6, $7::uuid
		FROM proposals p WHERE p.id::text = $1 AND p.workspace_id = $2
		RETURNING id, proposal_id::text, proposal_version, decision, snapshot_id::text, engine_outcome, comment, decided_by::text, created_at`,
		in.ProposalID, ws, in.Decision, in.SnapshotID, in.EngineOutcome, in.Comment, model.ActorID(ctx),
	).Scan(&a.ID, &a.ProposalID, &a.ProposalVersion, &a.Decision, &a.SnapshotID, &a.EngineOutcome, &a.Comment, &a.DecidedBy, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListApprovals lists a proposal's approvals, newest first, each marked stale when the inputs changed since.
func (s *Store) ListApprovals(ctx context.Context, proposalID string) ([]model.Approval, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.proposal_id::text, a.proposal_version, a.decision, a.snapshot_id::text, a.engine_outcome, a.comment,
		       a.decided_by::text, a.created_at, a.proposal_version < p.version
		FROM approvals a JOIN proposals p ON p.id = a.proposal_id
		WHERE a.proposal_id::text = $1 AND a.workspace_id = $2
		ORDER BY a.created_at DESC, a.id DESC LIMIT 100`, proposalID, model.WorkspaceID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Approval{}
	for rows.Next() {
		var a model.Approval
		if err := rows.Scan(&a.ID, &a.ProposalID, &a.ProposalVersion, &a.Decision, &a.SnapshotID, &a.EngineOutcome, &a.Comment,
			&a.DecidedBy, &a.CreatedAt, &a.Stale); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ProposalsForEngine returns the full proposals (with inputs) of the request workspace: the given ids
// in that order, or every active proposal when ids is empty. A missing id is ErrUnknownProposal.
func (s *Store) ProposalsForEngine(ctx context.Context, ids []string) ([]model.Proposal, error) {
	ws := model.WorkspaceID(ctx)
	var rows pgx.Rows
	var err error
	if len(ids) == 0 {
		rows, err = s.pool.Query(ctx, `
			SELECT id::text, name, status, version, inputs, notes, created_at, updated_at FROM proposals
			WHERE workspace_id = $1 AND status = 'active' ORDER BY created_at, id LIMIT 50`, ws)
	} else {
		rows, err = s.pool.Query(ctx, `
			SELECT id::text, name, status, version, inputs, notes, created_at, updated_at FROM proposals
			WHERE workspace_id = $1 AND id::text = ANY($2)`, ws, ids)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]model.Proposal{}
	order := []string{}
	for rows.Next() {
		var p model.Proposal
		if err := rows.Scan(&p.ID, &p.Name, &p.Status, &p.Version, &p.Inputs, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		byID[p.ID] = p
		order = append(order, p.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		order = ids
	}
	out := make([]model.Proposal, 0, len(order))
	for _, id := range order {
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("%w: %s", model.ErrUnknownProposal, id)
		}
		out = append(out, p)
	}
	return out, nil
}

// ProposalVersions returns the current inputs version for each id of the request workspace (missing ids absent).
func (s *Store) ProposalVersions(ctx context.Context, ids []string) (map[string]int, error) {
	out := map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id::text, version FROM proposals WHERE workspace_id = $1 AND id::text = ANY($2)`,
		model.WorkspaceID(ctx), ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

// LatestProposalObservation returns the newest observation linked to the proposal (ErrNotFound when none),
// so stored evidence feeds the next engine run instead of sitting unused.
func (s *Store) LatestProposalObservation(ctx context.Context, proposalID string) (*model.Observation, error) {
	var o model.Observation
	err := s.pool.QueryRow(ctx, `
		SELECT id, pilot_id, proposal_id::text, adoption_pct, quality_pct, to_char(observed_on, 'YYYY-MM-DD'), source, created_at
		FROM observations WHERE proposal_id::text = $1 AND workspace_id = $2
		ORDER BY observed_on DESC, id DESC LIMIT 1`, proposalID, model.WorkspaceID(ctx),
	).Scan(&o.ID, &o.PilotID, &o.ProposalID, &o.AdoptionPct, &o.QualityPct, &o.ObservedOn, &o.Source, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
