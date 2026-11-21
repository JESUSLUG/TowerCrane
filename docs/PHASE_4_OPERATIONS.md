# Phase 4: REST API Core - Infrastructure Operations

Esta fase implementa los endpoints REST necesarios para ejecutar operaciones de infraestructura (plan, apply, destroy) mediante OpenTofu/Terraform.

## 🎯 Objetivos

- ✅ Crear endpoints REST para operaciones de IaC
- ✅ Implementar ejecución asincrónica de operaciones
- ✅ Tracking de historial de operaciones
- ✅ UI React para ejecutar y monitorear operaciones
- ✅ Integración con repositorios conectados

## 📋 Componentes Implementados

### Backend

#### 1. Database Layer (`backend/internal/db/operations.go`)

**Modelo**:
```go
type Operation struct {
  ID            string    // UUID
  RepositoryID  string    // Reference to connected repo
  UserID        string    // Who executed
  Type          string    // "plan", "apply", "destroy"
  Status        string    // "pending", "running", "success", "failed"
  Environment   string    // "dev", "prod", etc
  WorkingDir    string    // Path within repo (e.g., "environments/dev")
  Plan          string    // Terraform plan output
  Log           string    // Operation log
  Error         string    // Error message if failed
  CreatedAt     time.Time
  StartedAt     *time.Time
  CompletedAt   *time.Time
}
```

**Tablas SQL**:
- `operations` - Almacena información de operaciones
- Índices en: repository_id, user_id, status, created_at

**Métodos**:
- `Create()` - Crear nueva operación
- `GetByID()` - Obtener operación por ID
- `ListByRepository()` - Listar operaciones de un repositorio
- `UpdateStatus()` - Actualizar estado
- `UpdateLog()` - Actualizar log
- `UpdatePlan()` - Guardar plan output
- `Complete()` - Marcar como completada
- `Start()` - Marcar como iniciada

#### 2. IaC Executor (`backend/internal/iac/executor.go`)

**Características**:
- Ejecuta `tofu init`, `tofu plan`, `tofu apply`, `tofu destroy`
- Captura output y errores
- Parse de cambios en terraform plan
- Soporte para diferentes directorios de trabajo

**Tipos**:
```go
type ExecutorConfig struct {
  RepoPath    string // Ruta al repositorio clonado
  Environment string // dev, prod, etc
  WorkingDir  string // Subdirectorio (e.g., "environments/dev")
}

type PlanResult struct {
  Output   string
  Changes  []string // Recursos que cambiarán
  HasError bool
  Error    string
}
```

**Métodos**:
- `Plan()` - Ejecuta terraform plan
- `Apply()` - Ejecuta terraform apply
- `Destroy()` - Ejecuta terraform destroy

#### 3. API Handlers (`backend/internal/api/operations.go`)

**Endpoints**:

```
POST   /api/v1/repositories/:repo_id/operations/plan
POST   /api/v1/repositories/:repo_id/operations/apply
POST   /api/v1/repositories/:repo_id/operations/destroy
GET    /api/v1/repositories/:repo_id/operations
GET    /api/v1/operations/:operation_id
```

**Ejemplo de Plan Request**:
```json
{
  "repository_id": "uuid-here",
  "environment": "dev",
  "working_dir": "environments/dev"
}
```

**Respuesta (202 Accepted)**:
```json
{
  "id": "operation-uuid",
  "repository_id": "repo-uuid",
  "type": "plan",
  "status": "pending",
  "environment": "dev",
  "created_at": "2026-11-13T11:16:12Z"
}
```

**Características**:
- Ejecución asincrónica (no bloquea)
- Polling de status en tiempo real
- Captura de logs y errores
- Validación de autorización

### Frontend

#### 1. Store (`frontend/src/store/operationStore.ts`)

**Estado Zustand**:
```typescript
interface OperationState {
  operations: Operation[];
  currentOperation: Operation | null;
  isLoading: boolean;
  error: string | null;
}
```

**Métodos**:
- `setOperations()` - Establecer lista de operaciones
- `addOperation()` - Agregar nueva operación
- `updateOperation()` - Actualizar operación existente
- `setError()` - Establecer error
- `clearError()` - Limpiar error

#### 2. API Client (`frontend/src/api/operations.ts`)

```typescript
operationAPI.executePlan(data: PlanRequest): Promise<Operation>
operationAPI.executeApply(data: ApplyRequest): Promise<Operation>
operationAPI.executeDestroy(data: DestroyRequest): Promise<Operation>
operationAPI.getOperation(operationId: string): Promise<Operation>
operationAPI.listOperations(repositoryId: string): Promise<{ operations: Operation[] }>
```

#### 3. UI Component (`frontend/src/pages/OperationsPage.tsx`)

**Características**:
- Control panel para ejecutar operaciones
- Selector de ambiente (dev, prod, staging)
- Campo opcional para working directory
- Historial de operaciones con expansible
- Polling automático cada 2 segundos
- Muestra plan output, logs y errores

