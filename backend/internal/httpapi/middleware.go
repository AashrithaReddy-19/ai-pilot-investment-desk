package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
)

const ctxReqID = "request_id"

func reqID(c *gin.Context) string { return c.GetString(ctxReqID) }

// requestID accepts a safe caller-supplied X-Request-ID or makes one, and echoes it back.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if !validRequestID(id) {
			id = newRequestID()
		}
		c.Set(ctxReqID, id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func validRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
		if !ok {
			return false
		}
	}
	return true
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func accessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("request",
			"method", c.Request.Method,
			"route", c.FullPath(),
			"status", c.Writer.Status(),
			"ms", time.Since(start).Milliseconds(),
			"request_id", reqID(c))
	}
}

func recoverJSON(log *slog.Logger) gin.RecoveryFunc {
	return func(c *gin.Context, err any) {
		log.Error("panic recovered", "panic", err, "request_id", reqID(c))
		fail(c, http.StatusInternalServerError, "internal_error", "unexpected server error", "")
	}
}

// cors allows only the configured origins (your Vite dev server and deployed frontend).
func cors(allowed []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && slices.Contains(allowed, origin) {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, Authorization, X-Workspace-ID")
			h.Set("Access-Control-Expose-Headers", "X-Request-ID, X-Cache, Content-Disposition, Retry-After")
			h.Set("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// timeout gives every request a deadline that flows into Postgres and rules-engine calls.
func timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
