export type Role = 'member' | 'approver' | 'admin'

export interface RoleSources {
  /** Assigned inside TowerCrane by an admin ('' = none). */
  local: Role | ''
  /** Granted by a GitHub team mapping ('' = none). */
  github: Role | ''
  /** Listed in TOWERCRANE_ADMINS on the server. */
  bootstrap: boolean
}

const rank: Record<Role, number> = { member: 1, approver: 2, admin: 3 }

/** Whether `role` grants at least the permissions of `min`. UI-only: the backend enforces this too. */
export function atLeast(role: Role | undefined, min: Role): boolean {
  return !!role && rank[role] >= rank[min]
}

export const roleLabels: Record<Role, string> = {
  member: 'Member',
  approver: 'Approver',
  admin: 'Admin',
}

export const roleDescriptions: Record<Role, string> = {
  member: 'View everything and run plans',
  approver: 'Member + confirm applies',
  admin: 'Approver + connect repos, approve policy changes, destroy, manage team',
}