**Vista**:
```
┌─────────────────────────────────────┐
│  Execute Infrastructure Operations  │
├─────────────────────────────────────┤
│ Environment: [dev ▼]                │
│ Working Dir: [______________]       │
│ [Plan] [Apply] [Destroy]            │
├─────────────────────────────────────┤
│  Operations History                 │
│ ┌─────────────────────────────────┐ │
│ │ PLAN ✓ dev 2026-11-13 11:16    │ │
│ │ ▼ Show details                  │ │
│ └─────────────────────────────────┘ │
│ ┌─────────────────────────────────┐ │
│ │ APPLY ⏳ dev 2026-11-13 11:15   │ │
│ │ ▼ Show details                  │ │
│ └─────────────────────────────────┘ │
└─────────────────────────────────────┘
```

## 🔄 Flujo de Ejecución

### 1. User ejecuta Plan
```
User Request
    ↓
Handler crea Operation (pending)
    ↓
Retorna operation ID (202 Accepted)
    ↓
Goroutine async:
  - updateStatus(running)
  - Executor.Plan()
  - updatePlan(output)
  - Complete(success/failed)
    ↓
Frontend polling cada 2s → obtiene updates
```

### 2. Database State Transitions

```
PENDING → RUNNING → SUCCESS
                 ↘ FAILED
```

### 3. Ejecución Asincrónica

```go
go func() {
  operationStore.Start(opID)           // pending → running
  executor := NewExecutor(config)
  result := executor.Plan()
  
  if result.HasError {
    operationStore.Complete(opID, "failed", result.Error)
  } else {
    operationStore.UpdatePlan(opID, result.Output)
    operationStore.Complete(opID, "success", result.Output)
  }
}()
```

## 🛠 Requisitos del Sistema

**Backend**:
- Go 1.21+
- PostgreSQL 13+

**Frontend**:
- Node.js 20+
- React 18+

**IaC**:
- OpenTofu o Terraform instalado
- AWS CLI (para LocalStack testing)
- Docker (para LocalStack)

## 🧪 Testing

### Test Manual - Plan

```bash
curl -X POST http://localhost:8080/api/v1/repositories/{repo-id}/operations/plan \
  -H "Authorization: Bearer {token}" \
  -H "Content-Type: application/json" \
  -d '{
    "repository_id": "{repo-id}",
    "environment": "dev",
    "working_dir": "environments/dev"
  }'
```

**Respuesta**:
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

### Verificar Progreso

```bash
curl http://localhost:8080/api/v1/operations/550e8400-e29b-41d4-a716-446655440000 \
  -H "Authorization: Bearer {token}"
```

### Frontend Testing

1. Login con GitHub
2. Conectar repositorio
3. Ir a Operations tab
4. Seleccionar ambiente
5. Click en "Plan"
6. Ver en historial el progreso

## 📊 Tabla de Operaciones

```sql
CREATE TABLE operations (
  id UUID PRIMARY KEY,
  repository_id UUID NOT NULL,
  user_id UUID NOT NULL,
  type VARCHAR(50) NOT NULL,
  status VARCHAR(50) NOT NULL DEFAULT 'pending',
  environment VARCHAR(50) NOT NULL,
  working_dir VARCHAR(255),
  plan TEXT,
  log TEXT,
  error TEXT,
  created_at TIMESTAMP DEFAULT NOW(),
  started_at TIMESTAMP,
  completed_at TIMESTAMP,
  FOREIGN KEY (repository_id) REFERENCES repositories(id),
  FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE INDEX idx_operations_repository_id ON operations(repository_id);
CREATE INDEX idx_operations_user_id ON operations(user_id);
CREATE INDEX idx_operations_status ON operations(status);
CREATE INDEX idx_operations_created_at ON operations(created_at);
```

## 🔐 Seguridad

- ✅ Autenticación requerida (JWT)
- ✅ Authorization: solo dueño del repo puede ejecutar
- ✅ Sanitización de paths (working_dir)
- ✅ Validación de repository ID
- ✅ Logging de todas las operaciones

## 📈 Próximos Pasos (Phase 5+)

- [ ] State Management - Detectar drift de infraestructura
- [ ] Cost estimation - Calcular impacto de cambios
- [ ] Policy compliance - Validar políticas antes de apply
- [ ] Webhooks - Integración con CI/CD
- [ ] Output parsing - Extraer valores de terraform outputs
- [ ] Locking - Prevenir operaciones concurrentes
- [ ] Audit trail - Historial completo con razones

## 📝 Notas Importantes

### OpenTofu vs Terraform

Este proyecto usa **OpenTofu** (fork open-source de Terraform):
- Mejor compatibilidad con open-source
- Mismo comando `tofu` (sustituye `terraform`)
- Archivos `.tf` 100% compatibles
- Recomendado para nuevos proyectos

### LocalStack Testing

Para testing local sin AWS:
```bash
docker-compose -f docker-compose.floci.yml up
tofu init -backend=false
tofu plan -var region=us-east-1
```

### Logs y Debugging

Logs disponibles en:
- `operation.log` - Output estándar
- `operation.error` - Errores si falló
- `operation.plan` - Terraform plan output

## 🎓 Ejemplo Completo

Ver [docs/EXAMPLE_REPO_SETUP.md](./EXAMPLE_REPO_SETUP.md) para infraestructura de ejemplo con:
- Módulos reutilizables (networking, compute, database)
- Ambientes dev/prod pre-configurados
- LocalStack integration
- Terraform tfvars listos para usar
