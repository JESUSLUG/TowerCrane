package api

import "github.com/JESUSLUG/TowerCrane/backend/internal/manifest"

// diagramView is what the UI draws for a group: the Diagram manifest merged
// on the branch (merging is the approval, like any TowerCrane YAML). It is
// purely visual: it never changes the apply order.
type diagramView struct {
	Group string                `json:"group"`
	File  string                `json:"file"`
	Spec  *manifest.DiagramSpec `json:"spec"`
}

func diagramViews(diagrams []*manifest.Diagram) []diagramView {
	views := make([]diagramView, 0, len(diagrams))
	for _, d := range diagrams {
		spec := d.Spec
		views = append(views, diagramView{Group: d.Spec.Group, File: d.File, Spec: &spec})
	}
	return views
}
