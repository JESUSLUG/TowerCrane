import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import dagre from '@dagrejs/dagre'
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Check, Clock, Hourglass, MessageCircle, Pause, X } from 'lucide-react'
import type { DiagramColor, DiagramSpec, MapApplication } from '../api/resourceMap'
import { rolloutAPI, type Rollout, type RolloutItem } from '../api/rollouts'
import { cn } from '../lib/utils'
import { GroupTag, splitInstance } from './InstanceName'

// React Flow needs literal colors. Diagram colors are the user's (data), so
// they are a fixed palette rather than design tokens.
const PALETTE: Record<DiagramColor, string> = {
  red: '#DC2626',
  orange: '#EA580C',
  yellow: '#CA8A04',
  green: '#16A34A',
  teal: '#0D9488',
  blue: '#2563EB',
  purple: '#7C3AED',
  pink: '#DB2777',
  gray: '#6B7280',
}
const DEFAULT_NODE = '#5B21B6' // primary
const COLOR_LINE = '#E6E1F2'
const COLOR_EDGE = '#8B84A3'

const NODE_W = 190
const NODE_H = 64
const SECTION_PAD = 28

/** Live state of a service in the latest rollout, as the diagram shows it. */
type Live = 'running' | 'yes' | 'ok' | 'failed' | 'waiting' | 'paused' | 'none'

function liveOf(item?: RolloutItem): Live {
  if (!item) return 'none'
  if (item.paused && item.status === 'waiting') return 'paused'
  switch (item.status) {
    case 'planning':
    case 'applying':
      return 'running'
    case 'awaiting_yes':
      return 'yes'
    case 'applied':
    case 'no_changes':
      return 'ok'
    case 'failed':
    case 'blocked':
    case 'rejected':
      return 'failed'
    case 'waiting':
      return 'waiting'
    default:
      return 'none'
  }
}

interface ServiceData extends Record<string, unknown> {
  label: string
  color: string
  live: Live
  message?: string
  /** Group of an Application of another group this one depends on. */
  externalGroup?: string
  /** Ephemeral: applied and destroyed by a Job. */
  job?: string
}

function ServiceNode({ data }: NodeProps<Node<ServiceData>>) {
  const { label, color, live, message, externalGroup, job } = data
  return (
    <div className="relative">
      {live === 'yes' && (
        // The "needs a yes" speech bubble.
        <div className="absolute -top-11 left-1/2 z-10 -translate-x-1/2 whitespace-nowrap rounded-lg border border-attention-line bg-attention-soft px-2.5 py-1 text-[11px] font-semibold text-attention-ink shadow-card">
          <span className="inline-flex items-center gap-1">
            <MessageCircle className="h-3 w-3" aria-hidden="true" /> Needs a yes
          </span>
          <span className="absolute -bottom-1.5 left-1/2 h-3 w-3 -translate-x-1/2 rotate-45 border-b border-r border-attention-line bg-attention-soft" />
        </div>
      )}
      <div
        className={cn(
          'flex cursor-pointer items-center gap-2.5 rounded-xl border-2 bg-surface px-3 shadow-card transition hover:shadow-pop',
          live === 'running' && 'ring-2 ring-primary/30',
          externalGroup && 'border-dashed opacity-80'
        )}
        style={{ width: NODE_W, height: NODE_H, borderColor: color, background: `${color}0f` }}
        title={message}
      >
        <Handle type="target" position={Position.Left} className="!border-0 !bg-transparent" />
        <span className="h-8 w-1.5 flex-shrink-0 rounded-full" style={{ background: color }} aria-hidden="true" />
        <span className="min-w-0 flex-1">
          <span className="block truncate font-mono text-sm font-semibold text-ink">{label}</span>
          {externalGroup && <GroupTag group={externalGroup} className="mt-0.5" />}
          {job && (
            <span className="mt-0.5 inline-flex items-center gap-1 text-[11px] font-semibold text-muted" title={`Run by Job ${job}`}>
              <Clock className="h-3 w-3" aria-hidden="true" /> job
            </span>
          )}
        </span>
        <LiveIcon live={live} />
        <Handle type="source" position={Position.Right} className="!border-0 !bg-transparent" />
      </div>
    </div>
  )
}

