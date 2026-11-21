# TowerCrane Architecture - Phase 4 Update

> **Note:** this document describes the early design and is partly outdated (for example, the data
> model). For how TowerCrane works today (projects, manifests, rollouts, Jobs), see
> [MANIFESTS.md](./MANIFESTS.md) and the [README](../README.md).


## 🏗 System Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                        TowerCrane MVP                          │
└────────────────────────────────────────────────────────────────┘

FRONTEND LAYER
┌─────────────────────────────────────────────────────────────┐
│  React Application (TypeScript + Tailwind)                  │
│                                                             │
│  ┌─────────────┬──────────────┬─────────────────────────┐   │
│  │ Login Page  │ Dashboard    │ Repositories Page       │   │
│  │ OAuth2 Flow │ User Info    │ Connect/Disconnect     │   │
│  │             │ Quick Stats  │ View Branches          │   │
│  └─────────────┴──────────────┴─────────────────────────┘   │
│                                                             │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ Operations Page (NEW - Phase 4)                      │   │
│  │  ├─ Control Panel (Plan, Apply, Destroy)           │   │
│  │  ├─ Environment Selector                            │   │
│  │  ├─ Working Directory Input                         │   │
│  │  └─ Operations History with Details                │   │
│  └──────────────────────────────────────────────────────┘   │
│                                                             │
│  State Management (Zustand)                               │
│  ├─ authStore (user, token)                              │
│  ├─ repositoryStore (repos, branches, files)             │
│  └─ operationStore (NEW - operations, status)            │
│                                                             │
│  API Clients (Axios)                                      │
│  ├─ authAPI                                              │
│  ├─ repositoriesAPI                                      │
│  └─ operationsAPI (NEW)                                 │
│                                                             │
│  Hooks                                                    │
│  ├─ useAuth()                                            │
│  ├─ useRepositories()                                    │
│  └─ useOperations() (NEW)                               │
└─────────────────────────────────────────────────────────────┘
                              ↓ HTTPS/WebSocket
                        (API Calls & Polling)
┌─────────────────────────────────────────────────────────────┐
│  Backend API (Go + Gin)                                     │
│                                                             │
│  ┌────────────────┬──────────────────┬──────────────────┐   │
│  │ Auth Handler   │ Repository       │ Operations       │   │
│  │                │ Handler          │ Handler (NEW)    │   │
│  │ ├─ Login       │                  │                  │   │
│  │ ├─ Callback    │ ├─ List          │ ├─ ExecutePlan  │   │
│  │ ├─ Logout      │ ├─ Connect       │ ├─ ExecuteApply │   │
│  │ └─ GetUser     │ ├─ GetBranches   │ ├─ ExecuteDestroy│  │
│  │                │ └─ SyncData      │ ├─ GetOperation │   │
│  │                │                  │ └─ ListOperations│  │
│  └────────────────┴──────────────────┴──────────────────┘   │
│                                                             │
│  Middleware                                               │
│  ├─ CORS                                                  │
│  ├─ AuthMiddleware                                        │
│  └─ Error Handling                                        │
│                                                             │
│  Database Layer (db package)                              │
│  ├─ UserStore                                             │
│  ├─ RepositoryStore                                       │
│  └─ OperationStore (NEW)                                 │
│                                                             │
│  IaC Engine (NEW - Phase 4)                              │
│  └─ Executor                                              │
│     ├─ Plan()                                             │
│     ├─ Apply()                                            │
│     └─ Destroy()                                          │
└─────────────────────────────────────────────────────────────┘
                              ↓ SQL Queries
┌─────────────────────────────────────────────────────────────┐
│  PostgreSQL Database                                        │
│                                                             │
│  Tables:                                                  │
│  ├─ users (id, github_id, login, email, avatar_url)      │
│  ├─ repositories (id, user_id, github_id, clone_url...)   │
│  ├─ repository_branches (id, repo_id, name, sha)          │
│  ├─ repository_files (id, repo_id, path, content)         │
│  └─ operations (NEW)                                      │
│     ├─ id, repository_id, user_id                         │
│     ├─ type, status, environment                          │
│     ├─ working_dir, plan, log, error                      │
│     └─ created_at, started_at, completed_at               │
│                                                             │
│  Indexes:                                                 │
│  ├─ operations_repository_id                              │
│  ├─ operations_user_id                                    │
│  ├─ operations_status                                     │
│  └─ operations_created_at                                 │
└─────────────────────────────────────────────────────────────┘
                              ↓ Execute IaC
