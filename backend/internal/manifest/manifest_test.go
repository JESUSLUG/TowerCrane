package manifest

import (
	"strings"
	"testing"
	"time"
)

const devManifest = `apiVersion: towercrane.io/v1alpha1
kind: Application
metadata:
  name: example-dev
spec:
  source:
    path: environments/dev/
  destination:
    cloudAccount: floci-dev
  syncPolicy:
    apply: auto
  editScope:
    rules:
      - files: ["environments/dev/variables.tf"]
        blocks: ["variable.instance_count"]
        dependents: true
  variables:
    instance_count: 2
    tags: {team: infra}
    region: us-east-1
`

func TestParseValidManifest(t *testing.T) {
	app, err := Parse(".towercrane/dev.yaml", []byte(devManifest))
	if err != nil {
		t.Fatal(err)
	}
	if app.Spec.Source.Path != "environments/dev" {
		t.Errorf("path not cleaned: %q", app.Spec.Source.Path)
	}
	if app.ApplyMode() != "auto" {
		t.Errorf("apply = %s", app.ApplyMode())
	}
	for name, want := range map[string]string{"instance_count": "2", "tags": `{"team":"infra"}`, "region": "us-east-1"} {
		got, err := VariableValue(app.Spec.Variables[name])
		if err != nil || got != want {
			t.Errorf("variable %s = %q (%v), want %q", name, got, err, want)
		}
	}
	if !strings.HasPrefix(app.Hash, "sha256:") {
		t.Error("missing content hash")
	}
}

