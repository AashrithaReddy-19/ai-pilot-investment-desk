package model

import "encoding/json"

type ProposalExtraction struct {
	ID          string          `json:"id"`
	UploadID    string          `json:"upload_id"`
	WorkspaceID string          `json:"-"`
	Status      string          `json:"status"`
	Fields      json.RawMessage `json:"fields"`
	Warnings    json.RawMessage `json:"warnings"`
	Error       *string         `json:"error,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}
