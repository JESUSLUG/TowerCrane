# TowerCrane
<img width="1024" height="572" alt="image" src="https://github.com/user-attachments/assets/846952e0-d940-43fb-943f-fc3892849f04" />

TowerCrane is an early, open-source and self-hosted project that aims to bring a GitOps workflow to infrastructure as code.

The idea is to keep your Terraform, OpenTofu or Terragrunt code as it is, and add a few YAML manifests that describe what TowerCrane should manage, where to apply it, in which order and when a person should say "yes". Those manifests can live next to the code (in `.towercrane/`) or in a configuration repository of their own. After a merge on the default branch, TowerCrane tries to plan only what the merge affects, checks the plan against that policy, and then applies it or waits for an approver.

It is a work in progress and still changing often, so expect rough edges.

## How it works (today)

1. **Declare a project** with a `project.yaml` (`kind: Project`): its code repositories and the cloud accounts it asks for.
2. **Describe the policy next to it**: Applications (what to apply, from which repository), Groups (where to apply it and in which order) and Jobs (what to bring up and tear down on a schedule).
3. **Connect the repositories** through the TowerCrane GitHub App. The project appears on its own; an admin authorizes the cloud accounts it asks for, once.
4. **Merge.** TowerCrane reads the default branches (polling every 3 minutes, or on **Refresh**) and starts a **rollout**. It plans the Applications the merge affects in dependency order, checks each plan against its policy, and then applies it, waits for a "yes" or rejects it.

```yaml
# .towercrane/vpc.yaml
apiVersion: towercrane.io/v1alpha1
kind: Application
metadata:
  name: vpc
spec:
  groups: [sandbox, prod]
  source:
    path: network/vpc            # a root module or a Terragrunt unit
    engine: opentofu             # opentofu (default) | terraform
  syncPolicy:
    apply: manual                # changes no rule matches wait for a "yes"
    rules:
      - when: { actions: [create] }
        apply: auto
  editScope:
    rules:
      - files: ["network/vpc/*.tf"]
        blocks: ["*"]
```

**Merging is the approval.** The intent is that what is merged on the default branch *is* the policy. Who can change the manifests is left to GitHub (branch protection, `CODEOWNERS`, required reviews), and TowerCrane tries to show on the project page whether that protection is in place. The one thing a manifest can't grant is cloud credentials: an admin authorizes each account.

The full reference is in [docs/MANIFESTS.md](./docs/MANIFESTS.md).

## What it does so far

These are the pieces that exist today. Most of them have only been tried in small test setups.

### Policy as code
- **Applications** (`kind: Application`): declarative YAML manifests with strict parsing. A typo makes the manifest invalid, so that it doesn't silently widen what TowerCrane may change.
- **`syncPolicy`**: per planned change (`create`, `update`, `delete`, `replace`, by resource address), first match wins: `auto`, `manual` or `reject`. Delete and replace are rejected unless a rule allows them.
- **`editScope`**: deny-by-default list of the files and HCL blocks (`resource.*`, `variable.*`, `locals`…) an Application may change. It is checked twice: against the git diff before planning, and against the real plan (`tofu show -json`) before applying, to try to catch changes that ripple through `locals`, `moved`/`removed` blocks or cloud drift.
- **First apply (onboarding)** always waits for a "yes".

### Projects and repositories
- **Projects** (`kind: Project`) group what TowerCrane works with: groups, services, Jobs, rollouts and cloud accounts. Connecting a repository with a `project.yaml` creates the project; deleting the file removes it.
- **Configuration and code can live apart**: manifests in one repository, code in others (`spec.source.repo`). Each operation records both commits.
- **Manifests folder**: where `project.yaml` lives (`.towercrane/` next to the code, or the root of a configuration-only repository).
- **Repositories** is only the inventory of what the GitHub App can read.

### Groups, promotion and multi-account
- **Groups** (`kind: Group`) set the cloud account of their Applications and their promotion order (`after: [sandbox]`).
- An Application listed in several groups runs as **one instance per group** (`vpc@sandbox`, `vpc@prod`), each with its own history, approvals and baseline.
- **`dependsOn`** orders Applications inside a group, or across groups by naming the instance (`vpc@general-us-east-1`).
- **Several cloud accounts per project.** A project asks for them in `project.yaml`, and can only use the ones an admin authorized.
- TowerCrane passes `TOWERCRANE_GROUP`, `TOWERCRANE_APPLICATION`, `TOWERCRANE_INSTANCE` and `TOWERCRANE_CLOUD_ACCOUNT` to the run, so shared code can keep one state per group.