func TestParseRejectsInvalidManifests(t *testing.T) {
	cases := map[string]string{
		"unknown field (typo)":   strings.Replace(devManifest, "dependents:", "dependent:", 1),
		"wrong apiVersion":       strings.Replace(devManifest, "v1alpha1", "v1", 1),
		"wrong kind":             strings.Replace(devManifest, "kind: Application", "kind: App", 1),
		"bad name":               strings.Replace(devManifest, "example-dev", "Example_Dev", 1),
		"path escapes repo":      strings.Replace(devManifest, "environments/dev/", "../other", 1),
		"bad apply mode":         strings.Replace(devManifest, "apply: auto", "apply: always", 1),
		"rule without blocks":    strings.Replace(devManifest, `        blocks: ["variable.instance_count"]`+"\n", "", 1),
		"planGuard is gone":      devManifest[:strings.Index(devManifest, "  variables:")] + "    planGuard:\n      denyActions: [delete]\n",
		"bad rule apply":         strings.Replace(devManifest, "    apply: auto\n", "    apply: auto\n    rules:\n      - when: {actions: [create]}\n        apply: always\n", 1),
		"bad rule action":        strings.Replace(devManifest, "    apply: auto\n", "    apply: auto\n    rules:\n      - when: {actions: [nuke]}\n        apply: auto\n", 1),
		"bad engine":             strings.Replace(devManifest, "    path: environments/dev/\n", "    path: environments/dev/\n    engine: pulumi\n", 1),
		"bad variable name":      strings.Replace(devManifest, "instance_count: 2", "1bad: 2", 1),
		"all plus rules":         strings.Replace(devManifest, "    rules:", "    all: true\n    rules:", 1),
		"two documents per file": devManifest + "---\n" + devManifest,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(".towercrane/x.yaml", []byte(content)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func secretsApp(t *testing.T) *Application {
	t.Helper()
	m := `apiVersion: towercrane.io/v1alpha1
kind: Application
metadata:
  name: secrets
spec:
  source:
    path: secret-manager
    engine: terraform
  syncPolicy:
    apply: manual
    rules:
      - when:
          actions: [create]
          resources: ["aws_secretsmanager_secret.*", "aws_secretsmanager_secret_version.*"]
        apply: auto
      - when:
          actions: [delete]
        apply: reject
`
	a, err := Parse(".towercrane/secrets.yaml", []byte(m))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDecideRules(t *testing.T) {
	a := secretsApp(t)
	if a.EngineName() != EngineTerraform {
		t.Errorf("engine = %s", a.EngineName())
	}
	cases := []struct {
		name    string
		changes []PlannedChange
		want    string
	}{
		{"only new secrets: auto", []PlannedChange{
			{"aws_secretsmanager_secret.db", ActionCreate},
			{"module.x.aws_secretsmanager_secret_version.db[0]", ActionCreate},
		}, ApplyAuto},
		{"secret updated: default manual", []PlannedChange{
			{"aws_secretsmanager_secret.db", ActionCreate},
			{"aws_secretsmanager_secret.other", ActionUpdate},
		}, ApplyManual},
		{"anything else created: manual", []PlannedChange{{"aws_iam_role.r", ActionCreate}}, ApplyManual},
		{"deleting a secret: reject", []PlannedChange{
			{"aws_secretsmanager_secret.db", ActionCreate},
			{"aws_secretsmanager_secret.old", ActionDelete},
		}, ApplyReject},
		{"replace hits the implicit rule: reject", []PlannedChange{{"aws_lambda_function.f", ActionReplace}}, ApplyReject},
		{"no-ops ignored: auto", []PlannedChange{{"aws_vpc.main", ""}}, ApplyAuto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := a.Decide(c.changes)
			if d.Apply != c.want {
				t.Errorf("Decide = %s (%v), want %s", d.Apply, d.Reasons, c.want)
			}
			if d.Apply != ApplyAuto && len(d.Reasons) == 0 {
				t.Error("a non-auto decision must explain itself")
			}
		})
	}
}

func TestRulesCanAllowDeletes(t *testing.T) {
	m := strings.Replace(devManifest, "    apply: auto\n", "    apply: auto\n    rules:\n      - when: {actions: [delete]}\n        apply: manual\n", 1)
	a, err := Parse(".towercrane/x.yaml", []byte(m))
	if err != nil {
		t.Fatal(err)
	}
	if d := a.Decide([]PlannedChange{{"aws_s3_bucket.b", ActionDelete}}); d.Apply != ApplyManual {
		t.Errorf("an explicit rule must override the implicit reject, got %s", d.Apply)
	}
	if d := a.Decide([]PlannedChange{{"aws_s3_bucket.b", ActionReplace}}); d.Apply != ApplyReject {
		t.Errorf("replace is still rejected by the implicit rule, got %s", d.Apply)
	}
}

func TestLoadSkipsDuplicatesAndInvalid(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/a.yaml":       devManifest,
		".towercrane/b.yml":        devManifest, // same name
		".towercrane/broken.yaml":  "kind: [",
		".towercrane/sub/x.yaml":   devManifest, // subdirectories count too: same name again
		"environments/dev/main.tf": "",
		".towercrane/README.md":    "docs",
	})
	if len(res.Apps) != 1 || res.Apps[0].File != ".towercrane/a.yaml" {
		t.Fatalf("apps = %v", res.Apps)
	}
	if len(res.Errors) != 3 {
		t.Errorf("errors = %v", res.Errors)
	}
}

func groupYAML(name, account string, after ...string) string {
	s := "apiVersion: towercrane.io/v1alpha1\nkind: Group\nmetadata:\n  name: " + name + "\nspec:\n  cloudAccount: " + account + "\n"
	if len(after) > 0 {
		s += "  after: [" + strings.Join(after, ", ") + "]\n"
	}
	return s
}

func appYAML(name, path, groups string, deps ...string) string {
	s := "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: " + name + "\nspec:\n"
	if groups != "" {
		s += "  groups: [" + groups + "]\n"
	}
	s += "  source:\n    path: " + path + "\n"
	if len(deps) > 0 {
		s += "  syncPolicy:\n    dependsOn: [" + strings.Join(deps, ", ") + "]\n"
	}
	return s
}

func TestGroupsExpandIntoInstances(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/groups/sandbox.yaml": groupYAML("sandbox", "floci-example"),
		".towercrane/groups/prod.yaml":    groupYAML("prod", "aws-prod", "sandbox"),
		".towercrane/vpc.yaml":            appYAML("vpc", "vpc", "sandbox, prod"),
		".towercrane/svc.yaml":            appYAML("svc", "lambdas/svc", "sandbox, prod", "vpc"),
	})
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if len(res.Groups) != 2 || len(res.Apps) != 4 {
		t.Fatalf("groups=%d apps=%d", len(res.Groups), len(res.Apps))
	}
	svc := res.ByName("svc@prod")
	if svc == nil {
		t.Fatal("svc@prod missing")
	}
	if svc.App != "svc" || svc.Group != "prod" || svc.CloudAccount != "aws-prod" || svc.GroupFile != ".towercrane/groups/prod.yaml" {
		t.Errorf("instance = %+v", svc)
	}
	if got := strings.Join(svc.Spec.SyncPolicy.DependsOn, ","); got != "vpc@prod" {
		t.Errorf("dependsOn resolved inside the group, got %s", got)
	}
	if got := strings.Join(svc.Promotion, ","); got != "svc@sandbox,vpc@sandbox" {
		t.Errorf("promotion = %s", got)
	}
	if res.ByName("svc@sandbox").Hash == res.ByName("svc@prod").Hash {
		t.Error("each instance's hash must include its group manifest")
	}
	order := strings.Join(ApplyOrder(res.Apps), ",")
	if order != "vpc@sandbox,svc@sandbox,vpc@prod,svc@prod" {
		t.Errorf("order = %s", order)
	}
	if err := ValidOrder(res.Apps, []string{"vpc@sandbox", "vpc@prod", "svc@sandbox", "svc@prod"}); err == nil {
		t.Error("prod must not start before every sandbox instance")
	}
}

