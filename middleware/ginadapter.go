package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Wrap adapts a standard net/http middleware (func(http.Handler) http.Handler)
// into a gin.HandlerFunc, so the existing CDPG middleware collection
// (RequestID, Logger, CORS, Compression, Recovery, Timeout, and chi's
// dependency-free Recoverer/RealIP) can be reused unchanged under gin.
func Wrap(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Request = r
			c.Next()
		})
		mw(next).ServeHTTP(c.Writer, c.Request)
	}
}