function LiveIcon({ live }: { live: Live }) {
  switch (live) {
    case 'running':
      return <Clock className="h-4 w-4 flex-shrink-0 animate-spin text-primary [animation-duration:2.5s]" aria-label="Running" />
    case 'yes':
      return <Hourglass className="h-4 w-4 flex-shrink-0 text-attention-ink" aria-label="Waiting for a yes" />
    case 'ok':
      return <Check className="h-4 w-4 flex-shrink-0 text-success" aria-label="Applied" />
    case 'failed':
      return <X className="h-4 w-4 flex-shrink-0 text-danger" aria-label="Failed" />
    case 'paused':
      return <Pause className="h-4 w-4 flex-shrink-0 text-muted" aria-label="Paused" />
    case 'waiting':
      return <span className="h-2 w-2 flex-shrink-0 rounded-full bg-muted/50" aria-label="Waiting" />
    default:
      return null
  }
}

interface SectionData extends Record<string, unknown> {
  name: string
  color: string
  width: number
  height: number
}

function SectionNode({ data }: NodeProps<Node<SectionData>>) {
  return (
    <div
      className="relative rounded-2xl border-2 border-dashed"
      style={{ width: data.width, height: data.height, borderColor: `${data.color}66`, background: `${data.color}0a` }}
    >
      <span
        className="absolute -top-3 left-4 rounded-full px-2.5 py-0.5 text-xs font-semibold text-white"
        style={{ background: data.color }}
      >
        {data.name}
      </span>
    </div>
  )
}

const nodeTypes = { service: ServiceNode, section: SectionNode }

/** Base name of an instance ("ecr@sandbox" -> "ecr"). */
const baseName = (instance: string) => instance.split('@')[0]

/**
 * A group drawn from TowerCrane's own data: its services, their dependsOn
 * (solid arrows, the real apply order) and the Diagram manifest's sections,
 * colors and links (dashed, visual only). The latest rollout animates it: a
 * running service shows a clock, one that needs a yes a speech bubble.
 */
