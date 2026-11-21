# 🚀 Phase 4: Infrastructure Operations - Complete Walkthrough

## What's New in Phase 4?

TowerCrane can now **execute infrastructure operations** through a complete REST API that manages Terraform/OpenTofu execution lifecycle.

### New Capabilities

```
Before (Phase 3):
✅ Connect GitHub repos
✅ View branches and files
❌ Execute infrastructure changes

After (Phase 4):
✅ Connect GitHub repos
✅ View branches and files
✅ EXECUTE TERRAFORM PLAN
✅ EXECUTE TERRAFORM APPLY
✅ EXECUTE TERRAFORM DESTROY
✅ MONITOR OPERATIONS IN REAL-TIME
✅ VIEW OPERATION HISTORY
```

## 🎯 Key Features

### 1. Non-Blocking Operations
```
Traditional Approach:
POST /apply → [WAIT 30 seconds] → Response

TowerCrane Approach:
POST /apply → 202 Accepted (immediately)
        ↓ (async execution in background)
        ↓ (frontend polls for updates)
Response: { id: "op-123", status: "success" }
```

### 2. Real-Time Monitoring
- Frontend automatically polls every 2 seconds
- Watches operation progress: `pending` → `running` → `success`/`failed`
- Displays terraform plan output, logs, and errors
- Operation history preserved permanently

### 3. Multi-Environment Support
```
environments/
├── dev/              ← Select here
├── prod/             ← Or here
└── staging/          ← Or here

Also supports custom working directories:
modules/
├── networking/
├── compute/
└── database/
```

### 4. Complete Audit Trail
Every operation records:
- Who executed it (user_id)
- When it was executed (created_at, started_at, completed_at)
- Full output (plan, log)
- Any errors that occurred
- Which repository and environment

## 📊 API Reference

### Execute Plan
```bash
curl -X POST http://localhost:8080/api/v1/repositories/{repo-id}/operations/plan \
  -H "Authorization: Bearer {jwt-token}" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "{repo-id}",
    "environment": "dev",
    "working_dir": "environments/dev"
  }'
```

**Response (202 Accepted)**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "repository_id": "repo-uuid",
  "type": "plan",
  "status": "pending",
  "environment": "dev",
  "created_at": "2026-11-13T11:16:12Z"
}
```

### Get Operation Status
```bash
curl http://localhost:8080/api/v1/operations/550e8400-e29b-41d4-a716-446655440000 \
  -H "Authorization: Bearer {jwt-token}"
```

**Response**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "repository_id": "repo-uuid",
  "type": "plan",
  "status": "success",
  "environment": "dev",
  "created_at": "2026-11-13T11:16:12Z",
  "started_at": "2026-11-13T11:16:15Z",
  "completed_at": "2026-11-13T11:16:25Z",
  "plan": "Plan: 5 to add, 0 to change, 0 to destroy...",
  "log": "terraform plan output..."
}
```

### List All Operations for Repository
```bash
curl http://localhost:8080/api/v1/repositories/{repo-id}/operations \
  -H "Authorization: Bearer {jwt-token}"
```

## 🎮 Using the Frontend

### Step 1: Connect a Repository
1. Go to **Repositories** tab
2. Find a repo with Terraform/OpenTofu code
3. Click **Connect**

### Step 2: Navigate to Operations
1. From Repositories page, click on your repo
2. Look for **Operations** button/tab
3. Click to open Operations Page

### Step 3: Execute Plan
```
┌─────────────────────────────────────┐
│  Execute Infrastructure Operations  │
├─────────────────────────────────────┤
│ Environment: [dev ▼]                │
│ Working Dir: [environments/dev]     │
│ [Plan] [Apply] [Destroy]            │
└─────────────────────────────────────┘
```

1. Select **Environment**: dev, prod, or staging
2. (Optional) Enter **Working Directory**: e.g., `environments/dev`
3. Click **Plan**
4. Watch status change in real-time

### Step 4: Monitor Progress
```
Operations History
┌──────────────────────────────────────────────┐
│ PLAN ⏳ dev 2026-11-13 11:16                 │
│ Status: running                              │
│ ▼ View Details                               │
│                                              │
│ Plan Output (from Terraform)                │
│ └─ Plan: 5 to add, 0 to change...          │
│                                              │
│ Log Output (Terraform stdout)               │
│ └─ aws_security_group.app: Creating...     │
└──────────────────────────────────────────────┘
```

### Step 5: View Results
When complete:
- ✅ Success: See full plan output and terraform log
- ❌ Failed: See error message and debug info

### Step 6: Apply Changes (Optional)
Once plan succeeds:
1. Review changes carefully
2. Click **Apply** to execute
3. Infrastructure changes will be created
4. Monitor progress same as Plan

## 📈 Understanding Operations

### Operation Types
- **Plan**: Preview what terraform will do (no changes)
- **Apply**: Create/update resources (actually changes infrastructure)
- **Destroy**: Remove all resources (destructive!)

### Operation Status
- **pending**: Waiting to start (0-1 seconds usually)
- **running**: Currently executing terraform command (3-30 seconds)
- **success**: Completed successfully, check plan/log for results
- **failed**: Error occurred, check error field

