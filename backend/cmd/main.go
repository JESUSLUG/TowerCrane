package main

import (
	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // Job time zones work in minimal containers too

	"github.com/JESUSLUG/TowerCrane/backend/internal/api"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/githubapp"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	godotenv.Load()

	// Timestamps are stored in TIMESTAMP (without time zone) columns, which
	// the Postgres driver reads back as UTC: the server must write UTC too,
	// or every time read from the database is off by the machine's offset.
	// Browsers show times in the viewer's own zone.
	time.Local = time.UTC

	// A guessable JWT_SECRET lets anyone sign in as any user, admins included.
	if err := auth.CheckJWTSecret(); err != nil {
		log.Fatalf("Refusing to start: %v", err)
	}

	// Connect to database
	database, err := db.Connect()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	// Create tables
	userStore := db.NewUserStore(database)
	if err := userStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create user tables: %v", err)
	}
	// GitHub tokens of older versions were stored in plaintext.
	if n, err := userStore.EncryptGitHubTokens(); err != nil {
		log.Fatalf("Failed to encrypt stored GitHub tokens: %v", err)
	} else if n > 0 {
		log.Printf("Encrypted %d GitHub token(s) stored in plaintext by an older version", n)
	}

	repoStore := db.NewRepositoryStore(database)
	if err := repoStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create repository tables: %v", err)
	}
	// Projects (configuration repository + code repositories); the first
	// time, every connected repository becomes a project.
	if err := db.NewProjectStore(database, repoStore).CreateTables(); err != nil {
		log.Fatalf("Failed to create project tables: %v", err)
	}

	operationStore := db.NewOperationStore(database)
	if err := operationStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create operation tables: %v", err)
	}

	integrationStore := db.NewIntegrationStore(database)
	if err := integrationStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create integration tables: %v", err)
	}

	rolloutStore := db.NewRolloutStore(database)
	if err := rolloutStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create rollout tables: %v", err)
	}

	jobStore := db.NewJobStore(database)
	if err := jobStore.CreateTables(); err != nil {
		log.Fatalf("Failed to create job tables: %v", err)
	}

	// Nothing survives a restart mid-operation: close what was left running.
	if n, err := operationStore.FailInterrupted(); err != nil {
		log.Printf("Failed to close interrupted operations: %v", err)
	} else if n > 0 {
		log.Printf("Marked %d operation(s) interrupted by the last restart as failed", n)
	}
	if err := rolloutStore.FailInterrupted(); err != nil {
		log.Printf("Failed to close interrupted rollout items: %v", err)
	}
	if _, err := jobStore.FailInterrupted(); err != nil {
		log.Printf("Failed to close interrupted job runs: %v", err)
	}
	// TowerCrane no longer stores application variables/secrets (code reads
	// secrets from the cloud): drop what older versions stored.
	if _, err := database.Exec(`DROP TABLE IF EXISTS variables`); err != nil {
		log.Printf("Failed to drop the old variables table: %v", err)
	}

	app, err := githubapp.FromEnv()
	if err != nil {
		log.Fatalf("Invalid GitHub App configuration: %v", err)
	}
	if app == nil {
		log.Printf("GitHub App not configured (GITHUB_APP_ID): repositories can only be connected with personal OAuth tokens")
	}
	if os.Getenv("GITHUB_WEBHOOK_SECRET") == "" {
		log.Printf("GITHUB_WEBHOOK_SECRET is not set: webhooks are disabled, TowerCrane relies on polling GitHub")
	}

	api.InitSetup(userStore)

	// Initialize Gin router
	router := gin.Default()
	// TowerCrane doesn't sit behind a proxy it should trust X-Forwarded-For
	// from; set TOWERCRANE_TRUSTED_PROXIES (comma-separated) if it does.
	var proxies []string
	for _, p := range strings.Split(os.Getenv("TOWERCRANE_TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	if err := router.SetTrustedProxies(proxies); err != nil {
		log.Fatalf("Invalid TOWERCRANE_TRUSTED_PROXIES: %v", err)
	}

	router.Use(securityHeaders())
	// Enable CORS
	router.Use(corsMiddleware())

	// Setup API routes
	api.SetupRoutes(router, database, app)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 TowerCrane backend running on port %s", port)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}

// corsMiddleware lets only the TowerCrane frontend call the API from a
// browser: FRONTEND_URL, plus TOWERCRANE_CORS_ORIGINS (comma-separated) for
// extra origins. The session travels in the Authorization header (no
// cookies), so credentials are never allowed cross-origin.
// securityHeaders: the API only serves JSON; tell browsers not to sniff it,
// frame it or cache it.
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

func corsMiddleware() gin.HandlerFunc {
	allowed := map[string]bool{}
	for _, o := range append([]string{os.Getenv("FRONTEND_URL")}, strings.Split(os.Getenv("TOWERCRANE_CORS_ORIGINS"), ",")...) {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[o] = true
		}
	}
	if len(allowed) == 0 {
		allowed["http://localhost:5173"] = true // local development
	}
	// localhost and 127.0.0.1 are the same machine but different browser
	// origins: accept both spellings of a loopback origin.
	for o := range allowed {
		switch {
		case strings.Contains(o, "://localhost"):
			allowed[strings.Replace(o, "://localhost", "://127.0.0.1", 1)] = true
		case strings.Contains(o, "://127.0.0.1"):
			allowed[strings.Replace(o, "://127.0.0.1", "://localhost", 1)] = true
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowed[origin] {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept, Cache-Control, X-Requested-With")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Add("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
