package api

import (
	"fmt"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
	"github.com/JESUSLUG/TowerCrane/backend/internal/tfmap"
)

// A rollout only plans the Applications a commit can affect. Each one is
// compared with where it last ended well (applied, no changes or
// unchanged): the manifests' commit and its code's commit (the same
// repository, or its spec.source.repo). It is planned when its manifest or
// Group changed, when any of its code changed (directories, modules,
// Terragrunt includes such as root.hcl), when it never ended well, or when
// an Application it depends on is planned (its inputs may change). The rest
// is "unchanged".

// changesFunc returns the files changed in a repository between two
// commits, or an error (then the Application is planned, to be safe).
type changesFunc func(repo, base, head string) ([]string, error)

// planReason says why an Application is planned; "" means unchanged.
type planReason = string

// appInputs is what the rollout knows about one Application.
type appInputs struct {
	App      *manifest.Application
	Files    map[string]string // its code's files plus the manifests
	Settled  db.Settled        // where it last ended well (zero: never)
	CodeRepo string            // full name of its code repository
	CodeHead string            // its code's current commit
}

// affectedApps decides, for every Application, whether the rollout plans it.
func affectedApps(inputs []appInputs, configRepo, configHead string, changes changesFunc) map[string]planReason {
	reasons := map[string]planReason{}
	for _, in := range inputs {
		name := in.App.Metadata.Name
		if in.Settled.ConfigSHA == "" {
			reasons[name] = "never applied by a rollout"
			continue
		}
		if why, err := touchesSince(in, configRepo, configHead, changes); err != nil {
			reasons[name] = err.Error() + ": planning to be safe"
		} else if why != "" {
			reasons[name] = why
		}
	}

	// Dependents of planned Applications are planned too, transitively.
	for {
		added := false
		for _, in := range inputs {
			name := in.App.Metadata.Name
			if reasons[name] != "" {
				continue
			}
			for _, d := range in.App.Deps() {
				if reasons[d] != "" {
					reasons[name] = fmt.Sprintf("depends on %s, which is planned", d)
					added = true
					break
				}
			}
		}
		if !added {
			return reasons
		}
	}
}

// touchesSince says what changed for one Application since it settled.
func touchesSince(in appInputs, configRepo, configHead string, changes changesFunc) (string, error) {
	compare := func(repo, base, head string) ([]string, error) {
		if base == head {
			return nil, nil
		}
		files, err := changes(repo, base, head)
		if err != nil {
			return nil, fmt.Errorf("could not compare %s with %s", repo, short(base))
		}
		return files, nil
	}
	configChanged, err := compare(configRepo, in.Settled.ConfigSHA, configHead)
	if err != nil {
		return "", err
	}
	for _, p := range configChanged {
		if p == in.App.File {
			return "its manifest changed", nil
		}
		if in.App.GroupFile != "" && p == in.App.GroupFile {
			return "its group changed", nil
		}
	}

	codeChanged := configChanged // code in the configuration repository
	if !sameRepo(in.CodeRepo, configRepo) {
		base := in.Settled.SourceSHA
		if base == "" {
			return "its code repository was never compared", nil
		}
		if codeChanged, err = compare(in.CodeRepo, base, in.CodeHead); err != nil {
			return "", err
		}
	}
	if own := tfmap.AppFiles(in.App, in.Files, in.Files, codeChanged); len(own) > 0 {
		where := own[0]
		if !sameRepo(in.CodeRepo, configRepo) {
			where = in.CodeRepo + ": " + where
		}
		if len(own) == 1 {
			return where + " changed", nil
		}
		return fmt.Sprintf("%s and %d more file(s) changed", where, len(own)-1), nil
	}
	return "", nil
}
