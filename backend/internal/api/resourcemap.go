package api

import (
	"context"
	"net/http"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/tfmap"
	"github.com/gin-gonic/gin"
)

type MapHandler struct {
	gh             *GitHubAccess
	repoStore      *db.RepositoryStore
	userStore      *db.UserStore
	operationStore *db.OperationStore
	integrations   *db.IntegrationStore
	snapshots      *SnapshotStore
	projects       *db.ProjectStore
}

func NewMapHandler(repoStore *db.RepositoryStore, userStore *db.UserStore, operationStore *db.OperationStore, integrations *db.IntegrationStore, gh *GitHubAccess, snapshots *SnapshotStore, projects *db.ProjectStore) *MapHandler {
	return &MapHandler{
		projects:       projects,
		snapshots:      snapshots,
		gh:             gh,
		integrations:   integrations,
		repoStore:      repoStore,
		userStore:      userStore,
		operationStore: operationStore,
	}
}

// statusForOperation maps the latest Operation recorded for an environment
// directory to a sync status badge.
func statusForOperation(op *db.Operation) string {
	if op == nil {
		return "unknown"
	}
	switch {
	case op.Status == "failed":
		return "error"
	case op.Status == "pending" || op.Status == "running":
		return "running"
	case op.Type == "apply" && op.Status == "success":
		return "synced"
	case op.Type == "plan" && op.Status == "success":
		return "out-of-sync"
	case op.Type == "destroy" && op.Status == "success":
		return "destroyed"
	default:
		return "unknown"
	}
}

