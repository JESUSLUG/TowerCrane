# Phase 4 Testing Guide

Guía completa para testear los endpoints de operaciones de infraestructura.

## 🚀 Prerequisites

1. TowerCrane running:
```bash
docker-compose up
```

2. Backend running on `http://localhost:8080`
3. Frontend running on `http://localhost:3000`
4. OpenTofu or Terraform installed locally
5. A repository connected to TowerCrane

## 📋 Test Scenarios

### Scenario 1: Successful Plan Operation

#### Setup
```bash
# Create test repository with basic terraform
mkdir -p /tmp/test-repo/environments/dev
cd /tmp/test-repo

# Create simple main.tf
cat > environments/dev/main.tf << 'EOF'
terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_security_group" "test" {
  name = "test-sg"
  
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

output "security_group_id" {
  value = aws_security_group.test.id
}
EOF

# Create variables.tf
cat > environments/dev/variables.tf << 'EOF'
variable "region" {
  default = "us-east-1"
}
EOF
```

#### Test via cURL
```bash
# 1. Get your JWT token
TOKEN="your-jwt-token-here"
REPO_ID="your-repository-id"

# 2. Execute plan
curl -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "'${REPO_ID}'",
    "environment": "dev",
    "working_dir": "environments/dev"
  }'
```

#### Expected Response
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "repository_id": "repo-id",
  "type": "plan",
  "status": "pending",
  "environment": "dev",
  "created_at": "2026-11-13T11:16:12Z"
}
```

#### Poll Status
```bash
OPERATION_ID="550e8400-e29b-41d4-a716-446655440000"

# Check status every 2 seconds
while true; do
  curl http://localhost:8080/api/v1/operations/${OPERATION_ID} \
    -H "Authorization: Bearer ${TOKEN}"
  sleep 2
done
```

#### Expected Progression
```
1. { "status": "pending" }
   ↓ (after ~1s)
2. { "status": "running" }
   ↓ (after ~3-5s)
3. { "status": "success", "plan": "..." }
```

### Scenario 2: List Operations

#### Test
```bash
curl http://localhost:8080/api/v1/repositories/${REPO_ID}/operations \
  -H "Authorization: Bearer ${TOKEN}"
```

#### Expected Response
```json
{
  "operations": [
    {
      "id": "op-1",
      "repository_id": "repo-id",
      "type": "plan",
      "status": "success",
      "environment": "dev",
      "created_at": "2026-11-13T11:16:12Z"
    },
    {
      "id": "op-2",
      "repository_id": "repo-id",
      "type": "apply",
      "status": "running",
      "environment": "dev",
      "created_at": "2026-11-13T11:15:00Z"
    }
  ]
}
```

### Scenario 3: Failed Operation (Invalid Path)

#### Test
```bash
curl -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "'${REPO_ID}'",
    "environment": "dev",
    "working_dir": "non/existent/path"
  }'
```

#### Expected Result
- Status becomes "failed"
- Error contains: "working directory does not exist"

### Scenario 4: Frontend Testing

#### Step 1: Login
1. Go to `http://localhost:3000`
2. Click "Login with GitHub"
3. Authorize the application

#### Step 2: Connect Repository
1. Go to "Repositories" tab
2. Find a repository with Terraform code
3. Click "Connect"

#### Step 3: Execute Operation
1. From Repositories page, click on connected repo
2. Should see "Operations" button or tab
3. Click "Plan"
4. Should see operation appear in history as "pending"
5. Wait 2-5 seconds
6. Status should change to "running"
7. Wait another 3-10 seconds
8. Status should change to "success"

#### Step 4: View Details
1. Click on operation in history (expand)
2. Should see Plan Output
3. Should see Logs
4. Can execute "Apply" or another "Plan"

## 🧪 Test Cases

### Test Case 1: Unauthorized Access
**Expected**: 401 Unauthorized
```bash
curl -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Content-Type: application/json" \
  -d '{"repository_id":"'"${REPO_ID}"'","environment":"dev"}'
```

