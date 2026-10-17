// Package rules is the HTTP client for the stateless FastAPI rules engine.
package rules

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"c23desk/internal/model"
)

const maxResponseBytes = 10 << 20

type Client struct {
	base string
	http *http.Client
}

func New(base string, timeout time.Duration) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     60 * time.Second,
			},
		},
	}
}

// Do sends a request and returns whatever the engine answered, including 4xx/5xx statuses.
// An error means the engine could not be reached or read (network, timeout, oversize).
func (c *Client) Do(ctx context.Context, method, path string, body []byte, reqID string) (*model.Upstream, error) {
	ct := ""
	if body != nil {
		ct = "application/json"
	}
	return c.do(ctx, method, path, body, reqID, ct)
}

// DoUpload forwards a body with the caller's content-type (multipart uploads skip the cache).
func (c *Client) DoUpload(ctx context.Context, path string, body []byte, contentType, reqID string) (*model.Upstream, error) {
	return c.do(ctx, http.MethodPost, path, body, reqID, contentType)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, reqID, contentType string) (*model.Upstream, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json, text/plain")
	if reqID != "" {
		req.Header.Set("X-Request-ID", reqID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxResponseBytes {
		return nil, errors.New("rules engine response too large")
	}
	return &model.Upstream{Status: resp.StatusCode, Body: b, ContentType: resp.Header.Get("Content-Type")}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	r, err := c.Do(ctx, http.MethodGet, "/health", nil, "")
	if err != nil {
		return err
	}
	if r.Status != http.StatusOK {
		return fmt.Errorf("rules engine /health returned %d", r.Status)
	}
	return nil
}