### Rollouts and operations
- Every merge starts a rollout that respects `dependsOn` and group promotion. If a dependency fails or is rejected, its dependents are skipped.
- **It tries to plan only what a commit affects**: an Application whose code, modules, manifest or group didn't change since it last ended well shows as "unchanged".
- **Jobs** (`kind: Job`) bring an ephemeral Application up on a schedule (or with **Run now**) and destroy it later, through the same plan/apply/destroy: for example, an automation that runs and goes away, or a test environment during office hours.
- Confirming applies **exactly the saved plan**, and is refused if the branch moved on.
- Admins can reorder independent Applications and pause, resume or retry them.
- **Emergency mode** (admins only): plan, apply or destroy without the policy. It requires typing the Application's name and is recorded as EMERGENCY.
- Live logs, operation history and plan summaries with a colored diff.

### Web interface
- **Group diagrams** with the live rollout state (running, waiting for a "yes", failed), styled with `kind: Diagram` manifests (still an early experiment).
- **Application pages** with logs, operations and manifest tabs.
- **Resource map** of the Terraform code.
- **Team** page with roles, **Integrations** (GitHub App, cloud providers) and first-run **setup**.
- Views are built from the last commit TowerCrane read, and show which commit they reflect.

### Engines
- **OpenTofu** (default), **Terraform**, and **Terragrunt** for any `source.path` with a `terragrunt.hcl`.
- Override binary paths with `TOWERCRANE_TOFU_BIN`, `TOWERCRANE_TERRAFORM_BIN` and `TOWERCRANE_TERRAGRUNT_BIN`.

### Access and security
- Sign in with **email/password** or **GitHub OAuth**.
- Roles: **member** (view, plan), **approver** (+ confirm plans, run Jobs) and **admin** (+ repositories, cloud accounts and their authorization, destroy, emergency mode, users, integrations). Roles can come from the Team page, from a mapped GitHub team, or from `TOWERCRANE_ADMINS`.
- Cloud credentials and GitHub tokens are encrypted at rest (`TOWERCRANE_ENCRYPTION_KEY`).
- Weak or example `JWT_SECRET` values are refused at startup, and CORS only allows `FRONTEND_URL` (plus `TOWERCRANE_CORS_ORIGINS`).
- TowerCrane doesn't store application secrets. Infrastructure reads them from the cloud (e.g. AWS Secrets Manager), and `tofu` never inherits the server's own credentials.
- Sign-in with GitHub checks the OAuth `state` in the browser that started it, and repeated failed password sign-ins for an email are refused for 15 minutes.
- In Docker, the backend runs as an unprivileged user, the frontend is served by nginx with security headers (CSP, no framing), and every port listens only on `127.0.0.1`. Use `DB_SSLMODE=verify-full` for a remote database.

**Known limitation:** a plan or apply runs the repository's code (providers, `external` data sources, `local-exec`), and today it runs as the same OS user as the TowerCrane server. Whoever can merge to a connected repository could therefore read the server's files and environment (`JWT_SECRET`, the GitHub App key, the database password). Until runs are isolated (a separate user or container), only connect repositories whose merges you trust as much as access to the server itself, and keep `WORKSPACE_DIR` outside `/tmp`.

## Quick start (local)

**Requirements:** Go 1.26+, Node.js 20+, Docker, and `tofu` (and `terraform` / `terragrunt` if you use them) on the `PATH`.

### 1. Start PostgreSQL and Floci

```bash
docker compose up -d postgres floci
```

