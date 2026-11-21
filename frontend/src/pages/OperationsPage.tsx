import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import {
  ChevronDown,
  History,
  ScrollText,
  ShieldAlert,
  Siren,
  Terminal,
} from 'lucide-react';
import { operationAPI } from '../api/operations';
import { useOperationStore } from '../store/operationStore';
import type { Operation } from '../store/operationStore';
import type { MapApplication } from '../api/resourceMap';
import { useResourceMap } from '../hooks/useResourceMap';
import { Layout } from '../components/Layout';
import { Card, CardBody, CardHeader, CardTitle } from '../components/ui/Card';
import { Badge } from '../components/ui/Badge';
import { EmptyState } from '../components/ui/EmptyState';
import { Alert } from '../components/ui/Alert';
import { PageHeader } from '../components/ui/PageHeader';
import { Field, Select } from '../components/ui/Field';
import { StatusBadge } from '../components/ui/StatusBadge';
import { MetaLine } from '../components/ui/MetaLine';
import { CodeBlock } from '../components/ui/CodeBlock';
import { cn } from '../lib/utils';
import { atLeast } from '../lib/roles';
import { operationStatus } from '../lib/status';
import { useAuth } from '../hooks/useAuth';
import { apiError } from '../api/admin';
import { RefreshButton } from '../components/RefreshButton';
import { InstanceName } from '../components/InstanceName';
import { AwaitingYesCard, RunOperationCard } from '../components/operations';

export function OperationsPage() {
  const { repo_id } = useParams<{ repo_id: string }>();
  const repositoryId = repo_id ?? '';
  const { operations, addOperation, updateOperation } = useOperationStore();
  const { user } = useAuth();
  const canApply = atLeast(user?.role, 'approver');
  const isAdmin = atLeast(user?.role, 'admin');

  const [applications, setApplications] = useState<MapApplication[]>([]);
  const [appName, setAppName] = useState('');
  const [error, setError] = useState<string | null>(null);

  const loadOperations = useCallback(async () => {
    try {
      const result = await operationAPI.listOperations(repositoryId, 50);
      useOperationStore.setState({ operations: result.operations });
    } catch (err) {
      setError(apiError(err, 'Failed to load operations'));
    }
  }, [repositoryId]);

  // Applications come from the project's manifests.
  // (cached: shown at once, refreshed in the background)
  const { map: repoMap, error: mapError } = useResourceMap(repositoryId ?? '');
  useEffect(() => {
    if (!repositoryId) return;
    loadOperations();
  }, [repositoryId, loadOperations]);
  useEffect(() => {
    if (!repoMap) return;
    const apps = repoMap.applications ?? [];
    setApplications(apps);
    if (apps[0]) setAppName((cur) => cur || apps[0].name);
  }, [repoMap]);
  useEffect(() => {
    if (mapError) setError(mapError);
  }, [mapError]);

  // Poll operations that are still running.
  useEffect(() => {
    const runningOps = operations.filter((op) => op.status === 'pending' || op.status === 'running');
    if (runningOps.length === 0) return;
    const interval = setInterval(async () => {
      for (const op of runningOps) {
        try {
          updateOperation(await operationAPI.getOperation(op.id));
        } catch {
          // keep polling
        }
      }
      // A plan may have spawned an automatic apply.
      loadOperations();
    }, 2500);
    return () => clearInterval(interval);
  }, [operations, updateOperation, loadOperations]);

  const selectedApp = applications.find((a) => a.name === appName);
  const awaiting = operations.filter((op) => op.type === 'plan' && op.awaiting_confirmation);

  return (
    <Layout>
      <PageHeader
        back={repositoryId ? { to: `/projects/${repositoryId}`, label: 'Project' } : undefined}
        title="Operations"
        description="TowerCrane plans every Application after each merge. Plans of apply: manual Applications (and every first apply) wait here for a “yes”."
        actions={repositoryId && <RefreshButton repoId={repositoryId} onProcessing={() => setTimeout(loadOperations, 1500)} />}
      />

      {error && <Alert className="mb-6">{error}</Alert>}

      <AwaitingYesCard
        plans={awaiting}
        canApply={canApply}
        isAdmin={isAdmin}
        onChanged={(created) => {
          if (created) addOperation(created);
          loadOperations();
        }}
        onError={setError}
      />

      <RunOperationCard
        repoId={repositoryId}
        appName={appName}
        canApply={canApply}
        isAdmin={isAdmin}
        onStarted={addOperation}
        onError={setError}
      >
        <Field
          label="Application"
          hint={
            selectedApp && (
              <>
                Defined in <code className="font-mono">{selectedApp.file}</code>
                {selectedApp.cloud_account && (
                  <>
                    {' '}
                    · cloud account <code className="font-mono">{selectedApp.cloud_account}</code>
                  </>
                )}
              </>
            )
          }
        >
          <Select value={appName} onChange={(e) => setAppName(e.target.value)}>
            {applications.length === 0 && <option value="">No Applications in the manifests</option>}
            {applications.map((app) => (
              <option key={app.name} value={app.name}>
                {app.app}
                {app.group ? ` [${app.group}]` : ''} — {app.path} ({app.apply})
              </option>
            ))}
          </Select>
        </Field>
      </RunOperationCard>

      <Card>
        <CardHeader>
          <CardTitle title="Operations history" />
        </CardHeader>
        <CardBody>
          {operations.length === 0 ? (
            <EmptyState
              icon={History}
              title="No operations yet"
              description="Merged changes are planned automatically; you can also run a plan above."
            />
          ) : (
            <div className="space-y-2">
              {operations.map((operation) => (
                <OperationRow key={operation.id} operation={operation} />
              ))}
            </div>
          )}
        </CardBody>
      </Card>
    </Layout>
  );
}

