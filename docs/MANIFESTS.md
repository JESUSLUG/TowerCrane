# TowerCrane manifests reference

TowerCrane decides what it manages, and how, from **manifests**: declarative
YAML files (Applications, Groups, Jobs, Diagrams) in the project's **manifests folder**. By default
that is `.towercrane/` at the root of the repository, next to the code; a repository that only
holds configuration can keep them at its root instead (see [Manifests folder](#manifests-folder)).
The Terraform/OpenTofu code itself carries no TowerCrane annotations. Paths below use the default
`.towercrane/`.

## Application manifest

```yaml
apiVersion: towercrane.io/v1alpha1
kind: Application
metadata:
  name: example-dev                 # lowercase letters, digits, dashes
spec:
  groups: [sandbox, prod]           # where it is applied (Groups, below); or group: sandbox
  source:
    path: environments/dev          # a root module, or a Terragrunt unit (has terragrunt.hcl)
    engine: opentofu                # opentofu (default) | terraform
  # Without groups, the cloud account is named here instead:
  # destination:
  #   cloudAccount: floci-dev
  syncPolicy:
    apply: manual                   # default for changes no rule matches: auto | manual
    dependsOn: [network]            # roll out after these Applications (same repo)
    rules:                          # per planned change, first match wins
      - when:
          actions: [create]
          resources: ["aws_secretsmanager_secret.*"]
        apply: auto
      - when:
          actions: [delete]
        apply: reject
  editScope:
    rules:
      - files: ["environments/dev/variables.tf"]
        blocks: ["variable.instance_count", "variable.instance_type"]
        dependents: true
      - files: ["modules/compute/*.tf"]
        blocks: ["*"]
  variables:                        # non-secret values, passed as TF_VAR_<name>
    environment: dev
  display:
    showSource: true                # show this Application's HCL on the map
```

Parsing is strict. Unknown or misspelled fields, a wrong `apiVersion` or `kind`, or invalid
values make the manifest invalid, so a typo can never silently widen what TowerCrane may change.
Each Application name must be unique.

## What an Application may change: `editScope`

`editScope` is deny-by-default. Without it, nothing may change. `editScope.all: true` allows
any change and is meant for sandboxes. Otherwise each rule allows:

- **`files`:** repository-relative globs. `*` matches within a directory and `**` matches
  across directories.
- **`blocks`:** which HCL blocks of those files may change. The block addresses are:
  `resource.<type>.<name>`, `data.<type>.<name>`, `module.<name>`, `variable.<name>`,
  `output.<name>`, `local.<name>` (each local separately), `provider.<name>`, `terraform`,
  `moved`, `removed`, `import`. Use `*` as a wildcard. Non-HCL files such as `.tfvars` need
  `blocks: ["*"]`.
- **`dependents: true`:** whatever uses the allowed blocks may also change in the plan,
  directly or through locals, variables, outputs and module calls.

TowerCrane checks every change twice, and both checks must pass:

1. **Against the git diff, before planning.** Every changed file of the Application must match
   a rule. Inside it, every added, changed or removed block must be allowed. Comments and
   whitespace outside blocks don't count. Docs (`*.md`) and other Applications' files are ignored.
2. **Against the real plan (`tofu show -json`), before applying.** Every resource the plan
   changes must belong to an allowed block, or be a dependent of one if `dependents: true`.

The plan check catches what the diff can't see: a `locals` change that ripples into protected
resources, `moved {}` and `removed {}` blocks, or drift in the cloud.

For **Terragrunt** units, the unit's `terragrunt.hcl` blocks are addressed as `inputs`,
`dependency.<name>`, `include.<name>`, `terraform`, `locals`… Its `terraform { source }` module
is the root module (plan addresses have no `module.` prefix), and an included `root.hcl` belongs to
every unit that includes it.

## How a plan is applied: `syncPolicy`

After the plan, every planned change is matched against `syncPolicy.rules`. The first matching rule
decides `auto`, `manual` or `reject`. A change no rule matches gets `syncPolicy.apply`, which is
`manual` when omitted. A built-in last rule **rejects delete and replace** unless one of the
Application's rules matches them first. The plan as a whole gets the strictest decision:

| Decision | Effect |
|---|---|
| `auto` | Applied without asking (after a merge) |
| `manual` | Waits for an approver's "yes" |
| `reject` | Never applied, not even with a "yes"; Applications that depend on it are skipped |

The **first apply** of an Application ("onboarding") has nothing to compare against. The edit
scope is skipped, the plan guard still applies, and it always waits for an approver's "yes",
even with `apply: auto`.

## Merging is the approval

TowerCrane follows GitOps strictly: **what is merged on the default branch is the policy**. There is
no second approval of manifests inside TowerCrane. Who may change `.towercrane/` (Applications,
Groups, Diagrams) is decided where the code lives, in GitHub:

- protect the default branch and require pull requests;
- add a `CODEOWNERS` rule for the manifests, e.g. `/.towercrane/ @your-org/platform` (or
  `* @your-org/platform` when the whole repository is configuration);
- enable "Require review from Code Owners", so that rule is enforced and not just suggested.

The repository page shows whether this is in place ("protection" panel) and warns when anyone who can
push could change the policy. For complete separation, keep the manifests in a repository the
developers can't write to (planned: a configuration repository pointing to code repositories).

What TowerCrane still decides at run time: the approver's "yes" before applying (`apply: manual` and
every first apply), the `reject` rules, the `editScope` of code changes and the admins' emergency mode.

## Applying

TowerCrane polls GitHub (every 3 minutes by default; **Refresh** checks right away). It plans
every Application after each new commit on the default branch. Then:

| Plan result | What happens |
|---|---|
| No changes | Nothing |
| Blocked by the policy | The plan shows why |
| Every change `auto`, within the edit scope, not the first apply | Applied automatically |
| Some change `reject` | Cancelled; its dependents are skipped |
| Otherwise | Waits in "Waiting for your yes" until an **approver or admin** confirms |

Confirming applies **exactly the saved plan**. It is refused if the branch moved on since the
plan, and the policy is re-checked first.

**Emergency mode (admins only)** runs plan, apply or destroy on an Application without the
policy. It requires typing the Application name, is recorded as EMERGENCY, and doesn't count as
the policy's baseline. A normal destroy needs `editScope.all: true` and a rule that doesn't reject
delete.

## Projects and code repositories

A **project** is where TowerCrane works: groups, Applications, Jobs, rollouts and cloud accounts. It reads
its manifests from a **configuration repository** and runs code from it and from its **code
repositories**. Connected repositories (Repositories) are only the inventory projects pick from.

A project is a manifest too. The configuration repository declares it:

```yaml
# project.yaml
apiVersion: towercrane.io/v1alpha1
kind: Project
metadata:
  name: platform
spec:
  description: Infrastructure and services
  sources:                          # code repositories (owner/name)
    - acme/infra
    - acme/apps
  cloudAccounts: [aws-prod]         # requested; an admin authorizes each one
```

- **Connecting** a repository with a Project manifest creates the project; every new commit of it
  updates it (name, code repositories), and deleting the file removes the project (nothing in the
  cloud is touched). Its name, folder and code repositories change only through a pull request.
- **The manifests folder is where the file lives**: `project.yaml` at the root means the whole
  repository is configuration; `.towercrane/project.yaml` means `.towercrane/`. A repository has at most
  one Project manifest, at its root, in `.towercrane/` or in any non-hidden folder.
- **Cloud accounts are requested, never granted, by the manifest.** Credentials are TowerCrane's own
  secrets: an admin authorizes each requested account once on the project's page ("Authorize"). An
  Application can only use an account that is both requested and authorized; otherwise it fails with
  which of the two is missing.
- **Code repositories must be connected.** The project page lists the ones the manifest names that
  aren't, and they are picked up as soon as they are connected.
- Repositories without a Project manifest (like single-repository examples) can still be made a project
  by hand (Projects → Create by hand), with the same settings in the UI.

The code of an Application can live in another repository than its manifest:

```yaml
spec:
  source:
    repo: acme/infra-services        # a code repository of the project
    path: us-east-1/prod/servicio-1
```

- The repository must be one of the project's code repositories (an admin chooses them in the project's
  settings); otherwise the Application fails before running anything.
