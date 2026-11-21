import { useCallback, useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'
import axios from 'axios'
import dagre from '@dagrejs/dagre'
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  Handle,
  Position,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Box, Database, FileCode2, Lock, Network, Package, RefreshCw, Settings2, X, EyeOff, Pencil } from 'lucide-react'
import { resourceMapAPI, type MapApplication, type MapNode, type MapNodeKind, type ResourceMap, type SyncStatus } from '../api/resourceMap'
import { Layout } from '../components/Layout'
import { RefreshButton } from '../components/RefreshButton'
import { Card } from '../components/ui/Card'
import { Button, ButtonLink } from '../components/ui/Button'
import { Badge } from '../components/ui/Badge'
import { Alert } from '../components/ui/Alert'
import { Spinner } from '../components/ui/Spinner'
import { EmptyState } from '../components/ui/EmptyState'
import { PageHeader } from '../components/ui/PageHeader'
import { MetaLine } from '../components/ui/MetaLine'
import { StatusBadge, StatusDot } from '../components/ui/StatusBadge'
import { CodeBlock } from '../components/ui/CodeBlock'
import { ApplicationMeta } from '../components/ApplicationMeta'
import { syncStatus } from '../lib/status'
import { cn } from '../lib/utils'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api/v1'

const NODE_WIDTH = 250
const NODE_HEIGHT = 76

// Graph colors match the design tokens in src/index.css (React Flow needs literal values).
const COLOR_PRIMARY = '#5B21B6'
const COLOR_MUTED = '#5E5873'
const COLOR_SUCCESS = '#1F7A4D'
const COLOR_LINE = '#E6E1F2'

const kindMeta: Record<MapNodeKind, { icon: typeof Box; label: string; iconBg: string; minimap: string }> = {
  module: { icon: Package, label: 'Module', iconBg: 'bg-primary-soft text-primary', minimap: COLOR_PRIMARY },
  resource: { icon: Box, label: 'Resource', iconBg: 'bg-subtle text-ink', minimap: COLOR_MUTED },
  data: { icon: Database, label: 'Data source', iconBg: 'bg-success-soft text-success-ink', minimap: COLOR_SUCCESS },
}

interface ResourceNodeData extends Record<string, unknown> {
  node: MapNode
  dimmed: boolean
  selected: boolean
}

function ResourceNode({ data }: NodeProps<Node<ResourceNodeData>>) {
  const { node, dimmed, selected } = data
  const meta = kindMeta[node.kind]
  const Icon = meta.icon

  return (
    <div
      className={cn(
        'rounded-lg border bg-surface px-3 py-2 shadow-card transition-opacity',
        node.applications.length > 0 ? 'border-line-strong' : 'border-dashed border-line-strong bg-canvas',
        selected && 'border-primary ring-2 ring-primary/30',
        dimmed && 'opacity-30'
      )}
      style={{ width: NODE_WIDTH, height: NODE_HEIGHT }}
    >
      <Handle type="target" position={Position.Left} className="!border-line-strong !bg-surface" />
      <div className="flex items-start gap-2">
        <span className={cn('mt-0.5 flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md', meta.iconBg)}>
          <Icon className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-xs font-semibold text-ink" title={node.address}>
            {node.label}
          </p>
          <p className="truncate text-[11px] text-muted" title={node.file_path}>
            {node.file_path}
          </p>
          <div className="mt-1 flex gap-1">
            {node.applications.length === 0 ? (
              <span className="inline-flex items-center gap-0.5 rounded bg-subtle px-1.5 text-[10px] font-semibold text-muted">
                <EyeOff className="h-2.5 w-2.5" /> no Application
              </span>
            ) : node.editable ? (
              <span className="inline-flex items-center gap-0.5 rounded bg-primary-soft px-1.5 text-[10px] font-semibold text-primary-ink">
                <Pencil className="h-2.5 w-2.5" /> editable
              </span>
            ) : (
              <span className="inline-flex items-center gap-0.5 rounded bg-danger-soft px-1.5 text-[10px] font-semibold text-danger-ink">
                <Lock className="h-2.5 w-2.5" /> locked
              </span>
            )}
          </div>
        </div>
      </div>
      <Handle type="source" position={Position.Right} className="!border-line-strong !bg-surface" />
    </div>
  )
}

