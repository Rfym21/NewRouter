/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/* eslint-disable react-refresh/only-export-components */
import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from 'react'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { ChannelAffinityInfo } from '../types'

// Auto-refresh preferences persist across sessions so a user's polling choice
// survives navigation and reloads. Kept in the provider (not the stats
// component) because both the stats query and the logs list query poll on the
// same cadence.
const AUTO_REFRESH_ENABLED_KEY = 'usage-logs:auto-refresh-enabled'
const AUTO_REFRESH_SECONDS_KEY = 'usage-logs:auto-refresh-seconds'
const DEFAULT_AUTO_REFRESH_SECONDS = 10
export const MIN_AUTO_REFRESH_SECONDS = 3

function readAutoRefreshEnabled(): boolean {
  try {
    return localStorage.getItem(AUTO_REFRESH_ENABLED_KEY) === 'true'
  } catch {
    return false
  }
}

function readAutoRefreshSeconds(): number {
  try {
    const raw = Number(localStorage.getItem(AUTO_REFRESH_SECONDS_KEY))
    return Number.isFinite(raw) && raw >= MIN_AUTO_REFRESH_SECONDS
      ? Math.floor(raw)
      : DEFAULT_AUTO_REFRESH_SECONDS
  } catch {
    return DEFAULT_AUTO_REFRESH_SECONDS
  }
}

export type LogsViewScope = 'all' | 'self'

export type LogsViewAccess = 'self' | 'admin' | 'root'

export function resolveLogsViewAccess(
  role: number,
  viewScope: LogsViewScope
): LogsViewAccess {
  if (viewScope !== 'all' || role < ROLE.ADMIN) return 'self'
  return role === ROLE.SUPER_ADMIN ? 'root' : 'admin'
}

interface UsageLogsContextValue {
  selectedUserId: number | null
  setSelectedUserId: (userId: number | null) => void
  userInfoDialogOpen: boolean
  setUserInfoDialogOpen: (open: boolean) => void
  affinityTarget: ChannelAffinityInfo | null
  setAffinityTarget: (target: ChannelAffinityInfo | null) => void
  affinityDialogOpen: boolean
  setAffinityDialogOpen: (open: boolean) => void
  sensitiveVisible: boolean
  setSensitiveVisible: (visible: boolean) => void
  autoRefreshEnabled: boolean
  setAutoRefreshEnabled: (enabled: boolean) => void
  autoRefreshSeconds: number
  setAutoRefreshSeconds: (seconds: number) => void
  exportDialogOpen: boolean
  setExportDialogOpen: (open: boolean) => void
  viewScope: LogsViewScope
  setViewScope: (scope: LogsViewScope) => void
}

const UsageLogsContext = createContext<UsageLogsContextValue | undefined>(
  undefined
)

export function UsageLogsProvider({ children }: { children: ReactNode }) {
  const [selectedUserId, setSelectedUserId] = useState<number | null>(null)
  const [userInfoDialogOpen, setUserInfoDialogOpen] = useState(false)
  const [affinityTarget, setAffinityTarget] =
    useState<ChannelAffinityInfo | null>(null)
  const [affinityDialogOpen, setAffinityDialogOpen] = useState(false)
  const [sensitiveVisible, setSensitiveVisible] = useState(true)
  const [autoRefreshEnabled, setAutoRefreshEnabledState] = useState(
    readAutoRefreshEnabled
  )
  const [autoRefreshSeconds, setAutoRefreshSecondsState] = useState(
    readAutoRefreshSeconds
  )
  const [exportDialogOpen, setExportDialogOpen] = useState(false)

  const setAutoRefreshEnabled = useCallback((enabled: boolean) => {
    setAutoRefreshEnabledState(enabled)
    try {
      localStorage.setItem(AUTO_REFRESH_ENABLED_KEY, String(enabled))
    } catch {
      // Ignore storage failures (private mode / quota); state still applies.
    }
  }, [])

  const setAutoRefreshSeconds = useCallback((seconds: number) => {
    const normalized =
      Number.isFinite(seconds) && seconds >= MIN_AUTO_REFRESH_SECONDS
        ? Math.floor(seconds)
        : DEFAULT_AUTO_REFRESH_SECONDS
    setAutoRefreshSecondsState(normalized)
    try {
      localStorage.setItem(AUTO_REFRESH_SECONDS_KEY, String(normalized))
    } catch {
      // Ignore storage failures (private mode / quota); state still applies.
    }
  }, [])
  const [viewScope, setViewScope] = useState<LogsViewScope>('all')

  return (
    <UsageLogsContext.Provider
      value={{
        selectedUserId,
        setSelectedUserId,
        userInfoDialogOpen,
        setUserInfoDialogOpen,
        affinityTarget,
        setAffinityTarget,
        affinityDialogOpen,
        setAffinityDialogOpen,
        sensitiveVisible,
        setSensitiveVisible,
        autoRefreshEnabled,
        setAutoRefreshEnabled,
        autoRefreshSeconds,
        setAutoRefreshSeconds,
        exportDialogOpen,
        setExportDialogOpen,
        viewScope,
        setViewScope,
      }}
    >
      {children}
    </UsageLogsContext.Provider>
  )
}

export function useUsageLogsContext() {
  const context = useContext(UsageLogsContext)
  if (!context) {
    throw new Error('useUsageLogsContext must be used within UsageLogsProvider')
  }
  return context
}

/**
 * Resolves the effective admin scope for usage logs: whether the current
 * user is allowed to view all users' logs (`canManageScope`), and whether
 * their current view preference (`viewScope`) has that scope active
 * (`isAdminView`). Data fetching and admin-only UI should key off
 * `isAdminView` rather than raw role, so an admin who switches to "only
 * mine" is treated exactly like a regular user for that view.
 */
export function useLogsViewScope() {
  const role = useAuthStore((state) => state.auth.user?.role ?? ROLE.GUEST)
  const { viewScope, setViewScope } = useUsageLogsContext()
  const canManageScope = role >= ROLE.ADMIN
  const viewAccess = resolveLogsViewAccess(role, viewScope)
  const isAdminView = viewAccess !== 'self'
  const isRootView = viewAccess === 'root'

  return {
    canManageScope,
    viewScope,
    setViewScope,
    isAdminView,
    isRootView,
    viewAccess,
  }
}
