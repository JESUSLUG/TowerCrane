package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSOnlyFrontend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("FRONTEND_URL", "http://localhost:5173/")
	t.Setenv("TOWERCRANE_CORS_ORIGINS", "")
	r := gin.New()
	r.Use(corsMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	for origin, want := range map[string]string{
		"http://localhost:5173": "http://localhost:5173",
		"http://127.0.0.1:5173": "http://127.0.0.1:5173",
		"https://evil.example":  "",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/x", nil)
		req.Header.Set("Origin", origin)
		r.ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != want {
			t.Errorf("%s: Allow-Origin = %q, want %q", origin, got, want)
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("%s: credentials must never be allowed", origin)
		}
	}
}
