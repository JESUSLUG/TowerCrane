package tfmap

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JESUSLUG/TowerCrane/backend/internal/manifest"
)

func repoFiles() map[string]string {
	return map[string]string{
		"environments/dev/main.tf": `module "networking" {
  source = "../../modules/networking"
}

module "compute" {
  source         = "../../modules/compute"
  instance_count = var.instance_count
  subnet_id      = module.networking.subnet_id
}

locals {
  tags = { team = "infra" }
  name = "dev"
}
`,
		"environments/dev/variables.tf": `variable "instance_count" {
  default = 1
}

variable "region" {
  default = "us-east-1"
}
`,
		"modules/compute/main.tf": `variable "instance_count" {}
variable "subnet_id" {}

resource "aws_instance" "main" {
  count     = var.instance_count
  subnet_id = var.subnet_id
}

resource "aws_iam_role" "ec2" {
  name = "ec2"
}
`,
		"modules/networking/main.tf": `resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_subnet" "a" {
  vpc_id = aws_vpc.main.id
}

output "subnet_id" {
  value = aws_subnet.a.id
}
`,
		"environments/prod/main.tf": `module "networking" {
  source = "../../modules/networking"
}
`,
	}
}

func app(t *testing.T, editScope string) *manifest.Application {
	t.Helper()
	m := `apiVersion: towercrane.io/v1alpha1
kind: Application
metadata:
  name: dev
spec:
  source:
    path: environments/dev
  editScope:
` + editScope
	a, err := manifest.Parse(".towercrane/dev.yaml", []byte(m))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func edit(files map[string]string, p, from, to string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		out[k] = v
	}
	out[p] = strings.Replace(out[p], from, to, 1)
	return out
}

func TestChangedBlocks(t *testing.T) {
	old := repoFiles()["environments/dev/variables.tf"]
	moved := "# a comment\n\n" + old // only a comment added: no block changed
	if got, _ := ChangedBlocks("v.tf", old, moved); len(got) != 0 {
		t.Errorf("comment-only change reported blocks %v", got)
	}
	changed := strings.Replace(old, "default = 1", "default = 3", 1)
	if got, _ := ChangedBlocks("v.tf", old, changed); !reflect.DeepEqual(got, []string{"variable.instance_count"}) {
		t.Errorf("got %v", got)
	}
	main := repoFiles()["environments/dev/main.tf"]
	if got, _ := ChangedBlocks("m.tf", main, strings.Replace(main, `"dev"`, `"dev2"`, 1)); !reflect.DeepEqual(got, []string{"local.name"}) {
		t.Errorf("locals must be tracked one by one, got %v", got)
	}
	if got, _ := ChangedBlocks("n.tf", "", old); len(got) != 2 {
		t.Errorf("new file: every block is added, got %v", got)
	}
	if _, err := ChangedBlocks("bad.tf", old, "resource {"); err == nil {
		t.Error("unparseable file must be an error")
	}
}

func TestDiffGate(t *testing.T) {
	files := repoFiles()
	a := app(t, `    rules:
      - files: ["environments/dev/variables.tf"]
        blocks: ["variable.instance_count"]
`)

	ok := edit(files, "environments/dev/variables.tf", "default = 1", "default = 2")
	if v := DiffGate(a, files, ok, []string{"environments/dev/variables.tf"}); len(v) != 0 {
		t.Errorf("allowed block change blocked: %v", v)
	}

	otherBlock := edit(files, "environments/dev/variables.tf", "us-east-1", "eu-west-1")
	v := DiffGate(a, files, otherBlock, []string{"environments/dev/variables.tf"})
	if len(v) != 1 || v[0].Kind != ViolationBlockNotInScope || v[0].Block != "variable.region" {
		t.Errorf("expected variable.region to be blocked, got %v", v)
	}

	otherFile := edit(files, "modules/networking/main.tf", "10.0.0.0/16", "10.1.0.0/16")
	v = DiffGate(a, files, otherFile, []string{"modules/networking/main.tf"})
	if len(v) != 1 || v[0].Kind != ViolationFileNotInScope {
		t.Errorf("expected file-not-in-scope, got %v", v)
	}

	// Other environments, docs and manifests are not this Application's code.
	v = DiffGate(a, files, files, []string{"environments/prod/main.tf", "README.md", ".towercrane/dev.yaml"})
	if len(v) != 0 {
		t.Errorf("unrelated files must be ignored, got %v", v)
	}

	// Non-HCL files need a whole-file rule.
	v = DiffGate(a, files, files, []string{"environments/dev/terraform.tfvars"})
	if len(v) != 1 || v[0].Kind != ViolationFileNotInScope {
		t.Errorf("tfvars outside any rule must be blocked, got %v", v)
	}
	whole := app(t, `    rules:
      - files: ["environments/dev/*.tfvars"]
        blocks: ["*"]
`)
	if v := DiffGate(whole, files, files, []string{"environments/dev/terraform.tfvars"}); len(v) != 0 {
		t.Errorf("whole-file rule must allow tfvars, got %v", v)
	}

	all := app(t, "    all: true\n")
	if v := DiffGate(all, files, otherFile, []string{"modules/networking/main.tf"}); len(v) != 0 {
		t.Errorf("all: true must allow everything, got %v", v)
	}
}