func TestGroupErrors(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/groups/sandbox.yaml": groupYAML("sandbox", "floci"),
		".towercrane/groups/a.yaml":       groupYAML("a", "x", "b"),
		".towercrane/groups/b.yaml":       groupYAML("b", "x", "a"),
		".towercrane/svc.yaml":            appYAML("svc", "svc", "sandbox, staging"),
		".towercrane/legacy.yaml":         appYAML("legacy", "legacy", "", "svc"),
	})
	if res.ByName("svc@sandbox") == nil {
		t.Error("svc@sandbox must load")
	}
	if res.ByName("svc@staging") != nil || res.Group("a") != nil || res.ByName("legacy") != nil {
		t.Error("unknown group, group cycle and ungrouped->grouped dependency must be dropped")
	}
	if len(res.Errors) != 4 {
		t.Errorf("errors = %v", res.Errors)
	}

	bad := strings.Replace(appYAML("x", "x", "sandbox"), "  source:", "  destination:\n    cloudAccount: other\n  source:", 1)
	if _, err := Parse(".towercrane/x.yaml", []byte(bad)); err == nil {
		t.Error("destination.cloudAccount with groups must be rejected")
	}
	single := strings.Replace(appYAML("x", "x", ""), "spec:\n", "spec:\n  group: sandbox\n", 1)
	a, err := Parse(".towercrane/x.yaml", []byte(single))
	if err != nil || strings.Join(a.Spec.Groups, ",") != "sandbox" {
		t.Errorf("group: shorthand: %v %v", err, a)
	}
}

func TestMatchGlobAndBlock(t *testing.T) {
	globs := []struct {
		pat, p string
		want   bool
	}{
		{"modules/compute/*.tf", "modules/compute/main.tf", true},
		{"modules/compute/*.tf", "modules/compute/sub/main.tf", false},
		{"modules/**", "modules/compute/sub/main.tf", true},
		{"**/*.tf", "main.tf", true},
		{"environments/dev/variables.tf", "environments/dev/variables.tf", true},
		{"environments/*/main.tf", "environments/prod/main.tf", true},
	}
	for _, g := range globs {
		if got := MatchGlob(g.pat, g.p); got != g.want {
			t.Errorf("MatchGlob(%q, %q) = %v", g.pat, g.p, got)
		}
	}
	blocks := []struct {
		pat, addr string
		want      bool
	}{
		{"*", "resource.aws_instance.main", true},
		{"variable.*", "variable.instance_count", true},
		{"variable.*", "resource.aws_instance.main", false},
		{"resource.aws_instance.*", "resource.aws_instance.main", true},
		{"module.compute", "module.compute", true},
		{"module.compute", "module.compute2", false},
	}
	for _, b := range blocks {
		if got := MatchBlock(b.pat, b.addr); got != b.want {
			t.Errorf("MatchBlock(%q, %q) = %v", b.pat, b.addr, got)
		}
	}
}

func app(name string, deps ...string) string {
	m := "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: " + name +
		"\nspec:\n  source:\n    path: environments/" + name + "\n"
	if len(deps) > 0 {
		m += "  syncPolicy:\n    dependsOn: [" + strings.Join(deps, ", ") + "]\n"
	}
	return m
}

