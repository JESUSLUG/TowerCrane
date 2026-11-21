package api

import (
	"net/http"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/gin-gonic/gin"
)

// RolloutHandler exposes rollouts: everyone can see them, admins can
// reorder, pause/resume and retry Applications.
type RolloutHandler struct {
	store  *db.RolloutStore
	engine *RolloutEngine
}

func NewRolloutHandler(store *db.RolloutStore, engine *RolloutEngine) *RolloutHandler {
	return &RolloutHandler{store: store, engine: engine}
}

func (h *RolloutHandler) List(c *gin.Context) {
	rollouts, err := h.store.ListByRepository(c.Param("repo_id"), 10)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list rollouts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rollouts": rollouts})
}

type reorderRequest struct {
	// Order lists every Application of the rollout; each must come after
	// the Applications in its dependsOn.
	Order []string `json:"order" binding:"required"`
}

func (h *RolloutHandler) Reorder(c *gin.Context) {
	var req reorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.engine.Reorder(c.Param("rollout_id"), req.Order); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respond(c)
}

func (h *RolloutHandler) Pause(c *gin.Context)  { h.setPaused(c, true) }
func (h *RolloutHandler) Resume(c *gin.Context) { h.setPaused(c, false) }

func (h *RolloutHandler) setPaused(c *gin.Context, paused bool) {
	if err := h.engine.SetPaused(c.Param("rollout_id"), c.Param("application"), paused); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respond(c)
}

func (h *RolloutHandler) Retry(c *gin.Context) {
	if err := h.engine.Retry(c.Param("rollout_id"), c.Param("application")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.respond(c)
}

func (h *RolloutHandler) respond(c *gin.Context) {
	r, err := h.store.Get(c.Param("rollout_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "rollout not found"})
		return
	}
	c.JSON(http.StatusOK, r)
}