func TestAllowedWithDependents(t *testing.T) {
	files := repoFiles()

	noDeps := Allowed(app(t, `    rules:
      - files: ["environments/dev/variables.tf"]
        blocks: ["variable.instance_count"]
`), files)
	if noDeps.Matches("module.compute.aws_instance.main[0]") {
		t.Error("without dependents, a variable allows no resources")
	}

	deps := Allowed(app(t, `    rules:
      - files: ["environments/dev/variables.tf"]
        blocks: ["variable.instance_count"]
        dependents: true
`), files)
	// var.instance_count -> module.compute (input) -> everything in that instance.
	for _, addr := range []string{"module.compute.aws_instance.main[0]", "module.compute.aws_iam_role.ec2"} {
		if !deps.Matches(addr) {
			t.Errorf("%s should be allowed as a dependent; allowed=%v", addr, deps.Describe())
		}
	}
	if deps.Matches("module.networking.aws_vpc.main") {
		t.Error("networking does not use instance_count and must stay blocked")
	}

	res := Allowed(app(t, `    rules:
      - files: ["modules/compute/main.tf"]
        blocks: ["resource.aws_instance.main"]
`), files)
	if !res.Matches(`module.compute.aws_instance.main[1]`) || res.Matches("module.compute.aws_iam_role.ec2") {
		t.Errorf("resource rule must allow exactly that resource, got %v", res.Describe())
	}

	// An output used by another module call carries dependents across modules.
	vpc := Allowed(app(t, `    rules:
      - files: ["modules/networking/main.tf"]
        blocks: ["resource.aws_subnet.a"]
        dependents: true
`), files)
	if !vpc.Matches("module.networking.aws_subnet.a") || !vpc.Matches("module.compute.aws_instance.main[0]") {
		t.Errorf("subnet -> output.subnet_id -> module.compute should be allowed, got %v", vpc.Describe())
	}
	if vpc.Matches("module.networking.aws_vpc.main") {
		t.Error("the VPC is used BY the subnet, not a user of it: must stay blocked")
	}
}

func TestPlanGate(t *testing.T) {
	files := repoFiles()
	a := app(t, `    rules:
      - files: ["modules/compute/main.tf"]
        blocks: ["*"]
`)
	changes := []PlanChange{
		{Address: "module.compute.aws_instance.main[0]", Action: manifest.ActionUpdate},
		{Address: "module.compute.aws_instance.main[1]", Action: manifest.ActionCreate},
		{Address: "module.networking.aws_vpc.main", Action: manifest.ActionUpdate},
		{Address: "module.compute.aws_iam_role.ec2", Action: manifest.ActionReplace},
		{Address: "module.compute.data.aws_ami.x", Action: ""},
	}
	v := PlanGate(a, files, changes)
	// Actions (like the replace) are decided by syncPolicy rules, not here.
	if len(v) != 1 || v[0].Kind != ViolationPlanOutOfScope || v[0].Address != "module.networking.aws_vpc.main" {
		t.Fatalf("expected only the VPC to be out of scope, got %v", v)
	}
}

// A locals change inside an allowed file that ripples into protected
// resources is caught by the plan gate even though the diff gate passes.
func TestPlanGateCatchesIndirectChanges(t *testing.T) {
	files := repoFiles()
	a := app(t, `    rules:
      - files: ["environments/dev/main.tf"]
        blocks: ["local.*"]
`)
	changed := edit(files, "environments/dev/main.tf", `"dev"`, `"dev2"`)
	if v := DiffGate(a, files, changed, []string{"environments/dev/main.tf"}); len(v) != 0 {
		t.Fatalf("diff gate should pass for a local, got %v", v)
	}
	v := PlanGate(a, changed, []PlanChange{{Address: "module.networking.aws_vpc.main", Action: manifest.ActionUpdate}})
	if len(v) != 1 {
		t.Errorf("plan gate must block the protected resource, got %v", v)
	}
}

