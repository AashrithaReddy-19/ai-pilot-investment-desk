package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"c23desk/internal/model"
)

const uploadCols = `id::text, kind, filename, content_type, format, size_bytes, sha256, options, status, attempts, max_attempts,
	next_attempt_at, last_error, result, confirmed, created_by::text, created_at, updated_at, storage_provider, object_key, uploaded_at`

func scanUpload(row pgx.Row) (*model.Upload, error) {
	var u model.Upload
	err := row.Scan(&u.ID, &u.Kind, &u.Filename, &u.ContentType, &u.Format, &u.SizeBytes, &u.SHA256, &u.Options, &u.Status,
		&u.Attempts, &u.MaxAttempts, &u.NextAttemptAt, &u.LastError, &u.Result, &u.Confirmed, &u.CreatedBy, &u.CreatedAt, &u.UpdatedAt,
		&u.StorageProvider, &u.ObjectKey, &u.UploadedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}

	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) CreateObjectUpload(ctx context.Context, in model.ObjectUploadIn) (*model.Upload, bool, error) {
	opts := []byte(in.Options)
	if len(opts) == 0 {
		opts = []byte(`{}`)
	}
	u, err := scanUpload(s.pool.QueryRow(ctx, `
		INSERT INTO uploads (id, workspace_id, kind, filename, content_type, format, size_bytes, sha256, content, object_key, storage_provider, options, status, created_by)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, NULL, $9, 's3', $10, 'pending_upload', $11::uuid)
		ON CONFLICT (workspace_id, kind, sha256) DO NOTHING
		RETURNING `+uploadCols,
		in.ID, model.WorkspaceID(ctx), in.Kind, in.Filename, in.ContentType, in.Format, in.SizeBytes, in.SHA256,
		in.ObjectKey, opts, model.ActorID(ctx)))
	if errors.Is(err, model.ErrNotFound) {
		existing, getErr := scanUpload(s.pool.QueryRow(ctx, `SELECT `+uploadCols+` FROM uploads
			WHERE workspace_id = $1 AND kind = $2 AND sha256 = $3`, model.WorkspaceID(ctx), in.Kind, in.SHA256))
		return existing, false, getErr
	}
	return u, err == nil, err
}

func (s *Store) CompleteObjectUpload(ctx context.Context, id string) (*model.Upload, error) {
	return scanUpload(s.pool.QueryRow(ctx, `UPDATE uploads SET status = 'received', uploaded_at = now(), last_error = NULL
		WHERE id::text = $1 AND workspace_id = $2 AND storage_provider = 's3' AND status = 'pending_upload'
		RETURNING `+uploadCols, id, model.WorkspaceID(ctx)))
}

// CreateUpload stores the original file and queues it. The same bytes uploaded again for the same kind
// return the existing upload (created=false) instead of a duplicate.
func (s *Store) CreateUpload(ctx context.Context, in model.UploadIn) (*model.Upload, bool, error) {
	opts := []byte(in.Options)
	if len(opts) == 0 {
		opts = []byte(`{}`)
	}
	u, err := scanUpload(s.pool.QueryRow(ctx, `
		INSERT INTO uploads (workspace_id, kind, filename, content_type, format, size_bytes, sha256, content, options, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::uuid)
		ON CONFLICT (workspace_id, kind, sha256) DO NOTHING
		RETURNING `+uploadCols,
		model.WorkspaceID(ctx), in.Kind, in.Filename, in.ContentType, in.Format, len(in.Content), in.SHA256, in.Content, opts, model.ActorID(ctx)))
	if errors.Is(err, model.ErrNotFound) { // conflict: already uploaded
		existing, err := scanUpload(s.pool.QueryRow(ctx, `SELECT `+uploadCols+` FROM uploads
			WHERE workspace_id = $1 AND kind = $2 AND sha256 = $3`, model.WorkspaceID(ctx), in.Kind, in.SHA256))
		return existing, false, err
	}
	return u, err == nil, err
}

func (s *Store) GetUpload(ctx context.Context, id string) (*model.Upload, error) {
	return scanUpload(s.pool.QueryRow(ctx, `SELECT `+uploadCols+` FROM uploads WHERE id::text = $1 AND workspace_id = $2`,
		id, model.WorkspaceID(ctx)))
}

