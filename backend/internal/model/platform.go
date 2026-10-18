package model

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// DefaultWorkspaceID is the workspace every pre-existing row moved into (migration 00010) and the one
// used when AUTH_MODE=off. Store calls without a workspace in their context act on it.
const DefaultWorkspaceID = "00000000-0000-0000-0000-000000000001"

// ErrUnknownProposal: a proposal id was given that does not exist in the caller's workspace.
var ErrUnknownProposal = errors.New("unknown proposal")

// ErrConflict: a uniqueness rule was hit (name already taken, member already present).
var ErrConflict = errors.New("conflict")

type ctxKey int

const (
	ctxWorkspace ctxKey = iota
	ctxPrincipal
)

// Principal is the signed-in caller for this request. Nil when AUTH_MODE=off.
type Principal struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	TokenID string `json:"token_id"`
	Role    string `json:"role"` // role in the request's workspace (viewer | analyst | admin)
}

// WithWorkspace scopes every store call made with the returned context to one workspace.
func WithWorkspace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxWorkspace, id)
}

// WorkspaceID returns the request's workspace, or the default workspace when none was set.
func WorkspaceID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxWorkspace).(string); ok && v != "" {
		return v
	}
	return DefaultWorkspaceID
}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxPrincipal, p)
}

func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxPrincipal).(*Principal)
	return p
}

// ActorID is the signed-in user's id for created_by / decided_by columns, nil when nobody is signed in.
func ActorID(ctx context.Context) *string {
	if p := PrincipalFrom(ctx); p != nil && p.UserID != "" {
		id := p.UserID
		return &id
	}
	return nil
}

// ---------- workspaces, users, tokens ----------

type Workspace struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Role      string          `json:"role,omitempty"` // the caller's role, when listed for a user
	Settings  json.RawMessage `json:"settings,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName *string   `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Member struct {
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName *string   `json:"display_name"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
}

// TokenAuth is what a valid bearer token resolves to.
type TokenAuth struct {
	TokenID string
	UserID  string
	Email   string
}

type APIToken struct {
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// ---------- uploads ----------

type UploadIn struct {
	Kind        string // balance_sheet | pilots
	Filename    string
	ContentType string
	Format      string // csv | pdf | xlsx | xls | docx
	SHA256      string
	Content     []byte
	Options     json.RawMessage
}

type ObjectUploadIn struct {
	ID          string
	Kind        string
	Filename    string
	ContentType string
	Format      string
	SizeBytes   int
	SHA256      string
	ObjectKey   string
	Options     json.RawMessage
}

// Upload is the job row without the file bytes.
type Upload struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Filename        string          `json:"filename"`
	ContentType     string          `json:"content_type"`
	Format          string          `json:"format"`
	SizeBytes       int             `json:"size_bytes"`
	SHA256          string          `json:"sha256"`
	Options         json.RawMessage `json:"options"`
	Status          string          `json:"status"`
	Attempts        int             `json:"attempts"`
	MaxAttempts     int             `json:"max_attempts"`
	NextAttemptAt   time.Time       `json:"next_attempt_at"`
	LastError       *string         `json:"last_error"`
	Result          json.RawMessage `json:"result"`
	Confirmed       json.RawMessage `json:"confirmed"`
	CreatedBy       *string         `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	StorageProvider string          `json:"storage_provider,omitempty"`
	ObjectKey       *string         `json:"-"`
	UploadedAt      *time.Time      `json:"uploaded_at,omitempty"`
}

// UploadJob is what the worker claims: the row's identity, the options and the original bytes.
type UploadJob struct {
	ID              string
	WorkspaceID     string
	Kind            string
	Filename        string
	ContentType     string
	Options         json.RawMessage
	Content         []byte
	StorageProvider string
	ObjectKey       string
	Attempts        int
	MaxAttempts     int
}

type CorrectionIn struct {
	UploadID       string
	RowRef         string
	Field          string
	OriginalValue  json.RawMessage
	CorrectedValue json.RawMessage
	Note           *string
}

type Correction struct {
	ID             int64           `json:"id"`
	UploadID       string          `json:"upload_id"`
	RowRef         string          `json:"row_ref"`
	Field          string          `json:"field"`
	OriginalValue  json.RawMessage `json:"original_value"`
	CorrectedValue json.RawMessage `json:"corrected_value"`
	Note           *string         `json:"note"`
	CreatedBy      *string         `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
}

// ---------- snapshots and approvals ----------

type SnapshotProposal struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type SnapshotIn struct {
	Kind            string
	Request         json.RawMessage
	Response        json.RawMessage
	Proposals       []SnapshotProposal
	RulesVersion    *string
	ContractVersion *string
}

type Snapshot struct {
	ID              string             `json:"id"`
	Kind            string             `json:"kind"`
	Request         json.RawMessage    `json:"request,omitempty"`
	Response        json.RawMessage    `json:"response,omitempty"`
	Proposals       []SnapshotProposal `json:"proposals"`
	RulesVersion    *string            `json:"rules_version"`
	ContractVersion *string            `json:"contract_version"`
	CreatedBy       *string            `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
}

type ApprovalIn struct {
	ProposalID      string
	ProposalVersion int
	Decision        string // approved | rejected | changes_requested
	SnapshotID      *string
	EngineOutcome   *string
	Comment         *string
}

type Approval struct {
	ID              int64     `json:"id"`
	ProposalID      string    `json:"proposal_id"`
	ProposalVersion int       `json:"proposal_version"`
	Decision        string    `json:"decision"`
	SnapshotID      *string   `json:"snapshot_id"`
	EngineOutcome   *string   `json:"engine_outcome"`
	Comment         *string   `json:"comment"`
	DecidedBy       *string   `json:"decided_by"`
	CreatedAt       time.Time `json:"created_at"`
	Stale           bool      `json:"stale"` // the proposal's inputs changed after this approval
}