// GetResourceMap builds the dependency map for a connected
// repository: every block is shown so the map always reflects how things
// connect; Application manifests decide which Application each block belongs
// to, whether it is editable and whether its source is shown.
// Query params: branch (defaults to the repository's default branch) and
// app (an Application name, to show only that Application's nodes).
func (h *MapHandler) GetResourceMap(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	repoID := c.Param("repo_id")
	repo, err := h.repoStore.GetRepositoryByID(repoID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}

	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	branch := c.Query("branch")
	if branch == "" {
		branch = defaultBranch
	}

	// The default branch comes from the commit TowerCrane last read (on its
	// poll, a webhook or a Refresh), never straight from GitHub on every
	// visit. Other branches are read on demand.
	ctx := context.Background()
	var files map[string]string
	var commitSHA string
	var readAt time.Time
	if branch == defaultBranch {
		snap := h.snapshots.Get(repo.ID)
		if snap == nil { // first view since TowerCrane started
			if snap, err = h.snapshots.readSnapshot(ctx, h.gh, repo, ""); err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				return
			}
		}
		files, commitSHA, readAt = snap.Files, snap.SHA, snap.ReadAt
	} else {
		ghClient, err := h.gh.Client(ctx, repo)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if files, err = ghClient.GetAllTerraformFiles(ctx, repo.Owner, repo.Name, branch); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read repository files: " + err.Error()})
			return
		}
		readAt = time.Now()
	}

	graph, err := tfmap.BuildGraph(files)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse Terraform files: " + err.Error()})
		return
	}

	// Decorate nodes from the Application manifests: which Applications
	// they belong to, whether their block may be edited, and their source
	// when the Application shows it.
	layout := h.projects.Layout(repo.ID)
	loaded := manifest.LoadIn(files, layout)
	// Cloud accounts attached to the repository, by name.
	attached := map[string]*db.CloudAccount{}
	if accts, err := h.integrations.RepositoryCloudAccounts(repo.ID); err == nil {
		for _, a := range accts {
			attached[a.Name] = a
		}
	}
	// accountStatus: "attached", "not_attached" (exists in TowerCrane but
	// this repository can't use it), "unknown" or "" (none named).
	accountStatus := func(name string) string {
		switch {
		case name == "":
			return ""
		case attached[name] != nil:
			return "attached"
		}
		if _, err := h.integrations.GetCloudAccountByName(name); err == nil {
			return "not_attached"
		}
		return "unknown"
	}

	type application struct {
		// Name is the instance name: "app", or "app@group" in a group.
		Name string `json:"name"`
		// App is the manifest's metadata.name; Group the instance's group.
		App                string `json:"app"`
		Group              string `json:"group,omitempty"`
		Path               string `json:"path"`
		File               string `json:"file"`
		Apply              string `json:"apply"`
		CloudAccount       string `json:"cloud_account,omitempty"`
		CloudAccountStatus string `json:"cloud_account_status,omitempty"`
		// Lifecycle "job": ephemeral, applied and destroyed by Job.
		Lifecycle string `json:"lifecycle,omitempty"`
		Job       string `json:"job,omitempty"`
		// Promotion: instances of earlier groups this one waits for.
		Promotion []string `json:"promotion"`
		EditAll   bool     `json:"edit_all"`
		Status    string   `json:"status"`
		// DependsOn / Dependents come from spec.syncPolicy.dependsOn.
		DependsOn  []string `json:"depends_on"`
		Dependents []string `json:"dependents"`
		// SharesModulesWith lists Applications using some of the same
		// module directories.
		SharesModulesWith []string `json:"shares_modules_with"`
		ResourceCount     int      `json:"resource_count"`
		Engine            string   `json:"engine"`
		Terragrunt        bool     `json:"terragrunt"`
		Manifest          string   `json:"manifest"`
		// SourceRepo is the repository with its code.
		SourceRepo string `json:"source_repo"`
	}
	// Each Application's code: the configuration repository's files, or its
	// spec.source.repo's (from what TowerCrane last read of it).
	type codeView struct {
		repo  string
		files map[string]string
		graph *tfmap.Graph
	}
	codes := map[string]*codeView{repo.FullName: {repo: repo.FullName, files: files, graph: graph}}
	codeOf := map[string]*codeView{}
	for _, app := range loaded.Apps {
		cv := codes[repo.FullName]
		if code, err := resolveCodeRepo(h.projects, repo, app); err == nil && code.ID != repo.ID {
			if cv = codes[code.FullName]; cv == nil {
				cv = &codeView{repo: code.FullName, files: map[string]string{}, graph: &tfmap.Graph{}}
				if snap, err := h.snapshots.readSnapshot(ctx, h.gh, code, ""); err == nil {
					cv.files = mergeProjectFiles(files, snap.Files, layout)
					if g, err := tfmap.BuildGraph(cv.files); err == nil {
						cv.graph = g
					}
				}
				codes[code.FullName] = cv
			}
		}
		codeOf[app.Metadata.Name] = cv
	}

	applications := make([]application, 0, len(loaded.Apps))
	appDirs := map[string]map[string]bool{}
	for _, app := range loaded.Apps {
		appDirs[app.Metadata.Name] = tfmap.ReachableDirs(codeOf[app.Metadata.Name].files, app.Spec.Source.Path)
	}
	for _, app := range loaded.Apps {
		op, _ := h.operationStore.GetLatestByApplication(repo.ID, app.Metadata.Name)
		applications = append(applications, application{
			Name: app.Metadata.Name, Path: app.Spec.Source.Path, File: app.File, Apply: app.ApplyMode(),
			App: app.App, Group: app.Group, Promotion: nonNilList(app.Promotion),
			Lifecycle:    app.Spec.Lifecycle,
			CloudAccount: app.CloudAccount, CloudAccountStatus: accountStatus(app.CloudAccount),
			EditAll:    app.Spec.EditScope.All,
			Status:     statusForOperation(op),
			DependsOn:  nonNilList(app.Spec.SyncPolicy.DependsOn),
			Manifest:   files[app.File],
			Engine:     app.EngineName(),
			Terragrunt: isTerragrunt(codeOf[app.Metadata.Name].files, app),
			SourceRepo: codeOf[app.Metadata.Name].repo,
		})
		cur := &applications[len(applications)-1]
		if j := loaded.JobFor(app.Metadata.Name); j != nil {
			cur.Job = j.Metadata.Name
		}
		cur.Dependents, cur.SharesModulesWith = []string{}, []string{}
		for _, other := range loaded.Apps {
			if other.Metadata.Name == app.Metadata.Name {
				continue
			}
			for _, d := range other.Spec.SyncPolicy.DependsOn {
				if d == app.Metadata.Name {
					cur.Dependents = append(cur.Dependents, other.Metadata.Name)
				}
			}
			if codeOf[other.Metadata.Name] != codeOf[app.Metadata.Name] {
				continue // modules of another repository are other modules
			}
			for d := range appDirs[other.Metadata.Name] {
				if d != other.Spec.Source.Path && appDirs[app.Metadata.Name][d] {
					cur.SharesModulesWith = append(cur.SharesModulesWith, other.Metadata.Name)
					break
				}
			}
		}
		dirs := appDirs[app.Metadata.Name]
		appGraph := codeOf[app.Metadata.Name].graph
		for i := range appGraph.Nodes {
			n := &appGraph.Nodes[i]
			if !dirs[n.Dir] {
				continue
			}
			n.Applications = append(n.Applications, app.Metadata.Name)
			cur.ResourceCount++
			if tfmap.NodeEditable(app, *n) {
				n.Editable = true
			}
			if app.Spec.Display.ShowSource && n.Source == "" {
				n.Source = tfmap.SourceLines(codeOf[app.Metadata.Name].files[n.FilePath], n.StartLine, n.EndLine)
			}
		}
	}
	// Groups: where instances are applied, with their cloud account.
	type groupAccount struct {
		Name         string `json:"name"`
		Provider     string `json:"provider"`
		Region       string `json:"region"`
		AWSAccountID string `json:"aws_account_id"`
		EndpointURL  string `json:"endpoint_url,omitempty"`
		Verified     bool   `json:"verified"`
	}
	type group struct {
		Name               string            `json:"name"`
		File               string            `json:"file"`
		Description        string            `json:"description,omitempty"`
		After              []string          `json:"after"`
		CloudAccount       string            `json:"cloud_account"`
		CloudAccountStatus string            `json:"cloud_account_status"`
		Account            *groupAccount     `json:"account,omitempty"`
		Instances          []string          `json:"instances"`
		Labels             map[string]string `json:"labels,omitempty"`
		Manifest           string            `json:"manifest"`
	}
	groups := make([]group, 0, len(loaded.Groups))
	for _, g := range loaded.Groups {
		out := group{
			Name: g.Metadata.Name, File: g.File, Description: g.Spec.Description,
			After: nonNilList(g.Spec.After), CloudAccount: g.Spec.CloudAccount,
			CloudAccountStatus: accountStatus(g.Spec.CloudAccount),
			Instances:          []string{}, Labels: g.Spec.Labels, Manifest: files[g.File],
		}
		if a := attached[g.Spec.CloudAccount]; a != nil {
			out.Account = &groupAccount{
				Name: a.Name, Provider: a.Provider, Region: a.Region, AWSAccountID: a.AWSAccountID,
				EndpointURL: a.EndpointURL, Verified: a.VerifiedAt != nil && a.VerifyError == "",
			}
		}
		for _, app := range loaded.Apps {
			if app.Group == g.Metadata.Name {
				out.Instances = append(out.Instances, app.Metadata.Name)
			}
		}
		groups = append(groups, out)
	}

	diagrams := diagramViews(loaded.Diagrams)
	manifestWarnings := make([]string, 0, len(loaded.Warnings))
	for _, w := range loaded.Warnings {
		manifestWarnings = append(manifestWarnings, w.Error())
	}

	manifestErrors := make([]string, 0, len(loaded.Errors))
	for _, err := range loaded.Errors {
		manifestErrors = append(manifestErrors, err.Error())
	}

	// ?app=<name> shows only that Application's nodes.
	nodes, edges := graph.Nodes, graph.Edges
	if name := c.Query("app"); name != "" {
		keep := map[string]bool{}
		nodes = nil
		for _, n := range graph.Nodes {
			for _, a := range n.Applications {
				if a == name {
					keep[n.ID] = true
					nodes = append(nodes, n)
					break
				}
			}
		}
		edges = nil
		for _, e := range graph.Edges {
			if keep[e.From] && keep[e.To] {
				edges = append(edges, e)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"applications":      applications,
		"groups":            groups,
		"diagrams":          diagrams,
		"manifest_warnings": manifestWarnings,
		"manifest_errors":   manifestErrors,
		"nodes":             nodes,
		"edges":             edges,
		"branch":            branch,
		"commit_sha":        commitSHA,
		"read_at":           readAt,
	})
}

func nonNilList(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
