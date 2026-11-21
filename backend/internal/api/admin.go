package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/JESUSLUG/TowerCrane/backend/internal/auth"
	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/gin-gonic/gin"
)

// AdminHandler manages users' roles and the GitHub team -> role mappings.
// Every route is admin-only.
type AdminHandler struct {
	userStore *db.UserStore
}

func NewAdminHandler(userStore *db.UserStore) *AdminHandler {
	return &AdminHandler{userStore: userStore}
}

type adminUserDTO struct {
	UserResponseDTO
	Role        auth.Role        `json:"role"`
	RoleSources auth.RoleSources `json:"role_sources"`
}

func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.userStore.ListUsersWithRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	out := make([]adminUserDTO, 0, len(users))
	for _, u := range users {
		out = append(out, adminUserDTO{UserResponseDTO: userToDTO(u.User), Role: u.Roles.Effective(), RoleSources: u.Roles})
	}
	c.JSON(http.StatusOK, gin.H{"users": out})
}

type setRoleRequest struct {
	// Role assigned inside TowerCrane; "" clears it (the user then keeps
	// whatever their GitHub teams grant, or member).
	Role string `json:"role"`
}

// SetUserRole changes the role assigned to a user inside TowerCrane. The last
// admin can't be demoted, so TowerCrane can never end up without one.
func (h *AdminHandler) SetUserRole(c *gin.Context) {
	var req setRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	role := auth.RoleNone
	if req.Role != "" {
		r, ok := auth.ParseRole(req.Role)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be member, approver or admin"})
			return
		}
		role = r
	}

	targetID := c.Param("user_id")
	users, err := h.userStore.ListUsersWithRoles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load users"})
		return
	}
	admins := 0
	var target *db.UserWithRoles
	for i := range users {
		if users[i].Roles.Effective() == auth.RoleAdmin {
			admins++
		}
		if users[i].User.ID == targetID {
			target = &users[i]
		}
	}
	if target == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	after := target.Roles
	after.Local = role
	if target.Roles.Effective() == auth.RoleAdmin && after.Effective() != auth.RoleAdmin && admins <= 1 {
		c.JSON(http.StatusConflict, gin.H{"error": "can't demote the last admin; promote someone else first"})
		return
	}

	if err := h.userStore.SetLocalRole(targetID, role); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update role"})
		return
	}
	c.JSON(http.StatusOK, adminUserDTO{UserResponseDTO: userToDTO(target.User), Role: after.Effective(), RoleSources: after})
}

func (h *AdminHandler) ListRoleMappings(c *gin.Context) {
	mappings, err := h.userStore.ListRoleMappings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list role mappings"})
		return
	}
	if mappings == nil {
		mappings = []db.RoleMapping{}
	}
	c.JSON(http.StatusOK, gin.H{"mappings": mappings})
}

type roleMappingRequest struct {
	// Team is "org/team-slug", e.g. "acme/infra-admins".
	Team string `json:"team" binding:"required"`
	Role string `json:"role" binding:"required"`
}

func (h *AdminHandler) UpsertRoleMapping(c *gin.Context) {
	var req roleMappingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	org, team, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(req.Team), "@"), "/")
	if !ok || org == "" || team == "" || strings.Contains(team, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": `team must look like "org/team-slug"`})
		return
	}
	role, ok := auth.ParseRole(req.Role)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be member, approver or admin"})
		return
	}

	m, err := h.userStore.UpsertRoleMapping(org, team, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save role mapping"})
		return
	}
	c.JSON(http.StatusOK, m)
}

func (h *AdminHandler) DeleteRoleMapping(c *gin.Context) {
	if err := h.userStore.DeleteRoleMapping(c.Param("mapping_id")); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "role mapping not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete role mapping"})
		return
	}
	c.Status(http.StatusNoContent)
}
