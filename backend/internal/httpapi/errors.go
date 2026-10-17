package httpapi

import (
	"context"
	"errors"
	"net"

	"github.com/gin-gonic/gin"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// fail writes the one error shape used by every endpoint and stops the handler chain.
func fail(c *gin.Context, status int, code, msg, field string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error":      apiError{Code: code, Message: msg, Field: field},
		"request_id": reqID(c),
	})
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