func TestDependsOnOrderAndValidation(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/secrets.yaml": app("secrets"),
		".towercrane/db.yaml":      app("db", "secrets"),
		".towercrane/service.yaml": app("service", "db", "secrets"),
		".towercrane/web.yaml":     app("web"),
	})
	if len(res.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	order := ApplyOrder(res.Apps)
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if !(pos["secrets"] < pos["db"] && pos["db"] < pos["service"]) {
		t.Errorf("bad order %v", order)
	}
	if err := ValidOrder(res.Apps, order); err != nil {
		t.Errorf("computed order must be valid: %v", err)
	}
	if err := ValidOrder(res.Apps, []string{"web", "service", "secrets", "db"}); err == nil {
		t.Error("service before its dependencies must be rejected")
	}
	if err := ValidOrder(res.Apps, []string{"secrets", "web", "db", "service"}); err != nil {
		t.Errorf("moving an independent Application must be allowed: %v", err)
	}
}

func TestDependsOnRejectsMissingAndCycles(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/a.yaml": app("a", "b"),
		".towercrane/b.yaml": app("b", "a"),
		".towercrane/c.yaml": app("c", "missing"),
		".towercrane/d.yaml": app("d", "c"), // depends on an invalid one: dropped too
		".towercrane/e.yaml": app("e"),
	})
	if len(res.Apps) != 1 || res.Apps[0].Metadata.Name != "e" {
		names := []string{}
		for _, a := range res.Apps {
			names = append(names, a.Metadata.Name)
		}
		t.Fatalf("only e should survive, got %v (errors %v)", names, res.Errors)
	}
	if len(res.Errors) != 4 {
		t.Errorf("expected 4 errors, got %v", res.Errors)
	}
	if _, err := Parse(".towercrane/x.yaml", []byte(app("x", "x"))); err == nil {
		t.Error("self dependency must be rejected")
	}
}

const diagramYAML = `apiVersion: towercrane.io/v1alpha1
kind: Diagram
metadata:
  name: sandbox
spec:
  group: sandbox
  sections:
    - name: Network
      color: blue
      apps: [vpc, svc]
  nodes:
    vpc: {color: green}
    svc: {color: red, label: Service}
  links:
    - {from: vpc, to: svc, label: subnets}
`

func TestDiagram(t *testing.T) {
	files := map[string]string{
		".towercrane/groups/sandbox.yaml":   groupYAML("sandbox", "floci"),
		".towercrane/vpc.yaml":              appYAML("vpc", "vpc", "sandbox"),
		".towercrane/svc.yaml":              appYAML("svc", "svc", "sandbox", "vpc"),
		".towercrane/diagrams/sandbox.yaml": diagramYAML,
	}
	res := Load(files)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	d := res.DiagramFor("sandbox")
	if d == nil || d.Spec.Nodes["svc"].Label != "Service" || len(d.Spec.Sections[0].Apps) != 2 {
		t.Fatalf("diagram = %+v", d)
	}
	if res.ByName("svc@sandbox").Hash != Load(map[string]string{
		".towercrane/groups/sandbox.yaml": files[".towercrane/groups/sandbox.yaml"],
		".towercrane/vpc.yaml":            files[".towercrane/vpc.yaml"],
		".towercrane/svc.yaml":            files[".towercrane/svc.yaml"],
	}).ByName("svc@sandbox").Hash {
		t.Error("a Diagram must not change the policy hash of Applications")
	}

	files[".towercrane/diagrams/sandbox.yaml"] = strings.Replace(diagramYAML, "color: red", "color: magenta", 1)
	if res := Load(files); len(res.Errors) != 1 || res.DiagramFor("sandbox") != nil {
		t.Errorf("unknown color: %v", res.Errors)
	}
	files[".towercrane/diagrams/sandbox.yaml"] = strings.Replace(diagramYAML, "apps: [vpc, svc]", "apps: [vpc, svc, ghost]", 1)
	if res := Load(files); len(res.Errors) != 0 || len(res.Warnings) != 1 || res.DiagramFor("sandbox") == nil {
		t.Errorf("unknown app is a warning and the diagram is kept: %v %v", res.Errors, res.Warnings)
	}
}

