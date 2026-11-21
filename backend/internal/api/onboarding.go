package api

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/gin-gonic/gin"
)

// Accounts can only be created three ways:
//
//  1. First run: the initial admin, protected by a one-time setup token the
//     backend prints in its logs (or TOWERCRANE_SETUP_TOKEN).
//  2. Signing in with GitHub as a member of an org where the TowerCrane
//     GitHub App is installed (see AuthHandler.GitHubCallback).
//  3. Accepting an invitation an admin created.
//
// There is no open registration.

var (
	setupMu    sync.Mutex
	setupToken string
)

// InitSetup prepares the first-run setup when no account exists yet.
func InitSetup(userStore *db.UserStore) {
	has, err := userStore.HasUsers()
	if err != nil {
		log.Printf("setup: failed to check for existing accounts: %v", err)
		return
	}
	if has {
		return
	}

	token := strings.TrimSpace(os.Getenv("TOWERCRANE_SETUP_TOKEN"))
	if token == "" {
		token = randomToken(18)
	}
	setupMu.Lock()
	setupToken = token
	setupMu.Unlock()

	log.Printf("================================================================")
	log.Printf(" TowerCrane has no accounts yet. Create the first admin at")
	log.Printf("   %s/setup", strings.TrimRight(os.Getenv("FRONTEND_URL"), "/"))
	log.Printf(" using this one-time setup token:")
	log.Printf("   %s", token)
	log.Printf("================================================================")
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

type OnboardingHandler struct {
	userStore    *db.UserStore
	integrations *db.IntegrationStore
}

func NewOnboardingHandler(userStore *db.UserStore, integrations *db.IntegrationStore) *OnboardingHandler {
	return &OnboardingHandler{userStore: userStore, integrations: integrations}
}

// SetupStatus tells the frontend whether to show the first-run screen.
func (h *OnboardingHandler) SetupStatus(c *gin.Context) {
	has, err := h.userStore.HasUsers()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check setup status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"needs_setup": !has})
}

type setupRequest struct {
	SetupToken string `json:"setup_token" binding:"required"`
	Email      string `json:"email" binding:"required,email"`
	Password   string `json:"password" binding:"required,min=12"`
	Name       string `json:"name"`
}

// Setup creates the initial admin account. It works exactly once.
func (h *OnboardingHandler) Setup(c *gin.Context) {
	var req setupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	setupMu.Lock()
	expected := setupToken
	setupMu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(req.SetupToken), []byte(expected)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid setup token"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}
	user, err := h.userStore.CreateInitialAdmin(req.Email, hash, req.Name)
	if err == db.ErrAlreadySetUp {
		c.JSON(http.StatusConflict, gin.H{"error": "TowerCrane is already set up"})
		return
	}
	if err != nil {
		log.Printf("setup: failed to create initial admin: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	setupMu.Lock()
	setupToken = "" // one-time
	setupMu.Unlock()

	respondWithSession(c, http.StatusCreated, user)
}

// --- Invitations (admin) ------------------------------------------------------

const invitationTTL = 7 * 24 * time.Hour

type createInvitationRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role" binding:"required"`
}

// CreateInvitation returns a single-use link (valid 7 days). The token is
// only shown in this response; TowerCrane stores just its hash.
func (h *OnboardingHandler) CreateInvitation(c *gin.Context) {
	var req createInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role, ok := auth.ParseRole(req.Role)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be member, approver or admin"})
		return
	}

	token := randomToken(32)
	inv, err := h.integrations.CreateInvitation(strings.TrimSpace(req.Email), string(role), token, c.GetString("user_id"), invitationTTL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invitation"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"invitation": inv,
		"invite_url": strings.TrimRight(os.Getenv("FRONTEND_URL"), "/") + "/invite/" + token,
	})
}

func (h *OnboardingHandler) ListInvitations(c *gin.Context) {
	invs, err := h.integrations.ListInvitations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list invitations"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitations": invs})
}

func (h *OnboardingHandler) RevokeInvitation(c *gin.Context) {
	if err := h.integrations.RevokeInvitation(c.Param("invitation_id")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found or already accepted"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke invitation"})
		return
	}
	c.Status(http.StatusNoContent)
}

// --- Invitations (public, token-authenticated) --------------------------------

// GetInvitation shows who an invitation is for, so the accept page can
// render it.
func (h *OnboardingHandler) GetInvitation(c *gin.Context) {
	inv, err := h.integrations.GetOpenInvitation(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "this invitation is invalid, expired or was already used"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"email": inv.Email, "role": inv.Role, "expires_at": inv.ExpiresAt})
}

type acceptInvitationRequest struct {
	Password string `json:"password" binding:"required,min=12"`
	Name     string `json:"name"`
}

// AcceptInvitation creates a local (email/password) account for the invited
// email. Invitees can instead sign in with GitHub, which passes the token
// through the OAuth state (see GitHubCallback).
func (h *OnboardingHandler) AcceptInvitation(c *gin.Context) {
	var req acceptInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inv, err := h.integrations.GetOpenInvitation(c.Param("token"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "this invitation is invalid, expired or was already used"})
		return
	}
	if _, err := h.userStore.GetUserByEmail(inv.Email); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "an account with this email already exists; sign in instead"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	tx, err := h.userStore.DB().Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}
	defer tx.Rollback()

	user, err := h.userStore.CreateInvitedLocalUser(tx, inv.Email, hash, req.Name, auth.Role(inv.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}
	if err := h.integrations.AcceptInvitation(tx, inv.ID, user.ID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "this invitation was already used"})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create account"})
		return
	}

	respondWithSession(c, http.StatusCreated, user)
}

// respondWithSession issues a JWT for user.
func respondWithSession(c *gin.Context, status int, user *auth.User) {
	token, expiresAt, err := auth.GenerateJWT(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}
	c.JSON(status, AuthResponse{Token: token, User: userToDTO(user), ExpiresAt: expiresAt})
}