func TestPlanActionFromTofu(t *testing.T) {
	cases := map[string][]string{
		manifest.ActionReplace: {"delete", "create"},
		manifest.ActionCreate:  {"create"},
		manifest.ActionUpdate:  {"update"},
		manifest.ActionDelete:  {"delete"},
		"":                     {"no-op"},
	}
	for want, actions := range cases {
		if got := PlanActionFromTofu(actions); got != want {
			t.Errorf("%v -> %q, want %q", actions, got, want)
		}
	}
	if got := PlanActionFromTofu([]string{"create", "delete"}); got != manifest.ActionReplace {
		t.Errorf("create-before-destroy must be a replace, got %q", got)
	}
}

func terragruntRepo() map[string]string {
	return map[string]string{
		"root.hcl": `generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite"
  contents  = "provider \"aws\" {}"
}
`,
		"secret-manager/terragrunt.hcl": `include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../modules//secret"
}

inputs = {
  name = "servicio-1-db"
}
`,
		"lambdas/servicio-1/terragrunt.hcl": `include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "../../modules/lambda"
}

dependency "secret" {
  config_path = "../../secret-manager"
}

inputs = {
  secret_arn = dependency.secret.outputs.arn
}
`,
		"modules/secret/main.tf": `variable "name" {}

resource "aws_secretsmanager_secret" "this" {
  name = var.name
}

output "arn" {
  value = aws_secretsmanager_secret.this.arn
}
`,
		"modules/lambda/main.tf": `variable "secret_arn" {}

resource "aws_iam_role" "lambda" {
  name = "servicio-1"
}

resource "aws_lambda_function" "this" {
  role = aws_iam_role.lambda.arn
  environment {
    variables = { SECRET_ARN = var.secret_arn }
  }
}
`,
	}
}

func tgApp(t *testing.T, path, editScope string) *manifest.Application {
	t.Helper()
	m := "apiVersion: towercrane.io/v1alpha1\nkind: Application\nmetadata:\n  name: x\nspec:\n  source:\n    path: " + path + "\n  editScope:\n" + editScope
	a, err := manifest.Parse(".towercrane/x.yaml", []byte(m))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTerragruntUnits(t *testing.T) {
	files := terragruntRepo()

	dirs := ReachableDirs(files, "lambdas/servicio-1")
	if !dirs["lambdas/servicio-1"] || !dirs["modules/lambda"] || dirs["modules/secret"] {
		t.Errorf("lambda unit should reach itself and its module only, got %v", dirs)
	}
	if !ExtraFiles(files, "lambdas/servicio-1")["root.hcl"] {
		t.Error("root.hcl (find_in_parent_folders) must belong to the unit")
	}
	if !ReachableDirs(files, "secret-manager")["modules/secret"] {
		t.Error(`"../modules//secret" must resolve to modules/secret`)
	}

	// Changing the unit's inputs is a change to block "inputs".
	changed := map[string]string{}
	for k, v := range files {
		changed[k] = v
	}
	changed["secret-manager/terragrunt.hcl"] = strings.Replace(files["secret-manager/terragrunt.hcl"], "servicio-1-db", "servicio-2-db", 1)
	only := tgApp(t, "secret-manager", "    rules:\n      - files: [\"secret-manager/terragrunt.hcl\"]\n        blocks: [\"inputs\"]\n")
	if v := DiffGate(only, files, changed, []string{"secret-manager/terragrunt.hcl"}); len(v) != 0 {
		t.Errorf("inputs change should be allowed, got %v", v)
	}
	deps := tgApp(t, "secret-manager", "    rules:\n      - files: [\"secret-manager/terragrunt.hcl\"]\n        blocks: [\"dependency.*\"]\n")
	if v := DiffGate(deps, files, changed, []string{"secret-manager/terragrunt.hcl"}); len(v) != 1 || v[0].Block != "inputs" {
		t.Errorf("inputs change must be blocked when only dependency blocks are allowed, got %v", v)
	}

	// A root.hcl change belongs to every unit including it.
	if v := DiffGate(only, files, files, []string{"root.hcl"}); len(v) != 1 || v[0].Kind != ViolationFileNotInScope {
		t.Errorf("root.hcl change must be checked against the unit's scope, got %v", v)
	}

	// Plan addresses of a unit have no module prefix.
	res := Allowed(tgApp(t, "lambdas/servicio-1", "    rules:\n      - files: [\"modules/lambda/*.tf\"]\n        blocks: [\"resource.aws_lambda_function.*\"]\n"), files)
	if !res.Matches("aws_lambda_function.this") || res.Matches("aws_iam_role.lambda") {
		t.Errorf("got %v", res.Describe())
	}
	all := Allowed(tgApp(t, "lambdas/servicio-1", "    rules:\n      - files: [\"lambdas/servicio-1/terragrunt.hcl\"]\n        blocks: [\"inputs\"]\n        dependents: true\n"), files)
	if !all.Matches("aws_iam_role.lambda") {
		t.Error("inputs with dependents feed the whole module")
	}
}
