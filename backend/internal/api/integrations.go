package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/cloud"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/secrets"
	"github.com/gin-gonic/gin"
)

// IntegrationsHandler manages GitHub App installations and cloud accounts.
type IntegrationsHandler struct {
	integrations *db.IntegrationStore
	repoStore    *db.RepositoryStore
	gh           *GitHubAccess
}

func NewIntegrationsHandler(integrations *db.IntegrationStore, repoStore *db.RepositoryStore, gh *GitHubAccess) *IntegrationsHandler {
	return &IntegrationsHandler{integrations: integrations, repoStore: repoStore, gh: gh}
}

// --- GitHub -------------------------------------------------------------------

// GetGitHub reports whether the GitHub App is configured, where to install
// it, and which installations TowerCrane knows about.
func (h *IntegrationsHandler) GetGitHub(c *gin.Context) {
	installations, err := h.integrations.ListInstallations()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list installations"})
		return
	}
	resp := gin.H{
		"app_configured":        h.gh.app != nil,
		"installations":         installations,
		"webhook_secret":        webhookSecretConfigured(),
		"poll_interval_seconds": int(PollInterval().Seconds()),
	}
	if h.gh.app != nil {
		resp["app_slug"] = h.gh.app.Slug
		resp["install_url"] = h.gh.app.InstallURL("towercrane")

		// Installations that exist on GitHub but TowerCrane hasn't
		// registered yet (e.g. the Setup URL redirect didn't complete).
		// An admin still has to confirm each one explicitly.
		known := map[int64]bool{}
		for _, i := range installations {
			known[i.InstallationID] = true
		}
		pending := []gin.H{}
		if all, err := h.gh.app.ListInstallations(c.Request.Context()); err != nil {
			log.Printf("failed to list GitHub App installations: %v", err)
			resp["app_error"] = "TowerCrane could not authenticate as the GitHub App: check GITHUB_APP_ID and the private key"
		} else {
			for _, inst := range all {
				if known[inst.GetID()] {
					continue
				}
				pending = append(pending, gin.H{
					"installation_id":      inst.GetID(),
					"account_login":        inst.GetAccount().GetLogin(),
					"account_type":         inst.GetAccount().GetType(),
					"repository_selection": inst.GetRepositorySelection(),
				})
			}
		}
		resp["pending_installations"] = pending
	}
	c.JSON(http.StatusOK, resp)
}

type addInstallationRequest struct {
	InstallationID int64 `json:"installation_id" binding:"required"`
}

// AddInstallation registers an installation after an admin installed the app
// on GitHub (GitHub redirects back to the app's Setup URL with its id).
func (h *IntegrationsHandler) AddInstallation(c *gin.Context) {
	if h.gh.app == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the GitHub App is not configured on the server (GITHUB_APP_ID)"})
		return
	}
	var req addInstallationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inst, err := h.gh.app.GetInstallation(context.Background(), req.InstallationID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	record := &db.Installation{
		InstallationID:      inst.GetID(),
		AccountLogin:        inst.GetAccount().GetLogin(),
		AccountType:         inst.GetAccount().GetType(),
		RepositorySelection: inst.GetRepositorySelection(),
		AddedBy:             c.GetString("user_id"),
	}
	if err := h.integrations.UpsertInstallation(record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save installation"})
		return
	}
	c.JSON(http.StatusOK, record)
}

// RemoveInstallation forgets an installation in TowerCrane (uninstalling the
// app itself is done on GitHub). Its repositories stay connected but can't
// be read until they are linked to another installation.
func (h *IntegrationsHandler) RemoveInstallation(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("installation_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid installation id"})
		return
	}
	if err := h.integrations.DeleteInstallation(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete installation"})
		return
	}
	if err := h.repoStore.ClearInstallation(id); err != nil {
		log.Printf("failed to detach repositories from installation %d: %v", id, err)
	}
	if h.gh.app != nil {
		h.gh.app.Forget(id)
	}
	c.Status(http.StatusNoContent)
}

// --- Cloud accounts -----------------------------------------------------------

type cloudAccountDTO struct {
	*db.CloudAccount
	AccessKeyID string `json:"access_key_id"` // masked
}

func toCloudAccountDTO(a *db.CloudAccount) cloudAccountDTO {
	return cloudAccountDTO{CloudAccount: a, AccessKeyID: cloud.MaskKey(a.AccessKeyID)}
}