┌─────────────────────────────────────────────────────────────┐
│  Infrastructure as Code                                     │
│                                                             │
│  Connected Repositories                                   │
│  └─ User's Git Repos (GitHub)                             │
│     ├─ Terraform/OpenTofu Code                            │
│     ├─ environments/                                      │
│     │  ├─ dev/                                            │
│     │  │  ├─ main.tf                                      │
│     │  │  ├─ variables.tf                                 │
│     │  │  └─ terraform.tfvars                             │
│     │  └─ prod/                                           │
│     └─ modules/                                           │
│        ├─ networking/                                     │
│        ├─ compute/                                        │
│        └─ database/                                       │
│                                                             │
│  IaC Tools                                                │
│  └─ OpenTofu/Terraform                                    │
│     ├─ Init State                                         │
│     ├─ Plan Changes                                       │
│     ├─ Apply Changes                                      │
│     └─ Destroy Resources                                  │
└─────────────────────────────────────────────────────────────┘
                              ↓ Manage
┌─────────────────────────────────────────────────────────────┐
│  AWS / Cloud Provider (or Floci for testing)           │
│                                                             │
│  Resources Managed by IaC:                                │
│  ├─ Networking (VPC, Subnets, Security Groups)            │
│  ├─ Compute (EC2, Auto Scaling)                           │
│  ├─ Storage (S3, EBS)                                     │
│  ├─ Databases (RDS)                                       │
│  └─ Other Services                                        │
└─────────────────────────────────────────────────────────────┘

External Services
├─ GitHub OAuth
├─ GitHub API (for repo management)
├─ Git (clone/pull)
└─ Cloud Provider APIs (AWS, GCP, Azure)
```

## 🔄 Request Flow - Execute Plan

```
User clicks "Plan" in OperationsPage
          ↓
Frontend calls operationAPI.executePlan()
          ↓
POST /api/v1/repositories/:repo_id/operations/plan
  Headers: Authorization: Bearer {jwt}
  Body: { repository_id, environment, working_dir }
          ↓
Backend OperationHandler.ExecutePlan()
  ├─ Validate JWT
  ├─ Verify repository ownership
  ├─ Create operation record (status: pending)
  ├─ Return 202 Accepted with operation ID
  └─ Spawn goroutine for async execution
          ↓
Async Goroutine:
  ├─ Update status to "running"
  ├─ Create IaC Executor
  ├─ Execute: tofu init && tofu plan
  ├─ Capture output/error
  ├─ Store plan output
  ├─ Update status to "success" or "failed"
  └─ Log completion
          ↓
Frontend polls every 2 seconds:
  GET /api/v1/operations/:operation_id
          ↓
Updates displayed in real-time:
  pending → running → success
```

## 📊 Data Flow - Operations

```
CREATE OPERATION
  ├─ ID: UUID (generated)
  ├─ RepositoryID: from request
  ├─ UserID: from JWT
  ├─ Type: "plan" | "apply" | "destroy"
  ├─ Status: "pending"
  ├─ Environment: "dev" | "prod"
  ├─ WorkingDir: from request
  ├─ Log: initial message
  └─ CreatedAt: now()

INSERT INTO operations (...)

ASYNC EXECUTION
  ├─ Start: status → "running", started_at = now()
  ├─ Execute: tofu init → tofu plan
  ├─ Capture: output, errors
  ├─ Update: 
  │  ├─ log ← stdout
  │  ├─ plan ← terraform output
  │  └─ error ← stderr (if error)
  └─ Complete: status → "success"/"failed", completed_at = now()

POLLING
  ├─ Frontend: GET /operations/:id every 2s
  ├─ Response:
  │  ├─ id, type, status, environment
  │  ├─ created_at, started_at, completed_at
  │  ├─ log, plan (if needed)
  │  └─ error (if failed)
  └─ Update UI with latest data
```

## 🔐 Security Architecture

```
AUTHENTICATION
  GitHub OAuth2
  ↓
  Authorization Code → Backend
  ↓
  Backend exchanges for Access Token
  ↓
  Backend fetches user data from GitHub API
  ↓
  Backend validates org membership
  ↓
  JWT generated + stored in DB
  ↓
  JWT sent to frontend + localStorage

AUTHORIZATION
  Protected routes check JWT
  ↓
  Verify token signature
  ↓
  Check token expiration (24h)
  ↓
  Extract user_id from claims
  ↓
  Verify user can access resource
  ↓
  Allow/Deny request

OPERATION SECURITY
  ├─ Only authenticated users can execute
  ├─ Only repo owner/org members can execute
  ├─ All operations logged with user_id
  ├─ Terraform runs in isolated directory
  ├─ No secrets in logs/output
  └─ Audit trail for compliance