- Each operation records two commits: the manifests' and the code's. The policy (editScope, the diff
  since the last apply) looks at the code's commits, and a confirmed plan is refused if either moved.
- TowerCrane watches every repository of a project: a commit in the configuration plans the Applications
  whose manifest or group changed; a commit in a code repository plans the Applications whose code it
  touches.
- Terragrunt `dependency` blocks only reach units of the same repository. Across repositories, share
  data the cloud's way: data sources by tags (recommended), SSM parameters or remote state.
- With Terragrunt, TowerCrane runs with backend bootstrap on, so a remote state bucket (e.g. S3) is
  created the first time a unit uses it.

For complete separation, keep the configuration repository writable only by the platform team.

### Manifests folder

Each project says where its manifests live in the configuration repository: where its `project.yaml` is,
or, for a project created by hand, its **Manifests folder** setting:

| Folder | When | What counts as a manifest |
|---|---|---|
| `.towercrane` (default) | Manifests next to the code in the same repository | Every `.yaml`/`.yml` under `.towercrane/` (an invalid one is an error) |
| `/` (repository root) | A repository that only holds configuration | Every `.yaml`/`.yml` outside hidden folders whose `apiVersion` is `towercrane.io/v1alpha1`; other YAML (CI workflows, docker-compose…) is ignored |
| Any other folder (`config`, `deploy/tc`…) | Your own convention | Like `.towercrane` |