const nodeTypes = { resource: ResourceNode }

// layout runs dagre over the map so the graph reads left-to-right:
// dependencies on the left, the things that consume them on the right.
function layout(map: ResourceMap, selectedId: string | null) {
  const mapNodes = map.nodes ?? []
  const mapEdges = map.edges ?? []

  const g = new dagre.graphlib.Graph()
  g.setGraph({ rankdir: 'LR', nodesep: 24, ranksep: 90, marginx: 20, marginy: 20 })
  g.setDefaultEdgeLabel(() => ({}))
  mapNodes.forEach((n) => g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT }))
  mapEdges.forEach((e) => g.setEdge(e.from, e.to))
  dagre.layout(g)

  const neighbours = new Set<string>()
  if (selectedId) {
    neighbours.add(selectedId)
    mapEdges.forEach((e) => {
      if (e.from === selectedId) neighbours.add(e.to)
      if (e.to === selectedId) neighbours.add(e.from)
    })
  }

  const nodes: Node<ResourceNodeData>[] = mapNodes.map((n) => {
    const pos = g.node(n.id)
    return {
      id: n.id,
      type: 'resource',
      position: { x: pos.x - NODE_WIDTH / 2, y: pos.y - NODE_HEIGHT / 2 },
      data: { node: n, selected: n.id === selectedId, dimmed: !!selectedId && !neighbours.has(n.id) },
    }
  })

  const edges: Edge[] = mapEdges.map((e, i) => {
    const contains = e.kind === 'contains'
    const active = !selectedId || e.from === selectedId || e.to === selectedId
    const color = contains ? COLOR_PRIMARY : COLOR_MUTED
    return {
      id: `${e.from}->${e.to}-${i}`,
      source: e.from,
      target: e.to,
      animated: !contains && active && !!selectedId,
      style: {
        stroke: color,
        strokeWidth: 1.5,
        strokeDasharray: contains ? '6 4' : undefined,
        opacity: active ? 0.85 : 0.15,
      },
      markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
    }
  })

  return { nodes, edges }
}

