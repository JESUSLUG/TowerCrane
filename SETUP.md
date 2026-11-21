# Project Setup Guide

## Prerequisites

- Go 1.21+
- Node.js 20+
- PostgreSQL 16+
- Docker & Docker Compose

## Local Development Setup

### 1. Backend Setup

```bash
cd backend

# Install dependencies
go mod download

# Create .env file
cp .env.example .env

# Run the server
go run cmd/main.go
```

Backend will run on `http://localhost:8080`

### 2. Frontend Setup

```bash
cd frontend

# Install dependencies
npm install

# Create .env file (optional - proxy is configured in vite.config.ts)
# API calls to /api will be proxied to http://localhost:8080

# Run development server
npm run dev
```

Frontend will run on `http://localhost:5173`

## Docker Setup

To run the complete stack with Docker Compose:

```bash
docker-compose up
```

This will start:
- **PostgreSQL** on `localhost:5432`
- **Backend** on `http://localhost:8080`
- **Frontend** on `http://localhost:3000`

## Project Structure

```
TowerCrane/
├── backend/                 # Go backend
│   ├── cmd/                # Executable entry points
│   ├── internal/
│   │   ├── api/           # API routes and handlers
│   │   ├── auth/          # Authentication logic
│   │   ├── db/            # Database layer
│   │   └── models/        # Data models
│   ├── go.mod            # Go dependencies
│   └── Dockerfile
├── frontend/               # React frontend
│   ├── src/
│   │   ├── App.tsx       # Main app component
│   │   ├── main.tsx      # Entry point
│   │   └── index.css     # Global styles
│   ├── package.json      # Node dependencies
│   ├── tsconfig.json     # TypeScript config
│   ├── vite.config.ts    # Vite config
│   └── Dockerfile
├── docs/                  # Documentation
├── .github/
│   └── workflows/        # CI/CD pipelines
└── docker-compose.yml    # Docker Compose configuration
```

## Development Workflow

1. Make changes in `backend/` or `frontend/`
2. Backend changes: restart `go run cmd/main.go`
3. Frontend changes: HMR (Hot Module Replacement) should reload automatically
4. Run linters: `npm run lint` (frontend) or `golangci-lint` (backend)
5. Commit with descriptive messages

## Testing

### Backend
```bash
cd backend
go test ./...
```

### Frontend
```bash
cd frontend
npm run type-check  # TypeScript checking
npm run lint        # ESLint
```

## Building

### Backend
```bash
cd backend
go build -o towercrane cmd/main.go
```

### Frontend
```bash
cd frontend
npm run build
```

Output will be in `frontend/dist/`

## Environment Variables

### Backend (.env)
```
PORT=8080
DB_HOST=localhost
DB_PORT=5432
DB_USER=towercrane
DB_PASSWORD=towercrane
DB_NAME=towercrane
JWT_SECRET=your-secret-key-change-in-production
LOG_LEVEL=debug
```

### Frontend
Frontend configuration is in `vite.config.ts` - API calls are proxied to the backend during development.
