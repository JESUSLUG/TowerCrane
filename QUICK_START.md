# TowerCrane Quick Start Guide

## 📋 Prerequisites

Make sure you have installed:
- Go 1.20+
- Node.js 18+
- Docker & Docker Compose
- OpenTofu (or Terraform)

### Install OpenTofu (Linux)

```bash
curl -fsSL https://apt.opentofu.org/opentofu.gpg | sudo tee /etc/apt/trusted.gpg.d/opentofu.gpg >/dev/null
echo "deb https://apt.opentofu.org/$(lsb_release -cs) opentofu main" | sudo tee /etc/apt/sources.list.d/opentofu.list >/dev/null
sudo apt-get update && sudo apt-get install -y tofu
```

Verify: `tofu --version`

## 🚀 Quick Start (5 minutes)

### 1. Start PostgreSQL

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane
docker-compose up postgres -d
```

Wait for it to be ready (~10 seconds).

### 2. Start Backend (Terminal 1)

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane
go run ./cmd/main.go
```

You should see: `Server running on :8080`

### 3. Start Frontend (Terminal 2)

```bash
cd /home/eduardo_lugo/Repos/JESUSLUG/TowerCrane/frontend
npm install  # Only first time
npm run dev
```

You should see: `VITE v... ready in ... ms`

### 4. Open in Browser

```
http://localhost:3000
```

Click "Login with GitHub" to get started.

## 🧪 Testing Phase 4

### A. Via API

1. Get your JWT token from the frontend (check browser Storage)
2. Connect TowerCrane-Example repository via API:

```bash
TOKEN="your_jwt_token"
REPO_ID="your_repo_id"

curl -X POST http://localhost:8080/api/operations/plan \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "'$REPO_ID'",
    "environment": "dev",
    "working_dir": "environments/dev"
  }'
```

3. Get operation status:

```bash
OPERATION_ID="op_xxx"

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/operations/$OPERATION_ID
```

### B. Via Frontend

1. Go to `http://localhost:3000`
2. Click "Repositories" → "Add Repository"
3. Enter: `https://github.com/JESUSLUG/TowerCrane-Example`
4. Go to "Operations" tab
5. Click "Execute Plan"
6. Select environment: `dev`
7. Set working_dir: `environments/dev`
8. Click "Execute"
9. Watch real-time updates (polling every 2 seconds)

## 📁 Project Structure

```
TowerCrane/
├── backend/
│   ├── cmd/main.go                    # Backend entry point
│   ├── internal/
│   │   ├── api/operations.go          # REST handlers
│   │   ├── db/operations.go           # Database layer
│   │   └── iac/executor.go            # OpenTofu wrapper
│   └── ...
├── frontend/
│   ├── src/
│   │   ├── pages/OperationsPage.tsx   # UI component
│   │   ├── store/operationStore.ts    # State management
│   │   ├── api/operations.ts          # API client
│   │   └── ...
│   └── ...
└── docs/
    ├── PHASE_4_REAL_TESTING.md        # Complete testing guide
    ├── PHASE_4_OPERATIONS.md          # API reference
    ├── ARCHITECTURE.md                # System design
    └── ...

TowerCrane-Example/                     # Test infrastructure repo
├── environments/
│   ├── dev/main.tf                    # Dev environment
│   └── prod/main.tf                   # Prod environment
└── modules/
    ├── networking/
    ├── compute/
    └── database/
```

## 🐛 Troubleshooting

### Port already in use

```bash
# Backend (8080)
lsof -i :8080

# Frontend (5173)
lsof -i :5173
```

Kill the process: `kill -9 <PID>`

### "Cannot find module" errors in frontend

```bash
cd frontend
rm -rf node_modules package-lock.json
npm install
npm run dev
```

### PostgreSQL connection failed

```bash
# Check if container is running
docker ps | grep postgres

# View logs
docker logs towercrane-postgres

# Restart
docker-compose restart postgres
```

### "Command not found: tofu"

Install OpenTofu (see Prerequisites section).

## 📚 Documentation

- **[PHASE_4_REAL_TESTING.md](./docs/PHASE_4_REAL_TESTING.md)** - Complete testing guide with detailed steps
- **[PHASE_4_OPERATIONS.md](./docs/PHASE_4_OPERATIONS.md)** - REST API reference
- **[ARCHITECTURE.md](./docs/ARCHITECTURE.md)** - System design and data flow
- **[INDEX.md](./docs/INDEX.md)** - Documentation index

## 🚦 API Endpoints

### Operations

```
POST   /api/operations/plan      - Execute terraform plan
POST   /api/operations/apply     - Execute terraform apply
POST   /api/operations/destroy   - Execute terraform destroy
GET    /api/operations/:id       - Get operation details
GET    /api/operations           - List operations
```

All endpoints require JWT authentication.

## 📊 Development Workflow

1. **Make changes** to code
2. **Backend rebuilds automatically** (use hot reload tools if available)
3. **Frontend rebuilds automatically** (Vite has hot reload built-in)
4. **Test in browser** - changes appear instantly

## 🔑 Environment Variables

Backend reads from `.env` (see `docker-compose.yml` for defaults):

```bash
DATABASE_URL=postgres://towercrane_user:towercrane_pass@localhost:5432/towercrane_db
JWT_SECRET=your-secret-key
GITHUB_CLIENT_ID=your-client-id
GITHUB_CLIENT_SECRET=your-client-secret
GITHUB_ORG=your-org-name
```

## 🎯 Next Steps

1. ✅ Get backend running
2. ✅ Get frontend running
3. ✅ Connect GitHub account
4. ✅ Add TowerCrane-Example repository
5. ✅ Execute a plan operation
6. ✅ Check database for audit trail
7. → Start Phase 5: State Management

## 💡 Tips

- **Real-time polling**: Frontend automatically polls for updates every 2 seconds
- **No blocking**: Operations return 202 Accepted immediately
- **Concurrent**: Multiple operations can run in parallel
- **Audit trail**: Every operation is logged to database
- **User isolation**: You can only access your own repositories

## 🆘 Getting Help

1. Check the relevant documentation file
2. Look for error messages in backend/frontend logs
3. Verify all prerequisites are installed
4. Check `PHASE_4_REAL_TESTING.md` troubleshooting section

---

**You're ready to go! Happy testing! 🚀**