export function GroupDiagram({
  repoId,
  apps,
  spec,
}: {
  repoId: string
  apps: MapApplication[]
  spec: DiagramSpec | null
}) {
  const navigate = useNavigate()
  const [rollout, setRollout] = useState<Rollout | null>(null)

  const loadRollout = useCallback(() => {
    rolloutAPI
      .list(repoId)
      .then((list) => setRollout(list[0] ?? null))
      .catch(() => undefined)
  }, [repoId])
  useEffect(loadRollout, [loadRollout])
  useEffect(() => {
    if (rollout?.status !== 'running') return
    const t = setInterval(loadRollout, 3000)
    return () => clearInterval(t)
  }, [rollout?.status, loadRollout])

  const liveByInstance = useMemo(() => new Map((rollout?.items ?? []).map((i) => [i.application, i])), [rollout])

  const { nodes, edges } = useMemo(
    () => buildGraph(apps, spec, liveByInstance),
    [apps, spec, liveByInstance]
  )

  return (
    <div className="h-[440px] overflow-hidden rounded-xl border border-line bg-surface">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.25 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        proOptions={{ hideAttribution: true }}
        onNodeClick={(_, node) => {
          if (node.type === 'service') navigate(`/projects/${repoId}/apps/${encodeURIComponent(node.id)}`)
        }}
      >
        <Background color={COLOR_LINE} gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

function buildGraph(
  apps: MapApplication[],
  spec: DiagramSpec | null,
  live: Map<string, RolloutItem>
): { nodes: Node[]; edges: Edge[] } {
  const byBase = new Map(apps.map((a) => [a.app, a]))
  const sectionOf = new Map<string, number>()
  ;(spec?.sections ?? []).forEach((s, i) => s.apps.forEach((a) => byBase.has(a) && sectionOf.set(a, i)))

  const g = new dagre.graphlib.Graph({ compound: true })
  g.setGraph({ rankdir: 'LR', nodesep: 50, ranksep: 110, marginx: 20, marginy: 20 })
  g.setDefaultEdgeLabel(() => ({}))
  ;(spec?.sections ?? []).forEach((_, i) => g.setNode(`section-${i}`, {}))
  for (const a of apps) {
    g.setNode(a.name, { width: NODE_W, height: NODE_H })
    const s = sectionOf.get(a.app)
    if (s !== undefined) g.setParent(a.name, `section-${s}`)
  }

  const edges: Edge[] = []
  const inGroup = new Set(apps.map((a) => a.name))
  // Dependencies on another group's Applications (dependsOn: [vpc@general])
  // are drawn as dashed nodes tagged with their group.
  const external = new Set<string>()
  for (const a of apps) {
    for (const dep of a.depends_on) {
      if (!inGroup.has(dep) && !external.has(dep)) {
        external.add(dep)
        g.setNode(dep, { width: NODE_W, height: NODE_H })
      }
    }
  }
  for (const a of apps) {
    for (const dep of a.depends_on) {
      g.setEdge(dep, a.name)
      const target = liveOf(live.get(a.name))
      edges.push({
        id: `dep-${dep}-${a.name}`,
        source: dep,
        target: a.name,
        animated: target === 'running',
        style: { stroke: COLOR_EDGE, strokeWidth: 1.75 },
        markerEnd: { type: MarkerType.ArrowClosed, color: COLOR_EDGE },
      })
    }
  }
  for (const [i, l] of (spec?.links ?? []).entries()) {
    const from = byBase.get(l.from)
    const to = byBase.get(l.to)
    if (!from || !to) continue
    g.setEdge(from.name, to.name)
    edges.push({
      id: `link-${i}`,
      source: from.name,
      target: to.name,
      label: l.label,
      labelStyle: { fontSize: 11, fill: COLOR_EDGE },
      labelBgStyle: { fill: '#ffffff' },
      style: { stroke: COLOR_EDGE, strokeWidth: 1.5, strokeDasharray: '6 5' },
      markerEnd: { type: MarkerType.ArrowClosed, color: COLOR_EDGE },
    })
  }

  dagre.layout(g)

  const nodes: Node[] = []
  ;(spec?.sections ?? []).forEach((s, i) => {
    const members = apps.filter((a) => sectionOf.get(a.app) === i)
    if (members.length === 0) return
    const xs = members.map((a) => g.node(a.name).x)
    const ys = members.map((a) => g.node(a.name).y)
    const left = Math.min(...xs) - NODE_W / 2 - SECTION_PAD
    const top = Math.min(...ys) - NODE_H / 2 - SECTION_PAD - 8
    const width = Math.max(...xs) - Math.min(...xs) + NODE_W + SECTION_PAD * 2
    const height = Math.max(...ys) - Math.min(...ys) + NODE_H + SECTION_PAD * 2 + 8
    nodes.push({
      id: `section-${i}`,
      type: 'section',
      position: { x: left, y: top },
      data: { name: s.name, color: PALETTE[s.color ?? 'gray'] ?? PALETTE.gray, width, height },
      zIndex: -1,
      selectable: false,
      draggable: false,
    })
  })
  for (const a of apps) {
    const p = g.node(a.name)
    const style = spec?.nodes?.[a.app]
    const item = live.get(a.name)
    nodes.push({
      id: a.name,
      type: 'service',
      position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 },
      data: {
        label: style?.label || baseName(a.name),
        color: style?.color ? PALETTE[style.color] ?? DEFAULT_NODE : DEFAULT_NODE,
        live: liveOf(item),
        message: item?.message,
        job: a.lifecycle === 'job' ? a.job || 'job' : undefined,
      },
    })
  }
  for (const dep of external) {
    const p = g.node(dep)
    const { app, group } = splitInstance(dep)
    const item = live.get(dep)
    nodes.push({
      id: dep,
      type: 'service',
      position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 },
      data: { label: app, color: PALETTE.gray, live: liveOf(item), message: item?.message, externalGroup: group },
    })
  }
  return { nodes, edges }
}
