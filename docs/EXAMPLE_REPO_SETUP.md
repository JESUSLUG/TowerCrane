# Example repositories

Two small setups are used to try TowerCrane. Both are private test repositories; they are described here
so the same layouts can be reproduced. The manifests reference is [MANIFESTS.md](./MANIFESTS.md).

## 1. Everything in one repository: `TowerCrane-Example`

Code and manifests live together; the manifests are in `.towercrane/`. It is a project created by hand
(it has no `project.yaml`).

```
root.hcl                                  shared Terragrunt config: local state + AWS provider
modules/                                  vpc, ecr, secret, lambda, job-lambda
us-east-1/
  vpc/                                    network shared by every environment
  sandbox/{ecr,secret-manager,servicio-1}
  prod/{ecr,secret-manager,servicio-1}
  prod/lambda-job                         ephemeral automation run by a Job
.towercrane/
  groups/                                 general-us-east-1, sandbox-us-east-1, prod-us-east-1
  general-us-east-1/vpc.yaml
  sandbox-us-east-1/*.yaml                ecr, secrets, servicio-1
  prod-us-east-1/*.yaml                   ecr, secrets, servicio-1, lambda-job
  jobs/lambda-job.yaml
  diagrams/                               how each group is drawn
```

What it shows:
- Groups per environment with the same Application names (`ecr@sandbox-us-east-1`, `ecr@prod-us-east-1`).
- `syncPolicy` rules: adding a secret applies on its own, deleting one is rejected, code updates of the
  Lambda apply on their own, anything else waits for a "yes".
- A Job (`lambda-job`) that creates a Lambda on weekdays, runs it once and destroys it an hour later.

## 2. Configuration and code apart: `towercrane-platform`, `-infra`, `-apps`

| Repository | Holds |
|---|---|
| `towercrane-platform` | Only configuration, at the root: `project.yaml`, groups, Applications, Jobs, and `CODEOWNERS` (`* @owner`) |
| `towercrane-infra` | Shared infrastructure: the VPC (`us-east-1/vpc`) |
| `towercrane-apps` | The services: ECR, secrets and Lambdas per environment, and the Job's Lambda |

```yaml
# towercrane-platform/project.yaml
apiVersion: towercrane.io/v1alpha1
kind: Project
metadata:
  name: platform
spec:
  sources:
    - JESUSLUG/towercrane-infra
    - JESUSLUG/towercrane-apps
  cloudAccounts: [aws-carlitos]
```

```yaml
# towercrane-platform/sandbox-us-east-1/servicio-1.yaml (excerpt)
spec:
  groups: [sandbox-us-east-1]
  source:
    repo: JESUSLUG/towercrane-apps
    path: us-east-1/sandbox/servicio-1
  syncPolicy:
    dependsOn: [ecr, secrets]
```

What it shows:
- Connecting the three repositories creates the `platform` project from `project.yaml`; an admin then
  authorizes the cloud account it requests.
- A commit in `towercrane-apps` only plans the Applications whose code it touches; a commit in
  `towercrane-platform` plans the ones whose manifest or group changed.
- State in S3 (`towercrane-tfstate-<account>`), one key per repository and unit; Terragrunt creates the
  bucket on first use. Repositories don't read each other's state: shared pieces are found by tags.

## Trying it locally

[Floci](https://floci.io) emulates AWS on `http://localhost:4566` (`docker compose up -d floci` from the
TowerCrane repository). Add it in **Integrations → Cloud providers** (account `000000000000`, keys
`test` / `test`, region `us-east-1`, endpoint `http://localhost:4566`) and point a Group's
`cloudAccount` to it.

To run a unit by hand, outside TowerCrane:

```bash
cd us-east-1/sandbox/servicio-1
terragrunt plan     # needs its dependencies (ecr, secret-manager) applied first
```
