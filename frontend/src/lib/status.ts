import type { SyncStatus } from '../api/resourceMap'
import type { RolloutItemStatus, Rollout } from '../api/rollouts'
import type { Operation } from '../store/operationStore'

/**
 * Every status in the UI collapses into one of five states, each with a
 * single color: ok (green), attention (yellow: a person must act),
 * progress (purple, animated), error (red) and inert (grey).
 */
export type StatusState = 'ok' | 'attention' | 'progress' | 'error' | 'inert'

export interface StatusInfo {
  state: StatusState
  label: string
}

export const syncStatus: Record<SyncStatus, StatusInfo> = {
  synced: { state: 'ok', label: 'Synced' },
  'out-of-sync': { state: 'attention', label: 'Out of sync' },
  running: { state: 'progress', label: 'Running' },
  error: { state: 'error', label: 'Error' },
  destroyed: { state: 'inert', label: 'Destroyed' },
  unknown: { state: 'inert', label: 'Not applied yet' },
}

export const operationStatus: Record<Operation['status'], StatusInfo> = {
  pending: { state: 'progress', label: 'Pending' },
  running: { state: 'progress', label: 'Running' },
  success: { state: 'ok', label: 'Success' },
  failed: { state: 'error', label: 'Failed' },
}

export const rolloutItemStatus: Record<RolloutItemStatus, StatusInfo> = {
  unchanged: { state: 'inert', label: 'Unchanged' },
  waiting: { state: 'inert', label: 'Waiting' },
  planning: { state: 'progress', label: 'Planning' },
  awaiting_yes: { state: 'attention', label: 'Waiting for yes' },
  applying: { state: 'progress', label: 'Applying' },
  applied: { state: 'ok', label: 'Applied' },
  no_changes: { state: 'ok', label: 'No changes' },
  blocked: { state: 'error', label: 'Blocked' },
  rejected: { state: 'error', label: 'Rejected' },
  failed: { state: 'error', label: 'Failed' },
  discarded: { state: 'inert', label: 'Discarded' },
  skipped: { state: 'inert', label: 'Skipped' },
  superseded: { state: 'inert', label: 'Superseded' },
}

export const rolloutStatus: Record<Rollout['status'], StatusInfo> = {
  running: { state: 'progress', label: 'Running' },
  done: { state: 'ok', label: 'Done' },
  superseded: { state: 'inert', label: 'Superseded' },
}
