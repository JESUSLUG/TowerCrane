# Connecting GitHub and AWS (local testing with Floci)

This guide takes a fresh TowerCrane to the point where it plans and applies the
TowerCrane-Example repository against [Floci](https://floci.io), a local AWS emulator.

## 1. First run: create the admin

1. Start Postgres and the backend (`cd backend && go run ./cmd`).
2. The backend prints a **one-time setup token**:
   ```
    TowerCrane has no accounts yet. Create the first admin at
      http://localhost:5173/setup
    using this one-time setup token:
      s6Smesq56_LrM2NX3rBweucD
   ```
3. Open that URL, paste the token and create the admin account (email + password, at least 12 characters).

From then on, there is no open registration. People join by:
- signing in with GitHub as a member of an organization where the TowerCrane GitHub App is installed (or as
  the owner of a personal account it is installed on), or
- accepting an invitation from **Team → Invitations**.

## 2. Webhooks (paused, skip this step)

Webhooks are paused for now: TowerCrane notices new commits by polling GitHub every 3 minutes
(`TOWERCRANE_POLL_INTERVAL`) or when someone presses **Refresh**, so it needs no public URL and no relay.
Leave the GitHub App's webhook inactive and `GITHUB_WEBHOOK_SECRET` empty. The endpoint
(`/api/v1/webhooks/github`) is still in the code but isn't used or tested at the moment.

## 3. Create the GitHub App

GitHub → **Settings → Developer settings → GitHub Apps → New GitHub App**. For an organization, create it under
the organization's own settings instead.

| Field | Value |
|---|---|
| Homepage URL | `http://localhost:5173` |
| Setup URL | `http://localhost:5173/integrations/github/callback` (also tick "Redirect on update") |
| Webhook | untick **Active** (webhooks are paused, see step 2) |

**Repository permissions:** Contents: *Read*; Metadata: *Read*; Commit statuses: *Read and write*; Pull requests: *Read*.
Optional: Administration: *Read*, so the repository page can also check classic branch protection (pull requests and Code Owners reviews required). Rulesets are checked without it.
**Organization permissions:** Members: *Read*. TowerCrane uses this to check whether someone signing in belongs to the org.
**Subscribe to events:** Push, Pull request.

After creating it, note the **App ID** and the URL slug (`github.com/apps/<slug>`), then **Generate a private
key**. Put the key in `GITHUB_APP_PRIVATE_KEY` as a single line with `\n` for line breaks, and delete the
downloaded `.pem`:

```bash
awk 'NF {printf "%s\\n", $0}' ~/Downloads/your-app.*.private-key.pem
```

In the cloud, keep it in a secret store (e.g. AWS Secrets Manager) and expose it to TowerCrane as that
environment variable. `GITHUB_APP_PRIVATE_KEY_PATH` also works if the key is mounted as a file.

`backend/.env`:

```bash
GITHUB_APP_ID=123456
GITHUB_APP_SLUG=towercrane-yourname
GITHUB_APP_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----"
TOWERCRANE_ENCRYPTION_KEY=<openssl rand -base64 32>
```

Restart the backend.

> Sign-in with GitHub still uses the OAuth app (`GITHUB_CLIENT_ID` / `GITHUB_CLIENT_SECRET`, callback
> `http://localhost:8080/api/v1/auth/callback`).

## 4. Connect an organization or specific repositories

1. In TowerCrane go to **Integrations → GitHub → Connect organization or repos**.
2. On GitHub, pick the org or account. Then choose **All repositories** or **Only select repositories**.
   Selecting repositories is how you connect specific repos.
3. GitHub sends you back to TowerCrane, which registers the installation.
4. **Repositories → Connect new** now lists every repo the app can see. Connect your project's repositories:
   the one with `project.yaml` (configuration) and its code repositories. The project appears in **Projects**
   on its own.

To change which repos the app can see later, use **Change repos** next to the installation, or on GitHub:
**Settings → Applications → Installed GitHub Apps → your app → Configure → Repository access**. Only the
installation's owner can change it (a CLI token can't).

Teams: TowerCrane gives roles to GitHub teams under **Team → GitHub team mappings** (for example
`acme/infra-admins → admin`). Repository access is chosen per installation on GitHub, not per team.

## 5. Require the policy check on PRs

In the repository: **Settings → Branches → Branch protection rule** for the default branch →
**Require status checks to pass before merging** → select `towercrane/policy`. The check only shows up in that list
after TowerCrane has reported it once, so open any PR first.

Results of the check:

| Status | Meaning |
|---|---|
| ✅ success | No policy problems. If the PR changes manifests, the description says how many: merging applies them (merging is the approval) |
| ❌ failure | The PR changes code outside an Application's `editScope`, or a manifest is invalid |

Only declared Applications are checked. The PR check is off by default; enable it with `TOWERCRANE_PR_CHECKS=true`.

## 6. Add the Floci AWS account

1. Start Floci from the TowerCrane repo: `docker compose up -d floci`. It listens on `http://localhost:4566`.
   EC2 instances and RDS databases run as real containers through the Docker socket.
2. Go to **Integrations → Cloud providers → Add AWS account**:
   - Name: `floci-dev`
   - Account ID: `000000000000` (Floci's default account)
   - Access key / secret: `test` / `test` (Floci accepts any non-empty value)
   - Region: `us-east-1`
   - Endpoint URL: `http://localhost:4566`, or `http://floci:4566` if the backend runs inside the same compose
3. **Save and verify**. TowerCrane calls STS `GetCallerIdentity` on Floci and checks the account ID.
4. Ask for it in the project's `project.yaml` (`cloudAccounts: [floci-dev]`) and, on the project page, click
   **Authorize floci-dev** (admins). For a project created by hand, pick it in the project's **Settings**.

tofu then runs with `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` and `AWS_ENDPOINT_URL` from that
account. It never inherits the server's own AWS, database, GitHub or TowerCrane variables.

### tofu

The backend runs `tofu` (OpenTofu) and `git`. The backend Docker image includes both. When running the backend
with `go run`, install OpenTofu locally (https://opentofu.org/docs/intro/install/).