```

## 🗄 Database Schema - Phase 4 Addition

```sql
-- Existing tables
users (id, github_id, login, email, avatar_url, org_name, created_at)
repositories (id, user_id, github_id, name, full_name, clone_url, cloned_path, connected_at)
repository_branches (id, repository_id, name, commit_sha, created_at)
repository_files (id, repository_id, file_path, content, created_at, updated_at)

-- NEW in Phase 4
operations (
  id UUID PRIMARY KEY,
  repository_id UUID NOT NULL REFERENCES repositories(id),
  user_id UUID NOT NULL REFERENCES users(id),
  type VARCHAR(50) NOT NULL,              -- 'plan', 'apply', 'destroy'
  status VARCHAR(50) NOT NULL,            -- 'pending', 'running', 'success', 'failed'
  environment VARCHAR(50) NOT NULL,       -- 'dev', 'prod', 'staging'
  working_dir VARCHAR(255),               -- 'environments/dev'
  plan TEXT,                              -- terraform plan output
  log TEXT,                               -- operation log/stdout
  error TEXT,                             -- error message if failed
  created_at TIMESTAMP DEFAULT NOW(),
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  created_by_user_id UUID                 -- audit trail
)

-- Indexes for common queries
CREATE INDEX idx_operations_repository_id ON operations(repository_id);
CREATE INDEX idx_operations_user_id ON operations(user_id);
CREATE INDEX idx_operations_status ON operations(status);
CREATE INDEX idx_operations_created_at ON operations(created_at DESC);
```

## 🔄 Phase Progression

```
Phase 1: Project Setup ✅
  └─ Base infrastructure, Docker, CI/CD

Phase 2: Authentication ✅
  └─ GitHub OAuth2, JWT, user management

Phase 3: Git Integration ✅
  └─ Repository sync, branch/file tracking

Phase 4: Operations API ✅ (CURRENT)
  └─ IaC execution, plan/apply/destroy, history

Phase 5: State Management (TODO)
  └─ Track infrastructure state, drift detection

Phase 6: Dashboard Enhancements (TODO)
  └─ Visualizations, real-time updates

Phase 7: Advanced Features (TODO)
  └─ Policies, cost estimation, webhooks

Phase 8: Testing (TODO)
  └─ Unit, integration, e2e tests

Phase 9: Production Deployment (TODO)
  └─ Scaling, monitoring, hardening
```

## 📦 Tech Stack Summary

| Layer | Technology | Purpose |
|-------|-----------|---------|
| **Frontend** | React 18 | UI framework |
| | TypeScript 5 | Type safety |
| | Tailwind CSS | Styling |
| | Zustand | State management |
| | React Router | Navigation |
| | Axios | HTTP client |
| **Backend** | Go 1.21 | Server runtime |
| | Gin Framework | Web framework |
| | PostgreSQL 13 | Database |
| | go-github | GitHub API |
| | golang.org/x/oauth2 | OAuth2 |
| | jsonwebtoken | JWT tokens |
| **DevOps** | Docker | Containerization |
| | Docker Compose | Orchestration |
| | GitHub Actions | CI/CD |
| **IaC** | OpenTofu/Terraform | Infrastructure code |
| | Floci | AWS emulation |

## 🚀 Deployment Architecture

```
Production Environment:
┌─────────────────────────────────────────────┐
│ Load Balancer (HTTPS)                       │
└────────────┬────────────────────────────────┘
             │
    ┌────────┴──────────┐
    │                   │
┌───▼───────┐    ┌──────▼────┐
│ Backend   │    │ Frontend   │
│ Container │    │ Container  │
│ (Replica) │    │ (Replica)  │
└───┬───────┘    └──────┬────┘
    │                   │
    └────────┬──────────┘
             │
    ┌────────▼──────────┐
    │  PostgreSQL       │
    │  (Managed DB)     │
    └───────────────────┘
```

## 📈 Performance Considerations

- **Frontend Polling**: 2-second intervals (configurable)
- **Database Queries**: Indexed for fast lookups
- **Async Execution**: Non-blocking API responses
- **Goroutines**: Per-operation execution in parallel
- **Plan Caching**: Terraform state files cached locally
- **Output Truncation**: Large outputs stored but paginated

## 🔮 Future Enhancements

- [ ] WebSocket instead of polling
- [ ] Real-time log streaming
- [ ] Cost impact calculation
- [ ] Policy-as-Code validation
- [ ] S3 backend for state files
- [ ] Terraform module registry
- [ ] GitOps workflows
- [ ] Compliance scanning
