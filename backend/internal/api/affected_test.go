package api

import (
	"errors"
	"testing"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
)

func TestAffectedApps(t *testing.T) {
	files := map[string]string{
		"root.hcl":                   "",
		"modules/ecr/main.tf":        "resource \"aws_ecr_repository\" \"r\" {}\n",
		"modules/lambda/main.tf":     "resource \"aws_lambda_function\" \"f\" {}\n",
		"sandbox/ecr/terragrunt.hcl": "include \"root\" {\n  path = find_in_parent_folders(\"root.hcl\")\n}\nterraform {\n  source = \"../../modules//ecr\"\n}\n",
		"sandbox/svc/terragrunt.hcl": "include \"root\" {\n  path = find_in_parent_folders(\"root.hcl\")\n}\nterraform {\n  source = \"../../modules//lambda\"\n}\n",
		"prod/ecr/terragrunt.hcl":    "include \"root\" {\n  path = find_in_parent_folders(\"root.hcl\")\n}\nterraform {\n  source = \"../../modules//ecr\"\n}\n",
		".towercrane/groups/sb.yaml": "apiVersion: towercrane.io/v1alpha1\nkind: Group\nmetadata:\n  name: sb\nspec:\n  cloudAccount: aws\n",
		".towercrane/groups/pr.yaml": "apiVersion: towercrane.io/v1alpha1\nkind: Group\nmetadata:\n  name: pr\nspec:\n  cloudAccount: aws\n",
		".towercrane/sb/ecr.yaml":    "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: ecr\nspec:\n  groups: [sb]\n  source:\n    path: sandbox/ecr\n",
		".towercrane/sb/svc.yaml":    "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: svc\nspec:\n  groups: [sb]\n  source:\n    path: sandbox/svc\n  syncPolicy:\n    dependsOn: [ecr]\n",
		".towercrane/pr/ecr.yaml":    "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: ecr\nspec:\n  groups: [pr]\n  source:\n    path: prod/ecr\n",
	}
	loaded := manifest.Load(files)
	if len(loaded.Errors) > 0 {
		t.Fatal(loaded.Errors)
	}
	settledAll := db.Settled{ConfigSHA: "old", SourceSHA: "old"}
	inputs := func(settled map[string]db.Settled) []appInputs {
		var out []appInputs
		for _, a := range loaded.Apps {
			st, ok := settled[a.Metadata.Name]
			if !ok {
				st = settledAll
			}
			out = append(out, appInputs{App: a, Files: files, Settled: st, CodeRepo: "acme/cfg", CodeHead: "head"})
		}
		return out
	}
	run := func(changed []string, settled map[string]db.Settled, err error) map[string]string {
		return affectedApps(inputs(settled), "acme/cfg", "head", func(_, _, _ string) ([]string, error) { return changed, err })
	}

	r := run([]string{"modules/lambda/main.tf"}, nil, nil)
	if r["svc@sb"] == "" || r["ecr@sb"] != "" || r["ecr@pr"] != "" {
		t.Errorf("only svc uses modules/lambda: %v", r)
	}

	r = run([]string{"sandbox/ecr/terragrunt.hcl"}, nil, nil)
	if r["ecr@sb"] == "" || r["svc@sb"] == "" || r["ecr@pr"] != "" {
		t.Errorf("sandbox ecr and its dependent svc, not prod: %v", r)
	}

	r = run([]string{"root.hcl"}, nil, nil)
	if r["ecr@sb"] == "" || r["svc@sb"] == "" || r["ecr@pr"] == "" {
		t.Errorf("root.hcl touches every unit: %v", r)
	}

	r = run([]string{".towercrane/groups/pr.yaml"}, nil, nil)
	if r["ecr@pr"] == "" || r["ecr@sb"] != "" {
		t.Errorf("a group change plans its Applications only: %v", r)
	}

	r = run([]string{"README.md"}, nil, nil)
	if len(r) != 0 {
		t.Errorf("nothing of theirs changed: %v", r)
	}

	r = run(nil, map[string]db.Settled{"svc@sb": {}}, nil)
	if r["svc@sb"] == "" || len(r) != 1 {
		t.Errorf("never settled -> planned: %v", r)
	}

	r = run([]string{"x"}, nil, errors.New("github down"))
	if len(r) != 3 {
		t.Errorf("compare failed -> plan everything: %v", r)
	}
}

// The code of an Application lives in another repository than its manifest.
func TestAffectedAppsSeparateCodeRepo(t *testing.T) {
	cfg := map[string]string{
		".towercrane/groups/sb.yaml": "apiVersion: towercrane.io/v1alpha1\nkind: Group\nmetadata:\n  name: sb\nspec:\n  cloudAccount: aws\n",
		".towercrane/svc.yaml":       "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: svc\nspec:\n  groups: [sb]\n  source:\n    repo: acme/apps\n    path: svc\n",
	}
	code := map[string]string{
		"svc/main.tf":     "resource \"aws_lambda_function\" \"f\" {}\n",
		"other/main.tf":   "resource \"aws_s3_bucket\" \"b\" {}\n",
		".towercrane/x.y": "ignored",
	}
	loaded := manifest.Load(cfg)
	in := []appInputs{{App: loaded.ByName("svc@sb"), Files: mergeProjectFiles(cfg, code, manifest.DefaultLayout()),
		Settled: db.Settled{ConfigSHA: "c1", SourceSHA: "a1"}, CodeRepo: "acme/apps", CodeHead: "a2"}}
	changes := map[string][]string{}
	fn := func(repo, base, head string) ([]string, error) { return changes[repo+" "+base+" "+head], nil }

	changes["acme/apps a1 a2"] = []string{"other/main.tf"}
	if r := affectedApps(in, "acme/cfg", "c1", fn); len(r) != 0 {
		t.Errorf("another directory of the code repository: %v", r)
	}
	changes["acme/apps a1 a2"] = []string{"svc/main.tf"}
	if r := affectedApps(in, "acme/cfg", "c1", fn); r["svc@sb"] == "" {
		t.Errorf("its code changed: %v", r)
	}
	changes["acme/apps a1 a2"] = nil
	changes["acme/cfg c1 c2"] = []string{".towercrane/svc.yaml"}
	if r := affectedApps(in, "acme/cfg", "c2", fn); r["svc@sb"] != "its manifest changed" {
		t.Errorf("its manifest changed in the configuration repository: %v", r)
	}
	changes["acme/cfg c1 c2"] = []string{"svc/main.tf"} // same path, but in the config repository
	if r := affectedApps(in, "acme/cfg", "c2", fn); len(r) != 0 {
		t.Errorf("a file of the configuration repository is not its code: %v", r)
	}
}
