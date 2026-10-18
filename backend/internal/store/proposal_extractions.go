package store

import (
	"context"
	"encoding/json"
	"errors"

	"c23desk/internal/model"
	"github.com/jackc/pgx/v5"
)

type ProposalExtractionStore interface {
	GetProposalExtraction(context.Context, string) (*model.ProposalExtraction, error)
	SaveProposalExtraction(context.Context, string, string, json.RawMessage, json.RawMessage, *string) (*model.ProposalExtraction, error)
}

func (s *Store) GetProposalExtraction(ctx context.Context, uploadID string) (*model.ProposalExtraction, error) {
	var out model.ProposalExtraction
	err := s.pool.QueryRow(ctx, `SELECT id, upload_id, workspace_id, status, fields, warnings, error, created_at::text, updated_at::text
		FROM proposal_extractions WHERE upload_id=$1 AND workspace_id=$2`, uploadID, model.WorkspaceID(ctx)).
		Scan(&out.ID, &out.UploadID, &out.WorkspaceID, &out.Status, &out.Fields, &out.Warnings, &out.Error, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, err
	}
	return &out, nil
}

func (s *Store) SaveProposalExtraction(ctx context.Context, uploadID, status string, fields, warnings json.RawMessage, extractionErr *string) (*model.ProposalExtraction, error) {
	var out model.ProposalExtraction
	err := s.pool.QueryRow(ctx, `INSERT INTO proposal_extractions (upload_id, workspace_id, status, fields, warnings, error)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (upload_id) DO UPDATE SET status=EXCLUDED.status, fields=EXCLUDED.fields, warnings=EXCLUDED.warnings, error=EXCLUDED.error
		RETURNING id, upload_id, workspace_id, status, fields, warnings, error, created_at::text, updated_at::text`,
		uploadID, model.WorkspaceID(ctx), status, fields, warnings, extractionErr).
		Scan(&out.ID, &out.UploadID, &out.WorkspaceID, &out.Status, &out.Fields, &out.Warnings, &out.Error, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
