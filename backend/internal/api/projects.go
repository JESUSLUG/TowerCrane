package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/gin-gonic/gin"
)

// ProjectHandler: projects are where TowerCrane works (groups, Applications,
// Jobs, rollouts, cloud accounts); connected repositories are only the
// inventory they pick from. A project's ID is its configuration
// repository's ID, so every /repositories/:repo_id endpoint also serves it.
type ProjectHandler struct {
	projects     *db.ProjectStore
	repoStore    *db.RepositoryStore
	integrations *db.IntegrationStore
}

func NewProjectHandler(projects *db.ProjectStore, repoStore *db.RepositoryStore, integrations *db.IntegrationStore) *ProjectHandler {
	return &ProjectHandler{projects: projects, repoStore: repoStore, integrations: integrations}
}

type projectResponse struct {
	*db.Project
	CloudAccountIDs []string `json:"cloud_account_ids"`
}

func (h *ProjectHandler) respond(c *gin.Context, status int, p *db.Project) {
	ids := []string{}
	if accts, err := h.integrations.RepositoryCloudAccounts(p.ID); err == nil {
		for _, a := range accts {
			ids = append(ids, a.ID)
		}
	}
	c.JSON(status, projectResponse{Project: p, CloudAccountIDs: ids})
}

func (h *ProjectHandler) List(c *gin.Context) {
	list, err := h.projects.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list projects"})
		return
	}
	out := make([]projectResponse, 0, len(list))
	for _, p := range list {
		ids := []string{}
		if accts, err := h.integrations.RepositoryCloudAccounts(p.ID); err == nil {
			for _, a := range accts {
				ids = append(ids, a.ID)
			}
		}
		out = append(out, projectResponse{Project: p, CloudAccountIDs: ids})
	}
	c.JSON(http.StatusOK, gin.H{"projects": out})
}

func (h *ProjectHandler) Get(c *gin.Context) {
	p, err := h.projects.Get(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	h.respond(c, http.StatusOK, p)
}

type projectRequest struct {
	Name                string   `json:"name"`
	ConfigRepositoryID  string   `json:"config_repository_id"`
	SourceRepositoryIDs []string `json:"source_repository_ids"`
	CloudAccountIDs     []string `json:"cloud_account_ids"`
	// ManifestsDir: folder with the manifests; "" or "/" = the whole
	// repository. Default ".towercrane".
	ManifestsDir *string `json:"manifests_dir"`
}

// layoutOf reads a requested manifests folder (nil: the default).
func layoutOf(dir *string) (manifest.Layout, error) {
	if dir == nil {
		return manifest.DefaultLayout(), nil
	}
	return manifest.NewLayout(*dir)
}

// validRepos checks that every id is a connected repository.
func (h *ProjectHandler) validRepos(ids []string) bool {
	for _, id := range ids {
		if _, err := h.repoStore.GetRepositoryByID(id); err != nil {
			return false
		}
	}
	return true
}

func (h *ProjectHandler) validAccounts(ids []string) bool {
	for _, id := range ids {
		if _, err := h.integrations.GetCloudAccount(id); err != nil {
			return false
		}
	}
	return true
}

// Create makes a project from a connected configuration repository.
func (h *ProjectHandler) Create(c *gin.Context) {
	var req projectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.repoStore.GetRepositoryByID(req.ConfigRepositoryID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose a connected repository for the project's manifests"})
		return
	}
	if !h.validRepos(req.SourceRepositoryIDs) || !h.validAccounts(req.CloudAccountIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown repository or cloud account"})
		return
	}
	if exists, _ := h.projects.IsProject(cfg.ID); exists {
		c.JSON(http.StatusConflict, gin.H{"error": cfg.FullName + " is already the configuration of a project"})
		return
	}
	layout, err := layoutOf(req.ManifestsDir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = cfg.Name
	}
	if err := h.projects.Create(cfg.ID, name, layout); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create the project"})
		return
	}
	if err := h.projects.SetSources(cfg.ID, req.SourceRepositoryIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the source repositories"})
		return
	}
	if err := h.integrations.SetRepositoryCloudAccounts(cfg.ID, req.CloudAccountIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the cloud accounts"})
		return
	}
	p, err := h.projects.Get(cfg.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load the project"})
		return
	}
	h.respond(c, http.StatusCreated, p)
}

// Update renames a project and/or replaces its source repositories or cloud
// accounts (fields left out are kept).
func (h *ProjectHandler) Update(c *gin.Context) {
	id := c.Param("project_id")
	var req struct {
		Name                *string   `json:"name"`
		SourceRepositoryIDs *[]string `json:"source_repository_ids"`
		CloudAccountIDs     *[]string `json:"cloud_account_ids"`
		ManifestsDir        *string   `json:"manifests_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	current, err := h.projects.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	// A project defined by a Project manifest changes through a pull request;
	// here only its cloud accounts are authorized.
	if current.ManifestFile != "" && (req.Name != nil || req.SourceRepositoryIDs != nil || req.ManifestsDir != nil) {
		c.JSON(http.StatusConflict, gin.H{"error": "this project is defined by " + current.ManifestFile +
			": change its name, code repositories or folder there (only cloud accounts are authorized here)"})
		return
	}
	if req.Name != nil {
		if name := strings.TrimSpace(*req.Name); name != "" {
			if err := h.projects.Rename(id, name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to rename the project"})
				return
			}
		}
	}
	if req.ManifestsDir != nil {
		layout, err := manifest.NewLayout(*req.ManifestsDir)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := h.projects.SetLayout(id, layout); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the manifests folder"})
			return
		}
	}
	if req.SourceRepositoryIDs != nil {
		if !h.validRepos(*req.SourceRepositoryIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown repository"})
			return
		}
		if err := h.projects.SetSources(id, *req.SourceRepositoryIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the source repositories"})
			return
		}
	}
	if req.CloudAccountIDs != nil {
		if !h.validAccounts(*req.CloudAccountIDs) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown cloud account"})
			return
		}
		if err := h.integrations.SetRepositoryCloudAccounts(id, *req.CloudAccountIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save the cloud accounts"})
			return
		}
	}
	p, err := h.projects.Get(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load the project"})
		return
	}
	h.respond(c, http.StatusOK, p)
}

// Delete removes a project; its repositories stay connected. A project
// defined by a Project manifest is removed by deleting that manifest.
func (h *ProjectHandler) Delete(c *gin.Context) {
	if p, err := h.projects.Get(c.Param("project_id")); err == nil && p.ManifestFile != "" {
		c.JSON(http.StatusConflict, gin.H{"error": "this project is defined by " + p.ManifestFile + ": remove that file to remove the project"})
		return
	}
	if err := h.projects.Delete(c.Param("project_id")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete the project"})
		return
	}
	c.Status(http.StatusNoContent)
}
