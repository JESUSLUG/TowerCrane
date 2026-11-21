# Phase 4 Real Testing Guide

## Overview

Este documento describe cómo probar **Phase 4: Infrastructure Operations API** usando el repositorio real `TowerCrane-Example` con archivos OpenTofu reales.

## Setup

### 1. Verificar que ambos repos existen

```bash
# Main project
ls -la /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane

# Example repo with OpenTofu files
ls -la /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example
```

### 2. Verificar estructura OpenTofu

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example
tree -L 2
```

Estructura esperada:
```
.
├── README.md
├── .gitignore
├── docker-compose.floci.yml
├── setup.sh
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── outputs.tf
│   └── prod/
│       ├── main.tf
│       ├── variables.tf
│       └── outputs.tf
└── modules/
    ├── compute/
    │   ├── main.tf
    │   ├── variables.tf
    │   └── outputs.tf
    ├── database/
    │   ├── main.tf
    │   ├── variables.tf
    │   └── outputs.tf
    └── networking/
        ├── main.tf
        ├── variables.tf
        └── outputs.tf
```

### 3. Inicializar OpenTofu

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example/environments/dev

# Initialize OpenTofu (NOT terraform)
tofu init

# Validate OpenTofu configuration
tofu validate

# Plan infrastructure changes (no apply yet)
tofu plan -out=tfplan
```

**Note**: Always use `tofu` command, NOT `terraform`. OpenTofu is the open-source alternative.

## Testing Flow

### Phase 4 API Testing

#### 1. Start TowerCrane Backend

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane

# Make sure PostgreSQL is running
docker-compose up postgres -d

# Build and run backend
go run ./cmd/main.go
```

Expected output:
```
2026/11/13 12:55:00 Server running on :8080
```

#### 2. Authenticate with GitHub

```bash
# Get your GitHub token (from Phase 2 login)
TOKEN="your_jwt_token_here"

# Verify token works
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/repositories
```

#### 3. Register TowerCrane-Example Repository

Via API:
```bash
curl -X POST http://localhost:8080/api/repositories \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "TowerCrane-Example",
    "url": "https://github.com/JESUSLUG/TowerCrane-Example",
    "branch": "master"
  }'
```

Or use the React UI at `http://localhost:3000`.

#### 4. Test OpenTofu Plan

Request:
```bash
curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "repo-id-from-above",
    "environment": "dev",
    "working_dir": "environments/dev"
  }'
```

Response (202 Accepted):
```json
{
  "operation_id": "op-12345",
  "status": "pending",
  "message": "Operation queued"
}
```

#### 5. Poll Operation Status

```bash
# Get operation details
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/operations/op-12345

# Expected response when complete:
{
  "id": "op-12345",
  "repository_id": "repo-id",
  "user_id": "user-id",
  "operation_type": "plan",
  "status": "success",
  "environment": "dev",
  "working_dir": "environments/dev",
  "output": "Plan: 5 to add, 0 to change, 0 to destroy...",
  "error": null,
  "started_at": "2026-11-13T12:55:00Z",
  "completed_at": "2026-11-13T12:55:05Z",
  "created_at": "2026-11-13T12:55:00Z",
  "updated_at": "2026-11-13T12:55:05Z"
}
```

#### 6. Test Frontend Polling

1. Open `http://localhost:3000`
2. Login with GitHub
3. Connect TowerCrane-Example repository
4. Navigate to Operations tab
5. Click "Execute Plan"
6. Select environment: `dev`
7. Enter working_dir: `environments/dev`
8. Click "Execute"

Observe:
- Status changes from `pending` → `running` → `success`
- Output updates in real-time (polling every 2 seconds)
- Timestamps show operation duration
- Detailed error messages if any

### Edge Cases to Test

#### 1. Invalid OpenTofu Configuration

Create a broken OpenTofu file:
```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example
echo 'invalid terraform code here' > environments/test/main.tf
```

Then test plan - should return error in `output` field.

#### 2. Non-existent Working Directory

Request:
```bash
curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "repo-id",
    "environment": "nonexistent",
    "working_dir": "environments/nonexistent"
  }'
```