func TestSameNameInDifferentGroupsAndCrossGroupDeps(t *testing.T) {
	res := Load(map[string]string{
		".towercrane/groups/general.yaml": groupYAML("general", "aws"),
		".towercrane/groups/sandbox.yaml": groupYAML("sandbox", "aws"),
		".towercrane/groups/prod.yaml":    groupYAML("prod", "aws"),
		".towercrane/vpc.yaml":            appYAML("vpc", "us-east-1/vpc", "general"),
		".towercrane/sandbox/ecr.yaml":    appYAML("ecr", "us-east-1/sandbox/ecr", "sandbox"),
		".towercrane/prod/ecr.yaml":       appYAML("ecr", "us-east-1/prod/ecr", "prod"),
		".towercrane/sandbox/svc.yaml":    appYAML("svc", "us-east-1/sandbox/svc", "sandbox", "vpc@general", "ecr"),
		".towercrane/prod/svc.yaml":       appYAML("svc", "us-east-1/prod/svc", "prod", "vpc@general", "ecr"),
	})
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if res.ByName("ecr@sandbox").Spec.Source.Path != "us-east-1/sandbox/ecr" || res.ByName("ecr@prod").Spec.Source.Path != "us-east-1/prod/ecr" {
		t.Error("each group keeps its own manifest and path")
	}
	if got := strings.Join(res.ByName("svc@prod").Deps(), ","); got != "vpc@general,ecr@prod" {
		t.Errorf("deps = %s", got)
	}
	order := strings.Join(ApplyOrder(res.Apps), ",")
	if strings.Index(order, "vpc@general") > strings.Index(order, "svc@sandbox") {
		t.Errorf("vpc must come first: %s", order)
	}

	dup := Load(map[string]string{
		".towercrane/groups/sandbox.yaml": groupYAML("sandbox", "aws"),
		".towercrane/a.yaml":              appYAML("ecr", "a", "sandbox"),
		".towercrane/b.yaml":              appYAML("ecr", "b", "sandbox"),
	})
	if len(dup.Errors) != 1 || len(dup.Apps) != 1 {
		t.Errorf("same name in the same group is a duplicate: %v", dup.Errors)
	}
	missing := Load(map[string]string{
		".towercrane/groups/sandbox.yaml": groupYAML("sandbox", "aws"),
		".towercrane/svc.yaml":            appYAML("svc", "svc", "sandbox", "vpc@nowhere"),
	})
	if len(missing.Errors) != 1 || len(missing.Apps) != 0 {
		t.Errorf("a missing cross-group dependency drops the instance: %v", missing.Errors)
	}
}

func jobYAML(spec string) string {
	return "apiVersion: towercrane.io/v1alpha1\nkind: Job\nmetadata:\n  name: nightly\nspec:\n" + spec
}

func TestJobs(t *testing.T) {
	ephemeral := strings.Replace(appYAML("batch", "batch", "prod"), "spec:\n", "spec:\n  lifecycle: job\n", 1)
	files := map[string]string{
		".towercrane/groups/prod.yaml": groupYAML("prod", "aws"),
		".towercrane/batch.yaml":       ephemeral,
		".towercrane/web.yaml":         appYAML("web", "web", "prod"),
		".towercrane/jobs/nightly.yaml": jobYAML(`  application: batch
  group: prod
  timezone: America/Mexico_City
  apply: {schedule: "0 9 * * 1-5"}
  destroy: {after: 1h}
`),
	}
	res := Load(files)
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	j := res.JobFor("batch@prod")
	if j == nil || j.ApplySchedule() == nil || j.DestroyAfter() != time.Hour || !j.ManualEnabled() {
		t.Fatalf("job = %+v", j)
	}

	// A Job can only run an ephemeral Application.
	files[".towercrane/jobs/nightly.yaml"] = strings.Replace(files[".towercrane/jobs/nightly.yaml"], "application: batch", "application: web", 1)
	if res := Load(files); len(res.Jobs) != 0 || len(res.Errors) != 1 {
		t.Errorf("job on a non-ephemeral Application: %v", res.Errors)
	}

	// Nothing may depend on an ephemeral Application.
	files[".towercrane/web.yaml"] = appYAML("web", "web", "prod", "batch")
	if res := Load(files); res.ByName("web@prod") != nil {
		t.Error("depending on an ephemeral Application must be refused")
	}

	for _, bad := range []string{
		"  application: batch\n  apply: {schedule: \"99 * * * *\"}\n",
		"  application: batch\n  destroy: {after: 1m}\n",
		"  application: batch\n  destroy: {after: 1h, schedule: \"0 1 * * *\"}\n",
		"  application: batch\n  manual: false\n",
		"  application: batch\n  timezone: Nowhere/Land\n",
	} {
		if _, err := ParseJob("j.yaml", []byte(jobYAML(bad))); err == nil {
			t.Errorf("must be rejected:\n%s", bad)
		}
	}
}