A configuration-only repository at the root looks like:

```
groups/sandbox-us-east-1.yaml
sandbox-us-east-1/servicio-1.yaml
jobs/lambda-job.yaml
.github/CODEOWNERS          * @your-org/platform
```

The protection check (project page) verifies that `CODEOWNERS` covers the configured folder, and every
subfolder of it.

## Groups and cloud accounts

A **Group** says where Applications are applied: its cloud account, and the groups it comes after.
Group manifests live in `.towercrane/` too (any file, `.towercrane/groups/` by convention):

```yaml
apiVersion: towercrane.io/v1alpha1
kind: Group
metadata:
  name: prod
spec:
  cloudAccount: aws-prod            # requested by the project and authorized by an admin
  after: [sandbox]                  # promotion: prod starts once sandbox applied cleanly
  description: Production
```

An Application lists its groups and TowerCrane runs **one instance per group**, named
`<application>@<group>` (`servicio-1@sandbox`, `servicio-1@prod`). Every instance has its own
history, approvals and baseline, and runs with its group's cloud account. `dependsOn` is resolved
inside the same group: `servicio-1@prod` waits for `vpc@prod`. To depend on an Application of
another group, name its instance: `dependsOn: [vpc@general-us-east-1, ecr]` (here `ecr` is the
one of the same group). An Application without groups can be a dependency of any group.

The same name can be used by different manifests in different groups, e.g. `.towercrane/sandbox/ecr.yaml`
(`groups: [sandbox]`, `path: us-east-1/sandbox/ecr`) and `.towercrane/prod/ecr.yaml`; only the same
name twice in one group is a duplicate.

This works for both layouts:

- **Shared code**: one `vpc/`, `lambda/`... for every environment, listed in several groups. Only
  the cloud account (and the state) changes.
- **One directory per environment**: `sandbox/vpc`, `prod/vpc`..., each Application in its own group.

**Cloud accounts per project.** A project asks for cloud accounts in its `project.yaml`
(`spec.cloudAccounts`) and an admin authorizes each one on the project's page (for a project created by
hand, in its Settings). Manifests name them exactly; an instance whose account isn't requested or isn't
authorized fails before running anything, so a project can't send its code to an account it wasn't
given. An Application without groups and without `destination.cloudAccount` uses the project's
account if exactly one is authorized.

**State per group.** When it runs an instance, TowerCrane sets `TOWERCRANE_GROUP`,
`TOWERCRANE_APPLICATION`, `TOWERCRANE_INSTANCE` and `TOWERCRANE_CLOUD_ACCOUNT`. Shared code applied in
several groups must keep one state per group, e.g. in Terragrunt:

```hcl
remote_state {
  config = {
    key = "${get_env("TOWERCRANE_GROUP", "default")}/${path_relative_to_include()}/terraform.tfstate"
  }
}
```

**Promotion.** In a rollout, every instance of a group waits for every instance of the groups in
its `after`. If one of them fails or is rejected, the later group is skipped.

**Groups are policy.** Changing a group (e.g. its cloud account) changes where every one of its
Applications is applied: protect `.towercrane/groups/` like the rest of `.towercrane/`.

## Diagrams

The first thing a group's page shows is its **diagram**, drawn from TowerCrane's own data (not by
parsing the Terraform code): the group's Applications, their `dependsOn` as solid arrows (the real
apply order) and, live, the latest rollout. A running Application shows a spinning clock, and one
waiting for a "yes" shows a speech bubble. Clicking an Application opens its page.

A `kind: Diagram` manifest styles it:

