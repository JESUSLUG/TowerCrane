package manifest

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// KindProject declares a TowerCrane project inside its configuration
// repository. Connecting a repository that holds one creates the project:
//
//	apiVersion: towercrane.io/v1alpha1
//	kind: Project
//	metadata:
//	  name: platform
//	spec:
//	  sources:                     # repositories with the code (owner/name)
//	    - acme/infra
//	    - acme/apps
//	  cloudAccounts: [aws-prod]    # requested; an admin authorizes each one
//
// Where the file lives is the project's manifests folder: project.yaml at
// the root means the whole repository is configuration; in .towercrane/ it
// means .towercrane/.
const KindProject = "Project"

type ProjectManifest struct {
	APIVersion string      `yaml:"apiVersion" json:"apiVersion"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       ProjectSpec `yaml:"spec" json:"spec"`
	File       string      `yaml:"-" json:"file"`
}

type ProjectSpec struct {
	Description string `yaml:"description" json:"description,omitempty"`
	// Sources are the repositories (owner/name) Applications may take code
	// from with spec.source.repo, besides this one.
	Sources []string `yaml:"sources" json:"sources,omitempty"`
	// CloudAccounts are the cloud accounts the project asks for. Using one
	// also needs an admin's authorization in TowerCrane: credentials are
	// never granted by a manifest alone.
	CloudAccounts []string `yaml:"cloudAccounts" json:"cloudAccounts,omitempty"`
}

// Layout is the manifests folder the Project manifest defines: its own
// folder.
func (p *ProjectManifest) Layout() Layout {
	dir := path.Dir(p.File)
	if dir == "." {
		return Layout{}
	}
	return Layout{Dir: dir}
}

// ParseProject reads and validates one Project manifest.
func ParseProject(file string, content []byte) (*ProjectManifest, error) {
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	var p ProjectManifest
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: invalid YAML: %w", file, err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: only one manifest per file is supported", file)
	}
	p.File = file
	switch {
	case p.APIVersion != APIVersion:
		return nil, fmt.Errorf("%s: apiVersion must be %q", file, APIVersion)
	case p.Kind != KindProject:
		return nil, fmt.Errorf("%s: kind must be %q", file, KindProject)
	case !nameRe.MatchString(p.Metadata.Name):
		return nil, fmt.Errorf("%s: metadata.name must be lowercase letters, digits and dashes (max 63)", file)
	}
	seen := map[string]bool{}
	for i, r := range p.Spec.Sources {
		r = strings.TrimSpace(r)
		if !repoRe.MatchString(r) {
			return nil, fmt.Errorf("%s: spec.sources[%d] must be a GitHub repository \"owner/name\", got %q", file, i, r)
		}
		if seen[strings.ToLower(r)] {
			return nil, fmt.Errorf("%s: spec.sources: %q is listed twice", file, r)
		}
		seen[strings.ToLower(r)] = true
		p.Spec.Sources[i] = r
	}
	seenAcct := map[string]bool{}
	for _, a := range p.Spec.CloudAccounts {
		if strings.TrimSpace(a) == "" || seenAcct[a] {
			return nil, fmt.Errorf("%s: spec.cloudAccounts: empty or repeated name %q", file, a)
		}
		seenAcct[a] = true
	}
	return &p, nil
}

// FindProject returns the repository's Project manifest, nil if it has
// none, or an error if it has an invalid one or more than one. Only YAML
// outside hidden folders (or in .towercrane/) that declares kind: Project
// and TowerCrane's apiVersion is considered.
func FindProject(files map[string]string) (*ProjectManifest, error) {
	var found []string
	for p, content := range files {
		if !(Layout{}).Holds(p) && !DefaultLayout().Holds(p) {
			continue // not YAML, or in a hidden folder other than .towercrane/
		}
		if kindOf([]byte(content)) == KindProject && isTowerCraneYAML([]byte(content)) {
			found = append(found, p)
		}
	}
	if len(found) == 0 {
		return nil, nil
	}
	sort.Strings(found)
	if len(found) > 1 {
		return nil, fmt.Errorf("a repository can declare only one Project; found %s", strings.Join(found, ", "))
	}
	return ParseProject(found[0], []byte(files[found[0]]))
}