func (h *IntegrationsHandler) ListCloudAccounts(c *gin.Context) {
	accounts, err := h.integrations.ListCloudAccounts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list cloud accounts"})
		return
	}
	out := make([]cloudAccountDTO, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, toCloudAccountDTO(a))
	}
	c.JSON(http.StatusOK, gin.H{"accounts": out})
}

type createCloudAccountRequest struct {
	Name            string `json:"name" binding:"required"`
	Provider        string `json:"provider"`
	AWSAccountID    string `json:"aws_account_id" binding:"required"`
	AccessKeyID     string `json:"access_key_id" binding:"required"`
	SecretAccessKey string `json:"secret_access_key" binding:"required"`
	Region          string `json:"region" binding:"required"`
	EndpointURL     string `json:"endpoint_url"`
}

// CreateCloudAccount stores AWS access-key credentials (the secret is
// encrypted at rest and never returned) and verifies them right away.
func (h *IntegrationsHandler) CreateCloudAccount(c *gin.Context) {
	var req createCloudAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Provider != "" && req.Provider != "aws" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only the aws provider is supported for now"})
		return
	}

	creds := cloud.AWSCredentials{
		AccountID:       strings.TrimSpace(req.AWSAccountID),
		AccessKeyID:     strings.TrimSpace(req.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(req.SecretAccessKey),
		Region:          strings.TrimSpace(req.Region),
		EndpointURL:     strings.TrimRight(strings.TrimSpace(req.EndpointURL), "/"),
	}
	if err := creds.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enc, err := secrets.Encrypt(creds.SecretAccessKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	acct := &db.CloudAccount{
		Name: strings.TrimSpace(req.Name), Provider: "aws", AWSAccountID: creds.AccountID,
		AccessKeyID: creds.AccessKeyID, SecretEnc: enc, Region: creds.Region, EndpointURL: creds.EndpointURL,
		CreatedBy: c.GetString("user_id"),
	}
	if err := h.integrations.CreateCloudAccount(acct); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "a cloud account with that name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save cloud account"})
		return
	}

	h.verify(acct, creds)
	saved, err := h.integrations.GetCloudAccount(acct.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load cloud account"})
		return
	}
	c.JSON(http.StatusCreated, toCloudAccountDTO(saved))
}

// VerifyCloudAccount re-runs the STS identity check.
func (h *IntegrationsHandler) VerifyCloudAccount(c *gin.Context) {
	acct, err := h.integrations.GetCloudAccount(c.Param("account_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cloud account not found"})
		return
	}
	secret, err := secrets.Decrypt(acct.SecretEnc)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.verify(acct, cloud.AWSCredentials{
		AccountID: acct.AWSAccountID, AccessKeyID: acct.AccessKeyID, SecretAccessKey: secret,
		Region: acct.Region, EndpointURL: acct.EndpointURL,
	})
	saved, err := h.integrations.GetCloudAccount(acct.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load cloud account"})
		return
	}
	c.JSON(http.StatusOK, toCloudAccountDTO(saved))
}

func (h *IntegrationsHandler) verify(acct *db.CloudAccount, creds cloud.AWSCredentials) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	msg := ""
	if _, err := creds.Verify(ctx); err != nil {
		msg = err.Error()
	}
	if err := h.integrations.SetCloudAccountVerification(acct.ID, msg); err != nil {
		log.Printf("failed to store verification for cloud account %s: %v", acct.ID, err)
	}
}

func (h *IntegrationsHandler) DeleteCloudAccount(c *gin.Context) {
	if err := h.integrations.DeleteCloudAccount(c.Param("account_id")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "cloud account not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete cloud account"})
		return
	}
	c.Status(http.StatusNoContent)
}

type setRepoCloudAccountsRequest struct {
	CloudAccountIDs []string `json:"cloud_account_ids"` // empty detaches all
}

// SetRepositoryCloudAccounts picks the cloud accounts a repository may use.
// Its manifests name them (Group spec.cloudAccount, or
// spec.destination.cloudAccount); any other account is refused at run time.
func (h *IntegrationsHandler) SetRepositoryCloudAccounts(c *gin.Context) {
	var req setRepoCloudAccountsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	repoID := c.Param("repo_id")
	if _, err := h.repoStore.GetRepositoryByID(repoID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range req.CloudAccountIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := h.integrations.GetCloudAccount(id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cloud account not found: " + id})
			return
		}
		ids = append(ids, id)
	}
	if err := h.integrations.SetRepositoryCloudAccounts(repoID, ids); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update repository"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"cloud_account_ids": ids})
}
