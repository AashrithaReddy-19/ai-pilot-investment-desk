// Package aiclient calls the internal FastAPI AI service for ADVISORY enrichment only.
//
// Every call has a hard timeout and context cancellation; responses are size-limited and validated.
// Any failure returns Status "degraded" and the caller continues with deterministic results.
// AI output never feeds finance, budget, stop rules, overlap accounting or approval.
package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Status values reported to API clients as ai_status.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDisabled = "disabled"
)

const maxResponse = 1 << 20

// Text is one pilot's text for semantic comparison.
type Text struct {
	ID           string  `json:"id"`
	Text         string  `json:"text"`
	OverlapGroup *string `json:"overlap_group"`
}

// Pair is an advisory similarity result.
type Pair struct {
	A               string  `json:"pilot_a"`
	B               string  `json:"pilot_b"`
	Similarity      float64 `json:"semantic_similarity"`
	PossibleOverlap bool    `json:"possible_overlap"`
	Band            string  `json:"band"` // likely | possible | unlikely (advisory certainty)
	Reason          string  `json:"reason"`
	Method          string  `json:"method"`
}

// OverlapResponse from /internal/semantic-overlap.
type OverlapResponse struct {
	ModelName    string `json:"model_name"`
	ModelVersion string `json:"model_version"`
	Advisory     bool   `json:"advisory"`
	Pairs        []Pair `json:"pairs"`
}

// ExplainResponse from /internal/explain.
type ExplainResponse struct {
	ModelName    string   `json:"model_name"`
	ModelVersion string   `json:"model_version"`
	Explanation  string   `json:"explanation"`
	Grounded     bool     `json:"grounded"`
	Checks       []string `json:"checks"`
}

// Client is safe for concurrent use. A nil *Client or empty base URL means AI is disabled.
type Client struct {
	base    string
	http    *http.Client
	timeout time.Duration
	log     *slog.Logger
	// memoised live status, so cache hits can report the CURRENT AI state cheaply
	statusMu sync.Mutex
	status   string
	statusAt time.Time
}

// statusTTL bounds how stale a reported ai_status can be.
const statusTTL = 3 * time.Second

func (c *Client) remember(st string) {
	c.statusMu.Lock()
	c.status, c.statusAt = st, time.Now()
	c.statusMu.Unlock()
}

// LiveStatus returns the current AI status (health probe memoised for statusTTL; real calls also update it).
func (c *Client) LiveStatus(ctx context.Context) string {
	if !c.Enabled() {
		return StatusDisabled
	}
	c.statusMu.Lock()
	st, at := c.status, c.statusAt
	c.statusMu.Unlock()
	if st != "" && time.Since(at) < statusTTL {
		return st
	}
	return c.Health(ctx)
}

// New builds a client. timeout should be short (1-2s).
func New(base string, timeout time.Duration, log *slog.Logger) *Client {
	return &Client{base: base, timeout: timeout, log: log, http: &http.Client{Timeout: timeout}}
}

// Enabled reports whether an AI service is configured.
func (c *Client) Enabled() bool { return c != nil && c.base != "" }

func (c *Client) post(ctx context.Context, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ai service returned HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxResponse))
	return dec.Decode(out)
}

// Health checks the AI service quickly.
func (c *Client) Health(ctx context.Context) string {
	if !c.Enabled() {
		return StatusDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		c.remember(StatusDegraded)
		return StatusDegraded
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.remember(StatusDegraded)
		return StatusDegraded
	}
	c.remember(StatusOK)
	return StatusOK
}

// SemanticOverlap asks for advisory similarity between pilot texts. Returns (nil, status) on any failure.
func (c *Client) SemanticOverlap(ctx context.Context, texts []Text) (*OverlapResponse, string) {
	if !c.Enabled() {
		return nil, StatusDisabled
	}
	var out OverlapResponse
	if err := c.post(ctx, "/internal/semantic-overlap", map[string]any{"pilots": texts}, &out); err != nil {
		c.log.Warn("ai service degraded", "endpoint", "semantic-overlap", "error", err.Error())
		c.remember(StatusDegraded)
		return nil, StatusDegraded
	}
	if err := validateOverlap(out, texts); err != nil {
		c.log.Warn("ai response rejected", "endpoint", "semantic-overlap", "error", err.Error())
		c.remember(StatusDegraded)
		return nil, StatusDegraded
	}
	c.remember(StatusOK)
	return &out, StatusOK
}

func validateOverlap(r OverlapResponse, texts []Text) error {
	ids := map[string]bool{}
	for _, t := range texts {
		ids[t.ID] = true
	}
	if r.ModelName == "" || !r.Advisory {
		return errors.New("response missing model_name or advisory flag")
	}
	for _, p := range r.Pairs {
		if !ids[p.A] || !ids[p.B] || p.A == p.B {
			return fmt.Errorf("response references unknown pilot pair %q/%q", p.A, p.B)
		}
		if p.Similarity < 0 || p.Similarity > 1 || p.Similarity != p.Similarity {
			return fmt.Errorf("similarity %v out of range", p.Similarity)
		}
	}
	return nil
}

// Explain asks for a plain-language explanation of an evaluation. The service must report it grounded.
func (c *Client) Explain(ctx context.Context, evaluation any) (*ExplainResponse, string) {
	if !c.Enabled() {
		return nil, StatusDisabled
	}
	var out ExplainResponse
	if err := c.post(ctx, "/internal/explain", map[string]any{"evaluation": evaluation}, &out); err != nil {
		c.log.Warn("ai service degraded", "endpoint", "explain", "error", err.Error())
		c.remember(StatusDegraded)
		return nil, StatusDegraded
	}
	if !out.Grounded || out.Explanation == "" || len(out.Explanation) > 4000 {
		c.log.Warn("ai response rejected", "endpoint", "explain", "reason", "not grounded or empty")
		c.remember(StatusDegraded)
		return nil, StatusDegraded
	}
	c.remember(StatusOK)
	return &out, StatusOK
}