[Floci](https://floci.io) is a local AWS emulator on `http://localhost:4566`, which lets you try the workflow without a real cloud account.

### 2. Configure and run the backend

```bash
cd backend
cp .env.example .env
```

Fill in at least:

| Variable | Value |
|---|---|
| `JWT_SECRET` | `openssl rand -base64 48` (32+ characters, required) |
| `TOWERCRANE_ENCRYPTION_KEY` | `openssl rand -base64 32` |
| `TOWERCRANE_ADMINS` | your GitHub login or email (first admin) |
| `GITHUB_APP_*` | see [docs/GITHUB_APP_SETUP.md](./docs/GITHUB_APP_SETUP.md) |
| `GITHUB_CLIENT_*` (optional) | GitHub sign-in, see [docs/GITHUB_OAUTH_SETUP.md](./docs/GITHUB_OAUTH_SETUP.md) |

```bash
go run ./cmd
```

The API listens on `http://localhost:8080` (health check: `/api/v1/health`). On the first run, the logs print a one-time setup token to create the first admin at `http://localhost:5173/setup` (or set `TOWERCRANE_SETUP_TOKEN`).

### 3. Run the frontend

```bash
cd frontend
npm install
npm run dev
```

Open `http://localhost:5173`.

### 4. Connect a cloud account and a project

1. **Integrations → Cloud providers:** add Floci with account `000000000000`, keys `test` / `test`, region `us-east-1` and endpoint `http://localhost:4566`.
2. Add a `project.yaml` and the manifests to your repository (see [docs/MANIFESTS.md](./docs/MANIFESTS.md)).
3. **Repositories:** connect it (and its code repositories) through the GitHub App. The project appears in **Projects**.
4. On the project page, **Authorize** the cloud account it asks for, then **Refresh**.

The example repositories (one repository with everything, and configuration and code apart) are described in [docs/EXAMPLE_REPO_SETUP.md](./docs/EXAMPLE_REPO_SETUP.md).

### Troubleshooting

- `dial tcp 127.0.0.1:5432: connect: connection refused`: PostgreSQL isn't running. Start it with `docker compose up -d postgres`.
- `JWT_SECRET must be at least 32 characters`: generate one with `openssl rand -base64 48`.

## Project layout

```
backend/            Go + Gin API
  cmd/              entry point
  internal/api      routes, rollouts, poller, webhooks, policy checks
  internal/manifest manifests (Project, Application, Group, Job, Diagram)
  internal/schedule cron schedules for Jobs
  internal/iac      OpenTofu / Terraform / Terragrunt executor
  internal/tfmap    HCL block graph used by editScope and the resource map
  internal/git      clones, workspaces and diffs
  internal/cloud    cloud account credentials (AWS, Floci)
  internal/secrets  encryption at rest
frontend/           React + TypeScript + Tailwind (Vite), React Flow diagrams
docs/               policy reference and setup guides
```

## Tech stack

- **Backend:** Go, Gin, PostgreSQL
- **Frontend:** React, TypeScript, Tailwind CSS, Zustand, React Flow + dagre
- **IaC engines:** OpenTofu, Terraform, Terragrunt
- **Integrations:** GitHub App (repositories, branch protection), GitHub OAuth, AWS / Floci

## Project status

**Pre-alpha, and an ongoing learning project.** The basic loop (manifests → rollouts → policy checks → plan/apply with approvals) runs in local tests, but it hasn't been used in production, and features and manifest fields will likely change (`apiVersion: towercrane.io/v1alpha1`). Feedback and ideas are very welcome.

**Webhooks are paused for now.** TowerCrane notices new commits by polling GitHub every 3 minutes (`TOWERCRANE_POLL_INTERVAL`) or when someone presses **Refresh**, so it doesn't need a public URL. The webhook endpoint is still in the code but isn't used or tested at the moment.

Some ideas being explored:
- `spec.source.revision`: pinning a branch or tag per Application, to promote by editing the configuration.
- Guides to deploy it outside a laptop (Docker, Kubernetes).

## Documentation

- [Manifests reference (Project, Application, Group, Job, Diagram, roles)](./docs/MANIFESTS.md)
- [GitHub App setup](./docs/GITHUB_APP_SETUP.md)
- [GitHub OAuth setup](./docs/GITHUB_OAUTH_SETUP.md)
- [Example repository](./docs/EXAMPLE_REPO_SETUP.md)
- [Architecture](./docs/ARCHITECTURE.md)
- [Documentation index](./docs/INDEX.md)

## Philosophy

Infrastructure as Code deserves good tooling and workflows, and TowerCrane is one attempt to contribute to that.

The hope is that teams can keep the tools and code they already know, and get a place where the policy is versioned and reviewed, where every apply is planned, checked and traceable, and where people only decide what really needs a person.

### Developed with OpenTofu and Floci

TowerCrane is being developed and tested with [OpenTofu](https://opentofu.org) and [Floci](https://floci.io), a local AWS emulator, so the workflow (plans, approvals, rollouts and applies) can be tried on a laptop without touching a real cloud account.

## License

MIT License