Should return error in operation output.

#### 3. Multiple Concurrent Operations

Request plan for `dev` and `prod` environments simultaneously:
```bash
# Terminal 1
curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repository_id": "repo-id", "environment": "dev", "working_dir": "environments/dev"}'

# Terminal 2 (simultaneously)
curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repository_id": "repo-id", "environment": "prod", "working_dir": "environments/prod"}'
```

Both should execute in parallel goroutines.

#### 4. User Isolation

Verify that a different user cannot access operations from another user's repository:
```bash
# Get operation ID from user A
OPERATION_ID="op-12345"

# Try to access with user B's token
curl -H "Authorization: Bearer $USER_B_TOKEN" \
  http://localhost:8080/api/operations/$OPERATION_ID
```

Should return 403 Forbidden.

### Database Verification

Connect to PostgreSQL and verify operations table:

```bash
# Connect to postgres
docker exec -it towercrane-postgres psql -U towercrane_user -d towercrane_db

# List operations
SELECT id, repository_id, operation_type, status, environment, created_at, completed_at 
FROM operations 
ORDER BY created_at DESC 
LIMIT 10;

# Count by status
SELECT status, COUNT(*) FROM operations GROUP BY status;

# View operation details with output
SELECT id, operation_type, status, output, error FROM operations 
WHERE id = 'op-12345';
```

## Performance Testing

### Throughput Test

Execute 10 plans concurrently:
```bash
for i in {1..10}; do
  curl -X POST http://localhost:8080/api/operations/plan \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"repository_id\": \"repo-id\", \"environment\": \"dev\", \"working_dir\": \"environments/dev\"}" &
done
wait
```

Monitor:
- All should return 202 quickly (<100ms)
- Operations should execute in parallel
- Check database for all 10 operations

### Latency Test

```bash
# Measure API response time
time curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repository_id": "repo-id", "environment": "dev", "working_dir": "environments/dev"}'
```

Expected: <50ms

### Memory Test

Monitor goroutine count:
```bash
# While operations are running, check goroutines
curl -s http://localhost:8080/debug/pprof/goroutine | head -20
```

Should show one goroutine per operation + base app goroutines.

## Troubleshooting

### Plan Fails with "command not found: tofu"

**Fix**: Install OpenTofu
```bash
# macOS
brew install opentofu

# Linux
curl -fsSL https://apt.opentofu.org/opentofu.gpg | sudo tee /etc/apt/trusted.gpg.d/opentofu.gpg >/dev/null
echo "deb https://apt.opentofu.org/$(lsb_release -cs) opentofu main" | sudo tee /etc/apt/sources.list.d/opentofu.list >/dev/null
sudo apt-get update && sudo apt-get install -y tofu

# Verify
tofu version
```

### "Working directory not found"

The working_dir must exist in the repository. Verify:
```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example
test -d "environments/dev" && echo "Directory exists" || echo "Not found"
```

### "Failed to initialize working directory"

Usually means OpenTofu variables are missing. Check:
```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane-Example/environments/dev
tofu init -upgrade
tofu validate
```

### Operation Stuck in "running"

Check logs:
```bash
# Backend logs for goroutine panic
docker logs towercrane-app 2>&1 | grep -i "panic\|error\|goroutine"

# Database for orphaned operations
psql -U towercrane_user -d towercrane_db -c \
  "SELECT * FROM operations WHERE status = 'running' AND updated_at < NOW() - INTERVAL '5 minutes'"
```

## Next Steps

Once Phase 4 testing is verified:

1. ✅ Test plan operation
2. ✅ Test apply operation (with auto-approve)
3. ✅ Test destroy operation
4. ✅ Test error handling
5. ✅ Verify frontend polling
6. ✅ Load testing with concurrent operations
7. → Proceed to Phase 5: State Management

## Resources

- OpenTofu Documentation: https://opentofu.org/docs/
- TowerCrane Phase 4 API: `docs/PHASE_4_OPERATIONS.md`
- Architecture: `docs/ARCHITECTURE.md`