func TestLayouts(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/": "/", ".": "/", ".towercrane": ".towercrane/", "/config/": "config/", "deploy/tc": "deploy/tc/"} {
		l, err := NewLayout(in)
		if err != nil || l.String() != want {
			t.Errorf("NewLayout(%q) = %q, %v; want %q", in, l.String(), err, want)
		}
	}
	for _, bad := range []string{"../x", "a/../b", `a\b`} {
		if _, err := NewLayout(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}

	files := map[string]string{
		"groups/sb.yaml":           groupYAML("sb", "aws"),
		"sb/svc.yaml":              appYAML("svc", "svc", "sb"),
		".github/workflows/ci.yml": "name: ci\non: push\n",
		"docker-compose.yaml":      "services: {}\n",
		"broken.yaml":              "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: Bad Name\n",
	}
	res := LoadIn(files, Layout{})
	if res.ByName("svc@sb") == nil || res.ByName("svc@sb").File != "sb/svc.yaml" {
		t.Fatalf("root layout: %+v", res.Apps)
	}
	if len(res.Errors) != 1 {
		t.Errorf("only the broken TowerCrane manifest is an error, not CI or compose files: %v", res.Errors)
	}
	if got := Load(files); len(got.Apps) != 0 {
		t.Error("the default layout only reads .towercrane/")
	}
}

const projectYAML = `apiVersion: towercrane.io/v1alpha1
kind: Project
metadata:
  name: platform
spec:
  sources: [acme/infra, acme/apps]
  cloudAccounts: [aws-prod]
`

func TestFindProject(t *testing.T) {
	files := map[string]string{
		"project.yaml":        projectYAML,
		"groups/sb.yaml":      groupYAML("sb", "aws-prod"),
		"sb/svc.yaml":         appYAML("svc", "svc", "sb"),
		".github/ci.yml":      "name: ci\n",
		"docker-compose.yaml": "services: {}\n",
	}
	p, err := FindProject(files)
	if err != nil || p == nil || p.Metadata.Name != "platform" || len(p.Spec.Sources) != 2 || !p.Layout().Root() {
		t.Fatalf("project = %+v, %v", p, err)
	}
	if res := LoadIn(files, p.Layout()); len(res.Errors) != 0 || res.ByName("svc@sb") == nil {
		t.Errorf("the Project file is not an Application: %v", res.Errors)
	}

	nested := map[string]string{".towercrane/project.yaml": projectYAML, "main.tf": ""}
	if p, err := FindProject(nested); err != nil || p == nil || p.Layout().Dir != ".towercrane" {
		t.Errorf(".towercrane/project.yaml -> manifests folder .towercrane/: %+v %v", p, err)
	}
	if p, _ := FindProject(map[string]string{".github/project.yaml": projectYAML}); p != nil {
		t.Error("other hidden folders are never configuration")
	}
	nested = map[string]string{"config/project.yaml": projectYAML}
	if p, err := FindProject(nested); err != nil || p.Layout().Dir != "config" {
		t.Errorf("project.yaml in config/ -> manifests folder config/: %+v %v", p, err)
	}

	if p, err := FindProject(map[string]string{"main.tf": ""}); p != nil || err != nil {
		t.Error("no Project manifest: nil, nil")
	}
	if _, err := FindProject(map[string]string{"a.yaml": projectYAML, "b/project.yaml": projectYAML}); err == nil {
		t.Error("two Projects in one repository must be refused")
	}
	bad := strings.Replace(projectYAML, "acme/apps", "not a repo", 1)
	if _, err := FindProject(map[string]string{"project.yaml": bad}); err == nil {
		t.Error("invalid source must be refused")
	}
}