### Test Case 2: Invalid Repository
**Expected**: 500 or 404
```bash
INVALID_REPO="00000000-0000-0000-0000-000000000000"
curl -X POST http://localhost:8080/api/v1/repositories/${INVALID_REPO}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"repository_id":"'"${INVALID_REPO}"'","environment":"dev"}'
```

### Test Case 3: Missing Required Fields
**Expected**: 400 Bad Request
```bash
curl -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"repository_id":"'"${REPO_ID}"'"}'
```

### Test Case 4: Concurrent Operations
**Expected**: Both should queue and execute independently
```bash
# Terminal 1
OP1=$(curl -s -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"repository_id":"'"${REPO_ID}"'","environment":"dev"}' | jq -r .id)

# Terminal 2 (same time)
OP2=$(curl -s -X POST http://localhost:8080/api/v1/repositories/${REPO_ID}/operations/plan \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"repository_id":"'"${REPO_ID}"'","environment":"dev"}' | jq -r .id)

# Monitor both
watch "curl -s http://localhost:8080/api/v1/operations/${OP1} -H 'Authorization: Bearer ${TOKEN}' | jq .status && curl -s http://localhost:8080/api/v1/operations/${OP2} -H 'Authorization: Bearer ${TOKEN}' | jq .status"
```

## 🔍 Debugging

### Check Database
```bash
# Connect to PostgreSQL
psql -h localhost -U postgres -d towercrane

# List operations
SELECT id, type, status, environment, created_at, started_at, completed_at 
FROM operations 
ORDER BY created_at DESC LIMIT 10;

# Check operation details
SELECT * FROM operations WHERE id = 'operation-uuid-here';

# View operation log
SELECT id, status, log FROM operations WHERE id = 'operation-uuid-here';
```

### Check Logs
```bash
# Backend logs
docker logs towercrane-backend

# Check for terraform errors
cat backend/logs/terraform.log

# Frontend console
# Browser DevTools → Console tab
```

### Common Issues

**Issue**: "working directory does not exist"
- **Cause**: Repository not cloned or path incorrect
- **Fix**: Ensure repository is connected and path is relative to repo root

**Issue**: "tofu: command not found"
- **Cause**: OpenTofu not installed
- **Fix**: Install OpenTofu: `curl -fsSL https://releases.opentofu.org/opentofu-repo-signing-key.gpg | sudo apt-key add - && echo "deb https://releases.opentofu.org/opentofu/ubuntu $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/opentofu.list && sudo apt update && sudo apt install tofu`

**Issue**: Operation stuck in "running" state
- **Cause**: Terraform command timed out or hung
- **Fix**: Kill process manually and mark as failed

**Issue**: Frontend doesn't update automatically
- **Cause**: Polling not working or token expired
- **Fix**: 
  - Check browser console for errors
  - Verify token in localStorage
  - Clear cache and reload

## 📊 Performance Benchmarks

Expected timings:
- `tofu init`: 2-5 seconds (first time), <1s (cached)
- `tofu plan`: 3-10 seconds
- `tofu apply`: 5-30 seconds (depending on resources)
- `tofu destroy`: 5-30 seconds

Database queries should complete in <100ms.

## ✅ Success Criteria

- [x] Can execute plan without errors
- [x] Operation status updates from pending → running → success
- [x] Can view operation output and logs
- [x] Multiple operations can run concurrently
- [x] Frontend polls updates every 2 seconds
- [x] Database persists all operation history
- [x] Unauthorized requests return 401
- [x] Invalid paths return appropriate errors
- [x] Apply and Destroy also work correctly

## 🚀 Next Steps

Once Phase 4 testing is complete:
1. Test with real infrastructure (not just terraform checks)
2. Test with LocalStack for full AWS emulation
3. Begin Phase 5: State Management and drift detection
