package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/git"
)

type AuthHandler struct {
	oauth        *auth.GitHubOAuth
	userStore    *db.UserStore
	integrations *db.IntegrationStore
	gh           *GitHubAccess
}

func NewAuthHandler(userStore *db.UserStore, integrations *db.IntegrationStore, gh *GitHubAccess) *AuthHandler {
	return &AuthHandler{
		oauth:        auth.NewGitHubOAuth(),
		userStore:    userStore,
		integrations: integrations,
		gh:           gh,
	}
}

type LoginLocalRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginResponse struct {
	LoginURL string `json:"login_url"`
}

type CallbackRequest struct {
	Code  string `json:"code" binding:"required"`
	State string `json:"state" binding:"required"`
}

type AuthResponse struct {
	Token     string            `json:"token"`
	User      UserResponseDTO   `json:"user"`
	ExpiresAt time.Time         `json:"expires_at"`
}

type UserResponseDTO struct {
	ID          string `json:"id"`
	GitHubID    int64  `json:"github_id"`
	GitHubLogin string `json:"github_login"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	AvatarURL   string `json:"avatar_url"`
}

func (h *AuthHandler) LoginWithGitHub(c *gin.Context) {
	state := c.Query("state")
	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state parameter required"})
		return
	}

	loginURL := h.oauth.GetAuthURL(state)
	c.JSON(http.StatusOK, LoginResponse{LoginURL: loginURL})
}

func (h *AuthHandler) GitHubCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	frontendURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	deny := func(reason string) {
		c.Redirect(http.StatusTemporaryRedirect, frontendURL+"/?error="+url.QueryEscape(reason))
	}

	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code"})
		return
	}

	ctx := context.Background()
	user, githubToken, err := h.oauth.ExchangeCode(ctx, code)
	if err != nil {
		log.Printf("Failed to exchange code: %v", err)
		deny("GitHub authentication failed")
		return
	}
	login := user.GitHubLogin.String

	// Invitation links pass their token through the OAuth state.
	var invitation *db.Invitation
	if token, ok := strings.CutPrefix(state, "invite:"); ok {
		inv, err := h.integrations.GetOpenInvitation(token)
		if err != nil {
			deny("This invitation is invalid, expired or was already used")
			return
		}
		invitation = inv
	}

	// Decide before creating anything: a user who may not sign in never
	// gets an account.
	allowed := invitation != nil || auth.IsBootstrapAdmin(login, user.Email)
	if !allowed {
		if existing, err := h.userStore.GetUserByGitHubID(user.GitHubID.Int64); err == nil {
			allowed, _ = h.userStore.IsInvited(existing.ID)
		}
	}
	if !allowed {
		allowed = h.belongsToConnectedAccount(ctx, login, githubToken)
	}
	if !allowed {
		log.Printf("Sign-in denied for GitHub user %s: not in a connected org and not invited", login)
		deny("Your GitHub account is not a member of an organization connected to TowerCrane. Ask an admin for an invitation.")
		return
	}

	dbUser, err := h.userStore.GetOrCreateUser(user)
	if err != nil {
		log.Printf("Failed to create/get user: %v", err)
		deny("Failed to create your account")
		return
	}

	if invitation != nil {
		if err := h.consumeInvitation(invitation, dbUser.ID); err != nil {
			log.Printf("Failed to accept invitation for %s: %v", login, err)
			deny("This invitation was already used")
			return
		}
	}

	// Persist the GitHub access token so later requests can call the GitHub
	// API on this user's behalf (legacy, non-app repositories).
	if err := h.userStore.SetGitHubToken(dbUser.ID, githubToken); err != nil {
		log.Printf("Failed to store GitHub token: %v", err)
	}

	h.syncGitHubTeams(ctx, dbUser.ID, githubToken)

	token, _, err := auth.GenerateJWT(dbUser)
	if err != nil {
		log.Printf("Failed to generate JWT: %v", err)
		deny("Failed to sign you in")
		return
	}

	redirectURL := fmt.Sprintf("%s/auth/callback?token=%s", frontendURL, url.QueryEscape(token))
	c.Redirect(http.StatusTemporaryRedirect, redirectURL)
}

func (h *AuthHandler) consumeInvitation(inv *db.Invitation, userID string) error {
	tx, err := h.userStore.DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := h.integrations.AcceptInvitation(tx, inv.ID, userID); err != nil {
		return err
	}
	if err := h.userStore.MarkInvited(tx, userID, auth.Role(inv.Role)); err != nil {
		return err
	}
	return tx.Commit()
}

// belongsToConnectedAccount reports whether a GitHub user is a member of an
// organization where the TowerCrane GitHub App is installed (or of the
// legacy GITHUB_ORG), or owns a personal account the app is installed on.
func (h *AuthHandler) belongsToConnectedAccount(ctx context.Context, login, userToken string) bool {
	installations, err := h.integrations.ListInstallations()
	if err != nil {
		log.Printf("Failed to list installations: %v", err)
		return false
	}

	type orgCheck struct {
		org            string
		installationID int64
	}
	var orgs []orgCheck
	for _, inst := range installations {
		switch inst.AccountType {
		case "User":
			if strings.EqualFold(inst.AccountLogin, login) {
				return true
			}
		case "Organization":
			orgs = append(orgs, orgCheck{inst.AccountLogin, inst.InstallationID})
		}
	}
	if legacy := strings.TrimSpace(os.Getenv("GITHUB_ORG")); legacy != "" {
		orgs = append(orgs, orgCheck{org: legacy})
	}

	for _, o := range orgs {
		var isMember bool
		var err error
		if o.installationID != 0 && h.gh.app != nil {
			// The app's view of the org (needs the Members: read permission)
			// works even when the org restricts OAuth app access.
			client, cerr := h.gh.app.Client(ctx, o.installationID)
			if cerr != nil {
				log.Printf("Failed to get installation client for %s: %v", o.org, cerr)
				continue
			}
			isMember, _, err = client.Organizations.IsMember(ctx, o.org, login)
		} else {
			isMember, _, err = git.NewGitHubClientFromToken(ctx, userToken).Organizations.IsMember(ctx, o.org, login)
		}
		if err != nil {
			log.Printf("Failed to check %s membership in %s: %v", login, o.org, err)
			continue
		}
		if isMember {
			return true
		}
	}
	return false
}

func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	user, err := h.userStore.GetUserByID(userID.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	sources, err := h.userStore.GetRoleSources(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load user role"})
		return
	}

	c.JSON(http.StatusOK, CurrentUserDTO{
		UserResponseDTO: userToDTO(user),
		Role:            sources.Effective(),
		RoleSources:     sources,
	})
}

// CurrentUserDTO is the signed-in user plus their effective role.
type CurrentUserDTO struct {
	UserResponseDTO
	Role        auth.Role        `json:"role"`
	RoleSources auth.RoleSources `json:"role_sources"`
}

// syncGitHubTeams stores the GitHub teams the user belongs to; their
// GitHub-derived role is computed from these and the current team mappings.
// It fails closed: if the teams can't be read, the stored teams are cleared
// rather than left stale.
func (h *AuthHandler) syncGitHubTeams(ctx context.Context, userID, githubToken string) {
	teams, err := auth.UserTeams(ctx, githubToken)
	if err != nil {
		log.Printf("Failed to read GitHub teams for %s, clearing GitHub-derived role: %v", userID, err)
		teams = nil
	}
	if err := h.userStore.SetGitHubTeams(userID, teams); err != nil {
		log.Printf("Failed to store GitHub teams for %s: %v", userID, err)
	}
}

func (h *AuthHandler) LoginLocal(c *gin.Context) {
	var req LoginLocalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user by email
	user, err := h.userStore.GetUserByEmail(req.Email)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	// Get password hash
	passwordHash, err := h.userStore.GetUserPasswordHash(user.ID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	// Verify password
	if !auth.VerifyPassword(passwordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	// Generate JWT
	token, expiresAt, err := auth.GenerateJWT(user)
	if err != nil {
		log.Printf("Failed to generate JWT: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, AuthResponse{
		Token:     token,
		User:      userToDTO(user),
		ExpiresAt: expiresAt,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

func userToDTO(user *auth.User) UserResponseDTO {
	githubID := int64(0)
	if user.GitHubID.Valid {
		githubID = user.GitHubID.Int64
	}
	
	githubLogin := ""
	if user.GitHubLogin.Valid {
		githubLogin = user.GitHubLogin.String
	}
	
	avatarURL := ""
	if user.AvatarURL.Valid {
		avatarURL = user.AvatarURL.String
	}
	
	return UserResponseDTO{
		ID:          user.ID,
		GitHubID:    githubID,
		GitHubLogin: githubLogin,
		Email:       user.Email,
		Name:        user.Name,
		AvatarURL:   avatarURL,
	}
}