```yaml
apiVersion: towercrane.io/v1alpha1
kind: Diagram
metadata:
  name: sandbox
spec:
  group: sandbox                    # whose Applications it draws ("" = without group)
  sections:
    - name: Foundation
      color: blue
      apps: [vpc, ecr]
  nodes:
    vpc: {color: green, label: VPC}
    ecr: {color: red}
  links:                            # dashed, visual only: never changes the apply order
    - {from: secrets, to: servicio-1, label: reads the secret}
```

Colors: red, orange, yellow, green, teal, blue, purple, pink, gray. Names that aren't Applications
of the group are reported as warnings and ignored.

A Diagram is TowerCrane YAML: what is merged is what is drawn, and who may change it is up to the
repository's protection in GitHub, like the rest of `.towercrane/`.

## Jobs

A **Job** keeps an ephemeral Application up only when it's needed: TowerCrane applies it at scheduled
times (or on demand) and destroys it later, always through the usual plan/apply/destroy Operations
(logs, history, policy checks). For anything that should exist for a while and then go away: an EC2
that runs an automation, a test environment during office hours, a temporary database for a report.

```yaml
apiVersion: towercrane.io/v1alpha1
kind: Job
metadata:
  name: lambda-job
spec:
  application: lambda-job
  group: prod-us-east-1
  timezone: America/Mexico_City     # IANA zone (default UTC)
  apply:
    schedule: "0 9 * * 1-5"         # cron: minute hour day-of-month month day-of-week
  destroy:
    after: 1h                       # or schedule: "0 18 * * 1-5"
  manual: true                      # Run now / Destroy now (default true)
```

- The Application must declare `spec.lifecycle: job`. Merges never apply it (rollouts skip it), only
  its Job may destroy it, and no other Application may depend on it.
- The merged Job is the authorization: its runs need no "yes". Applies are still checked against the
  Application's policy (editScope, reject rules).
- Whether it is up comes from its latest successful apply or destroy, whoever ran it.
- Times fire once. If TowerCrane was down: a late apply (more than 10 minutes) is skipped and recorded
  as missed; a late destroy always runs ("catch-up"), so nothing stays up longer than intended.
- One run at a time per Job. "Run now" and "Destroy now" need an approver or admin.

## Rollouts and `dependsOn`

Every merge starts a **rollout**: all Applications of the commit, in an order where each one comes
after its `dependsOn`. Only the ones the commit can affect are planned: an Application is compared
with the last commit where it ended well (applied, no changes or unchanged) and planned when any of its
files changed since then (its directories, modules, Terragrunt includes such as `root.hcl`, its
manifest or its Group's), when it never ended well, or when an Application it depends on is planned.
The rest show as **unchanged** (they count as done for their dependents); "Plan" on one plans it anyway,
and a forced Refresh plans everything. An Application starts only once its dependencies were applied or had no
changes, so its plan already sees what they created. If a dependency fails, is rejected or
discarded, its dependents are skipped. Admins can reorder Applications that don't depend on each
other, pause, resume or retry them; `dependsOn` itself can only be bypassed in emergency mode.
Cross-repository `dependsOn` is planned (issue #1).

## Engines

`spec.source.engine` selects **OpenTofu** (default) or **Terraform**. A `source.path` with a
`terragrunt.hcl` runs through **Terragrunt** driving that engine. The server needs the binaries;
`TOWERCRANE_TOFU_BIN`, `TOWERCRANE_TERRAFORM_BIN` and `TOWERCRANE_TERRAGRUNT_BIN` override their
paths.

## Variables and secrets

TowerCrane does not store application variables or secrets. It orchestrates **what** is applied, **in which
order** and **whether it needs a "yes"**. Infrastructure that needs a secret reads it from the cloud itself,
for example with a `data "aws_secretsmanager_secret_version"` lookup, and the secret is created in AWS by
whoever owns it. `spec.variables` in a manifest can pass plain, non-secret values (as `TF_VAR_<name>`). tofu
runs with the Application's cloud account credentials and never inherits the server's own.

## Roles

| Role | Can |
|---|---|
| member | View everything, run plans |
| approver | Member + confirm plans (the "yes"), Run now / Destroy now on Jobs |
| admin | Approver + connect repositories, authorize cloud accounts for projects, destroy, emergency mode, manage users and integrations |

A user's effective role is the **highest** of:

1. the role assigned in TowerCrane (Team page);
2. the role of a mapped GitHub team (`org/team-slug`);
3. `TOWERCRANE_ADMINS` in the server config (GitHub logins or emails).

The last admin can't be demoted.
