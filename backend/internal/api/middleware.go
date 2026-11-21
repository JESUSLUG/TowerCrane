package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
)

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header"})
			c.Abort()
			return
		}

		claims, err := auth.VerifyJWT(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		c.Set("user_id", claims.Subject)
		c.Set("github_login", claims.GitHubLogin)
		c.Set("github_id", claims.GitHubID)
		c.Next()
	}
}

// RoleMiddleware resolves the caller's effective role from the database on
// every request (never from the JWT), so promoting or demoting a user takes
// effect immediately. Must run after AuthMiddleware.
func RoleMiddleware(userStore *db.UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		sources, err := userStore.GetRoleSources(c.GetString("user_id"))
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			c.Abort()
			return
		}
		c.Set("role", string(sources.Effective()))
		c.Next()
	}
}

// RequireRole rejects callers whose effective role is below min.
func RequireRole(min auth.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !auth.Role(c.GetString("role")).AtLeast(min) {
			c.JSON(http.StatusForbidden, gin.H{"error": "this action requires the " + string(min) + " role"})
			c.Abort()
			return
		}
		c.Next()
	}
}