export function OperationRow({ operation }: { operation: Operation }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className={cn('overflow-hidden rounded-lg border', operation.emergency ? 'border-danger/40' : 'border-line')}>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        className="flex w-full items-center justify-between gap-4 px-4 py-3 text-left transition hover:bg-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary"
      >
        <div className="min-w-0 flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <span
              className={cn(
                'w-16 font-mono text-xs font-bold uppercase',
                operation.type === 'destroy' ? 'text-danger' : 'text-ink'
              )}
            >
              {operation.type}
            </span>
            <StatusBadge {...operationStatus[operation.status]} />
            {operation.emergency && (
              <Badge tone="danger">
                <Siren className="h-3 w-3" /> Emergency
              </Badge>
            )}
            {operation.awaiting_confirmation && <StatusBadge state="attention" label="Waiting for yes" />}
            {operation.type === 'plan' && operation.status === 'success' && !operation.has_changes && (
              <Badge tone="neutral">No changes</Badge>
            )}
            <InstanceName name={operation.application || operation.environment} className="font-mono text-sm font-medium text-ink" />
            <MetaLine items={[operation.working_dir, operation.commit_sha?.slice(0, 7)]} />
          </div>
          {operation.policy_note && <p className="text-xs text-muted">{operation.policy_note.split('\n')[0]}</p>}
          <p className="text-xs text-muted">{new Date(operation.created_at).toLocaleString()}</p>
        </div>
        <ChevronDown className={cn('h-5 w-5 flex-shrink-0 text-muted transition-transform', expanded && 'rotate-180')} />
      </button>

      {expanded && (
        <div className="space-y-3 border-t border-line bg-canvas px-4 py-3">
          {operation.error && <OutputBlock icon={Terminal} label="Error" content={operation.error} danger />}
          {operation.policy_note && operation.policy_note.includes('\n') && (
            <OutputBlock icon={ShieldAlert} label="Policy" content={operation.policy_note} />
          )}
          {operation.plan && <OutputBlock icon={ScrollText} label="Plan output" content={operation.plan} diff />}
          {operation.log && operation.log !== operation.plan && (
            <OutputBlock icon={ScrollText} label="Log" content={operation.log} diff />
          )}
          {!operation.error && !operation.plan && !operation.log && (
            <p className="text-sm text-muted">No output available for this operation.</p>
          )}
        </div>
      )}
    </div>
  );
}

interface OutputBlockProps {
  icon: typeof Terminal;
  label: string;
  content: string;
  diff?: boolean;
  danger?: boolean;
}

function OutputBlock({ icon: Icon, label, content, diff, danger }: OutputBlockProps) {
  return (
    <div>
      <p
        className={cn(
          'mb-1.5 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide',
          danger ? 'text-danger' : 'text-muted'
        )}
      >
        <Icon className="h-3.5 w-3.5" />
        {label}
      </p>
      <CodeBlock
        content={content}
        diff={diff}
        className={cn('max-h-64', danger && 'border-danger/30 bg-danger-soft text-danger-ink')}
      />
    </div>
  );
}

export default OperationsPage;
