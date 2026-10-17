package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"c23desk/internal/service"
)

var (
	reqIDRE = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	actorRE = regexp.MustCompile(`^[A-Za-z0-9 _.@-]{1,64}$`)
)

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// requestContext sets request id + actor and writes a structured access log (no bodies, no secrets).
func requestContext(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		id := c.GetHeader("X-Request-ID")
		if !reqIDRE.MatchString(id) {
			id = newID()
		}
		actor := c.GetHeader("X-Actor")
		if !actorRE.MatchString(actor) {
			actor = "anonymous"
		}
		c.Set("request_id", id)
		c.Set("actor", actor)
		c.Header("X-Request-ID", id)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "no-store")
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "(unmatched)"
		}
		lvl := slog.LevelInfo
		if c.Writer.Status() >= 500 {
			lvl = slog.LevelError
		} else if c.Writer.Status() >= 400 {
			lvl = slog.LevelWarn
		}
		log.Log(c.Request.Context(), lvl, "request", "method", c.Request.Method, "route", route, "status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(), "request_id", id, "actor", actor)
	}
}

// bodyLimit caps request bodies; oversize bodies fail with 413 when read.
func bodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > n {
			abort(c, &service.Error{Status: http.StatusRequestEntityTooLarge, Code: "PAYLOAD_TOO_LARGE", Message: "request body too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		c.Next()
	}
}

// recovery turns panics into a generic 500 without leaking internals.
func recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic recovered", "request_id", c.GetString("request_id"), "panic", r, "stack", string(debug.Stack()))
				abort(c, &service.Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Message: "internal error"})
			}
		}()
		c.Next()
	}
}

// apiKey protects mutating methods when API_KEY is configured (auth-ready hook; constant-time compare).
func apiKey(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if key == "" || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodOptions || c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-API-Key")), []byte(key)) != 1 {
			abort(c, &service.Error{Status: http.StatusUnauthorized, Code: "UNAUTHORIZED", Message: "missing or invalid X-API-Key"})
			return
		}
		c.Next()
	}
}

// cors allows only configured origins (no wildcard).
func cors(origins []string) gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return func(c *gin.Context) {
		o := c.GetHeader("Origin")
		if o != "" && allowed[o] {
			c.Header("Access-Control-Allow-Origin", o)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-Request-ID, X-Actor, X-API-Key")
			c.Header("Access-Control-Max-Age", "600")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