export function ResourceMapPage() {
  const { repo_id } = useParams<{ repo_id: string }>()
  const repositoryId = repo_id ?? ''

  const [repoName, setRepoName] = useState<string>('')
  const [map, setMap] = useState<ResourceMap | null>(null)
  const [appFilter, setAppFilter] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!repositoryId) return
    axios
      .get(`${API_URL}/repositories`, {
        headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}` },
      })
      .then((res) => {
        const repo = (res.data.repositories || []).find((r: { id?: string }) => r.id === repositoryId)
        if (repo) setRepoName(repo.full_name)
      })
      .catch(() => undefined)
  }, [repositoryId])

  const load = useCallback(async () => {
    if (!repositoryId) return
    setLoading(true)
    setError(null)
    try {
      const result = await resourceMapAPI.get(repositoryId, { app: appFilter || undefined })
      setMap(result)
    } catch (err) {
      const message = axios.isAxiosError(err) ? err.response?.data?.error : undefined
      setError(message || 'Failed to load the resource map')
    } finally {
      setLoading(false)
    }
  }, [repositoryId, appFilter])

  useEffect(() => {
    load()
  }, [load])

  const applications = map?.applications ?? []
  const counts = useMemo(() => {
    const c = { module: 0, resource: 0, data: 0 }
    ;(map?.nodes ?? []).forEach((n) => c[n.kind]++)
    return c
  }, [map])

  return (
    <Layout>
      <PageHeader
        back={{ to: `/projects/${repositoryId}`, label: 'Project' }}
        title="Resource map"
        meta={
          <MetaLine
            items={[
              repoName,
              map?.branch,
              map && `${counts.module} modules · ${counts.resource} resources · ${counts.data} data sources`,
            ]}
          />
        }
        actions={
          <>
            <ButtonLink to={`/projects/${repositoryId}/operations`}>
              <Settings2 className="h-4 w-4" />
              Operations
            </ButtonLink>
            <Button variant="ghost" onClick={load} loading={loading} title="Reload the map from GitHub">
              {!loading && <RefreshCw className="h-4 w-4" />}
              Reload map
            </Button>
            <RefreshButton repoId={repositoryId} onProcessing={load} />
          </>
        }
      />

      {error && <Alert className="mb-6">{error}</Alert>}

      {(map?.manifest_errors?.length ?? 0) > 0 && (
        <Alert tone="attention" className="mb-4">
          Some Application manifests are invalid and were ignored: {map!.manifest_errors!.join(' · ')}
        </Alert>
      )}

      {applications.length > 0 ? (
        <div className="mb-4 flex gap-2 overflow-x-auto pb-1" role="group" aria-label="Filter by Application">
          <FilterChip active={appFilter === ''} onClick={() => setAppFilter('')} label="Whole repository" />
          {applications.map((app) => (
            <FilterChip
              key={app.name}
              active={appFilter === app.name}
              onClick={() => setAppFilter(app.name)}
              label={app.name}
              status={app.status}
            />
          ))}
        </div>
      ) : (
        map && (
          <p className="mb-4 text-sm text-muted">
            No Applications yet: add a manifest in <code className="font-mono text-ink">.towercrane/</code> to let TowerCrane
            manage an environment.
          </p>
        )
      )}

      {loading && !map ? (
        <Spinner label="Parsing Terraform files..." />
      ) : !error && (map?.nodes?.length ?? 0) === 0 ? (
        <EmptyState
          icon={Network}
          title="No Terraform blocks found"
          description="TowerCrane didn't find any module, resource or data block on this branch."
        />
      ) : map ? (
        <MapCanvas map={map} />
      ) : null}
    </Layout>
  )
}

/** The interactive resource graph plus the details panel of the selected node. */
export function MapCanvas({ map }: { map: ResourceMap }) {
  const [selectedId, setSelectedId] = useState<string | null>(null)
  useEffect(() => setSelectedId(null), [map])

  const { nodes, edges } = useMemo(() => layout(map, selectedId), [map, selectedId])
  const selected = useMemo(() => map.nodes?.find((n) => n.id === selectedId) ?? null, [map, selectedId])
  const appsByName = useMemo(() => new Map((map.applications ?? []).map((a) => [a.name, a])), [map])

  return (
    <div className="flex flex-col gap-4 lg:flex-row">
      <Card className="relative h-[68vh] min-h-[420px] flex-1 overflow-hidden bg-canvas">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          colorMode="light"
          fitView
          fitViewOptions={{ padding: 0.2 }}
          minZoom={0.2}
          nodesDraggable={false}
          nodesConnectable={false}
          onNodeClick={(_, n) => setSelectedId((cur) => (cur === n.id ? null : n.id))}
          onPaneClick={() => setSelectedId(null)}
          proOptions={{ hideAttribution: true }}
          style={{ background: 'transparent' }}
        >
          <Background color={COLOR_LINE} gap={20} />
          <Controls showInteractive={false} />
          <MiniMap
            pannable
            zoomable
            maskColor="rgba(247, 245, 252, 0.7)"
            nodeColor={(n) => kindMeta[(n.data as ResourceNodeData).node.kind].minimap}
            style={{ background: '#FFFFFF', border: `1px solid ${COLOR_LINE}` }}
          />
        </ReactFlow>
        <Legend />
      </Card>

      {selected && (
        <DetailsPanel
          node={selected}
          apps={selected.applications.map((n) => appsByName.get(n)).filter((a): a is MapApplication => !!a)}
          onClose={() => setSelectedId(null)}
        />
      )}
    </div>
  )
}

function FilterChip({
  active,
  onClick,
  label,
  status,
}: {
  active: boolean
  onClick: () => void
  label: string
  status?: SyncStatus
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'flex h-8 flex-shrink-0 items-center gap-2 rounded-full border px-3 font-mono text-xs font-semibold transition',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary',
        active ? 'border-primary bg-primary text-white' : 'border-line-strong bg-surface text-ink hover:border-primary/50'
      )}
    >
      {status && <StatusDot {...syncStatus[status]} className={cn('h-2 w-2', active && 'ring-2 ring-white')} />}
      {label}
    </button>
  )
}

function Legend() {
  return (
    <div className="pointer-events-none absolute left-3 top-3 z-10 flex flex-wrap gap-3 rounded-lg border border-line bg-surface/90 px-3 py-2 text-[11px] text-muted shadow-card backdrop-blur">
      <span className="flex items-center gap-1.5">
        <span className="h-0 w-5 border-t-2 border-dashed border-primary" /> contains
      </span>
      <span className="flex items-center gap-1.5">
        <span className="h-0 w-5 border-t-2 border-muted" /> depends on
      </span>
      <span className="flex items-center gap-1">
        <Lock className="h-3 w-3 text-danger" /> not editable
      </span>
      <span className="flex items-center gap-1">
        <EyeOff className="h-3 w-3" /> not in any Application
      </span>
    </div>
  )
}

function DetailsPanel({ node, apps, onClose }: { node: MapNode; apps: MapApplication[]; onClose: () => void }) {
  const meta = kindMeta[node.kind]
  const Icon = meta.icon

  return (
    <Card className="w-full flex-shrink-0 animate-fade-in lg:w-96">
      <div className="flex items-start justify-between gap-2 border-b border-line px-5 py-4">
        <div className="flex min-w-0 items-center gap-2">
          <span className={cn('flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md', meta.iconBg)}>
            <Icon className="h-4 w-4" />
          </span>
          <div className="min-w-0">
            <p className="text-xs uppercase tracking-wide text-muted">{meta.label}</p>
            <p className="break-all font-mono text-sm font-semibold text-ink">{node.address}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close details"
          className="rounded p-1 text-muted hover:bg-subtle hover:text-ink"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
      <dl className="space-y-4 px-5 py-4 text-sm">
        <div>
          <dt className="text-xs text-muted">File</dt>
          <dd className="mt-1 flex items-center gap-1.5 break-all font-mono text-ink">
            <FileCode2 className="h-3.5 w-3.5 flex-shrink-0 text-muted" />
            {node.file_path}
          </dd>
        </div>
        <div>
          <dt className="text-xs text-muted">Applications</dt>
          <dd className="mt-1 space-y-2">
            {apps.length === 0 ? (
              <Badge tone="neutral">Not part of any Application</Badge>
            ) : (
              apps.map((app) => (
                <div key={app.name} className="space-y-1">
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="font-mono text-xs font-semibold text-ink">{app.name}</span>
                    <StatusBadge {...syncStatus[app.status ?? 'unknown']} />
                  </div>
                  <ApplicationMeta app={app} showPath={false} />
                </div>
              ))
            )}
          </dd>
        </div>
        {apps.length > 0 && (
          <div>
            <dt className="text-xs text-muted">Edit scope</dt>
            <dd className="mt-1">
              {node.editable ? (
                <Badge tone="primary">
                  <Pencil className="h-3 w-3" /> Editable through TowerCrane
                </Badge>
              ) : (
                <Badge tone="danger">
                  <Lock className="h-3 w-3" /> Locked by the Application's editScope
                </Badge>
              )}
            </dd>
          </div>
        )}
        <div>
          <dt className="text-xs text-muted">Source{node.start_line ? ` · lines ${node.start_line}–${node.end_line}` : ''}</dt>
          <dd className="mt-1">
            {node.source ? (
              <CodeBlock content={node.source} className="max-h-72 text-[11px]" />
            ) : (
              <p className="flex items-start gap-1.5 rounded-md border border-dashed border-line-strong px-3 py-2 text-xs text-muted">
                <EyeOff className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
                Code hidden: set display.showSource: true in the Application manifest to show it.
              </p>
            )}
          </dd>
        </div>
      </dl>
    </Card>
  )
}