### Timeline
```
PLAN       APPLY      DESTROY
│          │          │
┌──────────────────────────────┐
│ Time (seconds)               │
└──────────────────────────────┘
0 ←──→ 10 ←──→ 30 ←──────→ 60
pending  running    success
```

## 🧪 Test Scenario

### Using curl
```bash
# 1. Login and get token
# (from LoginPage in browser, check localStorage)

# 2. Get your repo ID
curl http://localhost:8080/api/v1/repositories \
  -H "Authorization: Bearer {token}"

# 3. Execute plan
curl -X POST http://localhost:8080/api/v1/repositories/{repo-id}/operations/plan \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "{repo-id}",
    "environment": "dev"
  }'

# Save the operation ID from response

# 4. Poll for updates (run multiple times)
curl http://localhost:8080/api/v1/operations/{op-id} \
  -H "Authorization: Bearer {token}"
```

## 🔧 Behind the Scenes

### How It Works

```
1. User clicks "Plan"
   ↓
2. API Handler receives request
   ├─ Validates authorization
   ├─ Creates operation record in DB (status=pending)
   ├─ Returns 202 Accepted immediately
   └─ Spawns goroutine
       ↓
3. Goroutine executes async:
   ├─ tofu init
   ├─ tofu plan -out=tfplan
   ├─ Captures output
   ├─ Stores in DB
   └─ Updates status → success/failed
       ↓
4. Frontend polls every 2s:
   ├─ GET /operations/{id}
   ├─ Receives updated status
   ├─ Updates UI
   └─ Stops when complete
```

### Database
```sql
CREATE TABLE operations (
  id UUID,                    -- Unique operation ID
  repository_id UUID,         -- Which repo
  user_id UUID,              -- Who ran it
  type VARCHAR(50),          -- plan/apply/destroy
  status VARCHAR(50),        -- pending/running/success/failed
  environment VARCHAR(50),   -- dev/prod/staging
  working_dir VARCHAR(255),  -- custom path in repo
  plan TEXT,                 -- terraform plan output
  log TEXT,                  -- terraform stdout
  error TEXT,                -- terraform stderr
  created_at TIMESTAMP,
  started_at TIMESTAMP,
  completed_at TIMESTAMP
);
```

## ⚙️ Configuration

### Environment Variables
```bash
# .env (already configured)
VITE_API_URL=http://localhost:8080/api/v1
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=towercrane
```

### Terraform/OpenTofu
```bash
# Must be installed locally
tofu --version

# TowerCrane expects 'tofu' command
# (substitute terraform command with tofu)
```

## 🐛 Troubleshooting

### "working directory does not exist"
**Cause**: Path relative to repo not found
**Fix**: 
- Check repo structure: `git clone <repo> && ls -la`
- Verify path: `environments/dev/` should exist
- Use correct working_dir: `environments/dev`

### "tofu: command not found"
**Cause**: OpenTofu not installed
**Fix**: Install from https://releases.opentofu.org

### Operation stuck in "running"
**Cause**: terraform command timed out
**Fix**: 
- Check if terraform is actually running: `ps aux | grep tofu`
- Kill if stuck: `kill <pid>`
- Check backend logs: `docker logs towercrane-backend`

### Frontend doesn't update
**Cause**: Token expired or polling stopped
**Fix**:
- Reload page (gets fresh token)
- Check browser console for errors
- Verify token in DevTools → Application → localStorage

## 📚 Documentation

For complete details, see:
- [PHASE_4_OPERATIONS.md](./docs/PHASE_4_OPERATIONS.md) - API reference
- [PHASE_4_TESTING.md](./docs/PHASE_4_TESTING.md) - Testing guide
- [ARCHITECTURE.md](./docs/ARCHITECTURE.md) - System design
- [PHASE_4_SUMMARY.md](./docs/PHASE_4_SUMMARY.md) - Executive summary

## 🚀 Next Steps

1. **Test Locally**
   ```bash
   docker-compose up
   # Access http://localhost:3000
   ```

2. **Create Example Repository**
   - Follow [CREATE_PRIVATE_REPO.md](./docs/CREATE_PRIVATE_REPO.md)
   - Use template from session files

3. **Test Operations**
   - Connect repo → Execute Plan → View results
   - Run Apply to create actual resources

4. **Prepare for Phase 5**
   - Phase 5 will add state management and drift detection
   - State files will be tracked automatically

## ✅ Success Indicators

- [x] Can execute plan without errors
- [x] Real-time status updates in UI
- [x] Operation history shows all past operations
- [x] Can view plan output and logs
- [x] Apply creates actual infrastructure
- [x] Destroy removes infrastructure
- [x] All endpoints require authentication
- [x] Database persists everything

## 🎓 Learning Resources

- Terraform/OpenTofu: https://opentofu.org/docs/
- REST API Design: https://restfulapi.net/
- Go Async: https://golang.org/doc/effective_go#goroutines
- React Hooks: https://react.dev/reference/react

---

**Phase 4 Status**: ✅ Complete  
**Ready for**: Phase 5 - State Management  
**Estimated Time**: 2-3 weeks for Phase 5-9