// UploadContent returns the original bytes and metadata needed to serve or re-send the file.
func (s *Store) UploadContent(ctx context.Context, id string) (*model.UploadJob, error) {
	var j model.UploadJob
	err := s.pool.QueryRow(ctx, `SELECT id::text, workspace_id::text, kind, filename, content_type, options, content, attempts, max_attempts,
		storage_provider, COALESCE(object_key, '')
		FROM uploads WHERE id::text = $1 AND workspace_id = $2`, id, model.WorkspaceID(ctx),
	).Scan(&j.ID, &j.WorkspaceID, &j.Kind, &j.Filename, &j.ContentType, &j.Options, &j.Content, &j.Attempts, &j.MaxAttempts,
		&j.StorageProvider, &j.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) ListUploads(ctx context.Context, limit int) ([]model.Upload, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+uploadCols+` FROM uploads WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`, model.WorkspaceID(ctx), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Upload{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		u.Result = nil // the list stays small; open one upload for its result
		u.Confirmed = nil
		out = append(out, *u)
	}
	return out, rows.Err()
}

// RetryUpload puts a failed upload back in the queue with a fresh attempt budget.
func (s *Store) RetryUpload(ctx context.Context, id string) (*model.Upload, error) {
	return scanUpload(s.pool.QueryRow(ctx, `
		UPDATE uploads SET status = 'received', attempts = 0, next_attempt_at = now(), last_error = NULL
		WHERE id::text = $1 AND workspace_id = $2 AND status = 'failed'
		RETURNING `+uploadCols, id, model.WorkspaceID(ctx)))
}

// ClaimUploadJob takes the next due job across all workspaces (FOR UPDATE SKIP LOCKED, so several
// gateway instances never process the same upload) and marks it processing.
func (s *Store) ClaimUploadJob(ctx context.Context) (*model.UploadJob, error) {
	var j model.UploadJob
	err := s.pool.QueryRow(ctx, `
		UPDATE uploads SET status = 'processing', attempts = attempts + 1
		WHERE id = (
			SELECT id FROM uploads
			WHERE (status = 'received' OR (status = 'failed' AND attempts < max_attempts)) AND next_attempt_at <= now()
			ORDER BY next_attempt_at, created_at LIMIT 1
			FOR UPDATE SKIP LOCKED)
	RETURNING id::text, workspace_id::text, kind, filename, content_type, options, content, attempts, max_attempts,
		storage_provider, COALESCE(object_key, '')`,
	).Scan(&j.ID, &j.WorkspaceID, &j.Kind, &j.Filename, &j.ContentType, &j.Options, &j.Content, &j.Attempts, &j.MaxAttempts,
		&j.StorageProvider, &j.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// FinishUploadJob records a job outcome. On failure the job is retried after `retryIn` until it runs
// out of attempts; a rejected file (permanent=true) is failed at once.
func (s *Store) FinishUploadJob(ctx context.Context, id string, result json.RawMessage, jobErr string, permanent bool, retryIn time.Duration) error {
	if jobErr == "" {
		_, err := s.pool.Exec(ctx, `UPDATE uploads SET status = 'parsed', result = $2, last_error = NULL WHERE id::text = $1`,
			id, []byte(result))
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE uploads SET status = 'failed', last_error = $2,
			attempts = CASE WHEN $3 THEN max_attempts ELSE attempts END,
			next_attempt_at = now() + make_interval(secs => $4)
		WHERE id::text = $1`, id, jobErr, permanent, retryIn.Seconds())
	return err
}

// RecoverStuckUploads requeues jobs left in processing (a gateway that died mid-job) after `olderThan`.
func (s *Store) RecoverStuckUploads(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE uploads SET status = 'failed', last_error = 'interrupted; will retry', next_attempt_at = now()
		WHERE status = 'processing' AND updated_at < now() - make_interval(secs => $1)`, olderThan.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ConfirmUpload stores the engine's confirm answer and closes the review.
func (s *Store) ConfirmUpload(ctx context.Context, id string, confirmed json.RawMessage) (*model.Upload, error) {
	return scanUpload(s.pool.QueryRow(ctx, `
		UPDATE uploads SET status = 'confirmed', confirmed = $3
		WHERE id::text = $1 AND workspace_id = $2 AND status IN ('parsed', 'confirmed')
		RETURNING `+uploadCols, id, model.WorkspaceID(ctx), []byte(confirmed)))
}

// AddCorrection records a reviewed value next to the original. The upload must be in this workspace.
func (s *Store) AddCorrection(ctx context.Context, in model.CorrectionIn) (*model.Correction, error) {
	var c model.Correction
	orig := []byte(in.OriginalValue)
	if len(orig) == 0 {
		orig = nil
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO upload_corrections (upload_id, row_ref, field, original_value, corrected_value, note, created_by)
		SELECT u.id, $3, $4, $5, $6, $7, $8::uuid FROM uploads u WHERE u.id::text = $1 AND u.workspace_id = $2
		RETURNING id, upload_id::text, row_ref, field, original_value, corrected_value, note, created_by::text, created_at`,
		in.UploadID, model.WorkspaceID(ctx), in.RowRef, in.Field, orig, []byte(in.CorrectedValue), in.Note, model.ActorID(ctx),
	).Scan(&c.ID, &c.UploadID, &c.RowRef, &c.Field, &c.OriginalValue, &c.CorrectedValue, &c.Note, &c.CreatedBy, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCorrections lists an upload's corrections in the order they were made (the latest per field wins).
func (s *Store) ListCorrections(ctx context.Context, uploadID string) ([]model.Correction, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.upload_id::text, c.row_ref, c.field, c.original_value, c.corrected_value, c.note, c.created_by::text, c.created_at
		FROM upload_corrections c JOIN uploads u ON u.id = c.upload_id
		WHERE c.upload_id::text = $1 AND u.workspace_id = $2 ORDER BY c.id`, uploadID, model.WorkspaceID(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Correction{}
	for rows.Next() {
		var c model.Correction
		if err := rows.Scan(&c.ID, &c.UploadID, &c.RowRef, &c.Field, &c.OriginalValue, &c.CorrectedValue, &c.Note, &c.CreatedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
