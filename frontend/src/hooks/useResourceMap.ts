import { useCallback, useEffect, useRef, useState } from 'react'
import { resourceMapAPI, type ResourceMap } from '../api/resourceMap'
import { apiError } from '../api/admin'

/**
 * Reading a repository's manifests and Terraform files goes to GitHub and
 * takes a while. The last result is kept (in memory, and in localStorage
 * across reloads) and shown right away; a fresh copy is always fetched in
 * the background (stale-while-revalidate).
 */
const memory = new Map<string, ResourceMap>()
const STORAGE_PREFIX = 'towercrane:map:'

function cacheKey(repoId: string, app?: string) {
  return `${repoId}:${app ?? ''}`
}

function readCache(key: string): ResourceMap | null {
  const hit = memory.get(key)
  if (hit) return hit
  try {
    const raw = localStorage.getItem(STORAGE_PREFIX + key)
    if (!raw) return null
    const parsed = JSON.parse(raw) as ResourceMap
    memory.set(key, parsed)
    return parsed
  } catch {
    return null
  }
}

function writeCache(key: string, map: ResourceMap) {
  memory.set(key, map)
  try {
    localStorage.setItem(STORAGE_PREFIX + key, JSON.stringify(map))
  } catch {
    // Storage full or blocked: the in-memory copy is enough.
  }
}

/** Forgets every cached map (on logout: another user may sign in next). */
export function clearResourceMapCache() {
  memory.clear()
  try {
    for (const k of Object.keys(localStorage)) {
      if (k.startsWith(STORAGE_PREFIX)) localStorage.removeItem(k)
    }
  } catch {
    // storage unavailable: nothing persisted
  }
}

export function useResourceMap(repoId: string, app?: string) {
  const key = cacheKey(repoId, app)
  const [map, setMap] = useState<ResourceMap | null>(() => readCache(key))
  const [error, setError] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)
  const latestKey = useRef(key)

  const reload = useCallback(() => {
    latestKey.current = key
    setRefreshing(true)
    resourceMapAPI
      .get(repoId, { app })
      .then((fresh) => {
        writeCache(key, fresh)
        if (latestKey.current !== key) return
        setMap(fresh)
        setError(null)
      })
      .catch((err) => {
        if (latestKey.current === key) setError(apiError(err, 'Failed to read the repository'))
      })
      .finally(() => {
        if (latestKey.current === key) setRefreshing(false)
      })
  }, [repoId, app, key])

  // Show what is cached for this key at once, then refresh it.
  useEffect(() => {
    setMap(readCache(key))
    setError(null)
    reload()
  }, [key, reload])

  return {
    map,
    error,
    /** True while a fresh copy is being fetched (map may be the cached one). */
    refreshing,
    /** Nothing to show yet: first visit and still loading. */
    loading: !map && !error,
    reload,
  }
}
