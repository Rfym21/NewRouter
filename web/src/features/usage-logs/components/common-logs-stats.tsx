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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  Activity,
  Coins,
  Eye,
  EyeOff,
  Gauge,
  RefreshCw,
  Zap,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Switch } from '@/components/ui/switch'
import { formatLogQuota } from '@/lib/format'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { buildApiParams } from '../lib/utils'
import {
  MIN_AUTO_REFRESH_SECONDS,
  useLogsViewScope,
  useUsageLogsContext,
} from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

// Per-stat visibility is persisted so a user who masks a sensitive figure
// (e.g. total consumption on a shared screen) keeps it masked across reloads.
const HIDDEN_STATS_KEY = 'usage-logs:hidden-stats'

type StatKey = 'QPD' | 'QPM' | 'RPM' | 'TPM'

// TPM (tokens/minute) compact formatting: >100000 -> M (2 decimals),
// >1000 -> K (1 decimal). M switches in at 100k (0.1M) to mirror the upstream
// dashboard scheme, so it deliberately differs from Intl compact notation.
function formatTpm(n: number): string {
  if (!Number.isFinite(n)) return '0'
  if (n > 100000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n > 1000) return `${(n / 1000).toFixed(1)}K`
  return String(n)
}

function readHiddenStats(): Partial<Record<StatKey, boolean>> {
  try {
    const raw = localStorage.getItem(HIDDEN_STATS_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

function StatChip(props: {
  label: string
  value: string
  icon: React.ReactNode
  accent: string
  loading: boolean
  hidden: boolean
  onToggleHidden: () => void
}) {
  const ToggleIcon = props.hidden ? EyeOff : Eye
  const toggleLabel = props.hidden
    ? `Show ${props.label}`
    : `Hide ${props.label}`

  let content: React.ReactNode = props.value
  if (props.loading) {
    content = (
      <span className='bg-muted inline-block h-3 w-10 animate-pulse rounded align-middle' />
    )
  } else if (props.hidden) {
    content = '••••'
  }

  return (
    <span className='border-border/60 bg-muted/25 relative inline-flex h-8 items-center gap-2 rounded-md border py-0 pr-7 pl-2.5 text-xs shadow-xs'>
      <span
        className={cn(
          'inline-flex size-4 items-center justify-center',
          props.accent
        )}
      >
        {props.icon}
      </span>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {content}
      </span>
      <button
        type='button'
        onClick={props.onToggleHidden}
        aria-label={toggleLabel}
        title={toggleLabel}
        className='text-muted-foreground/70 hover:text-foreground focus-visible:ring-ring absolute top-1/2 right-1.5 inline-flex size-4 -translate-y-1/2 items-center justify-center rounded transition-colors focus-visible:ring-2 focus-visible:outline-none'
      >
        <ToggleIcon className='size-3' />
      </button>
    </span>
  )
}

export function CommonLogsStats() {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const {
    autoRefreshEnabled,
    setAutoRefreshEnabled,
    autoRefreshSeconds,
    setAutoRefreshSeconds,
  } = useUsageLogsContext()

  const [hiddenStats, setHiddenStats] =
    useState<Partial<Record<StatKey, boolean>>>(readHiddenStats)
  const [secondsText, setSecondsText] = useState(() =>
    String(autoRefreshSeconds)
  )

  const toggleHiddenStat = (key: StatKey) => {
    setHiddenStats((prev) => {
      const next = { ...prev, [key]: !prev[key] }
      try {
        localStorage.setItem(HIDDEN_STATS_KEY, JSON.stringify(next))
      } catch {
        // Ignore storage failures (private mode / quota); state still applies.
      }
      return next
    })
  }

  const commitSeconds = () => {
    const parsed = Number.parseInt(secondsText, 10)
    const normalized =
      Number.isFinite(parsed) && parsed >= MIN_AUTO_REFRESH_SECONDS
        ? parsed
        : Math.max(MIN_AUTO_REFRESH_SECONDS, autoRefreshSeconds)
    setAutoRefreshSeconds(normalized)
    setSecondsText(String(normalized))
  }

  const { data: stats, isLoading } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      })

      const result = isAdmin
        ? requireServerSuccess(await getLogStats(params))
        : requireServerSuccess(await getUserLogStats(params))

      return result.success
        ? result.data || DEFAULT_LOG_STATS
        : DEFAULT_LOG_STATS
    },
    placeholderData: (previousData) => previousData,
    refetchInterval: autoRefreshEnabled
      ? Math.max(MIN_AUTO_REFRESH_SECONDS, autoRefreshSeconds) * 1000
      : false,
  })

  const chips: Array<{
    key: StatKey
    label: string
    value: string
    icon: React.ReactNode
    accent: string
  }> = [
    {
      key: 'QPD',
      label: t('QPD'),
      value: formatLogQuota(stats?.quota || 0),
      icon: <Coins className='size-3.5' />,
      accent: 'text-sky-500',
    },
    {
      key: 'QPM',
      label: t('QPM'),
      value: formatLogQuota(stats?.qpm || 0),
      icon: <Gauge className='size-3.5' />,
      accent: 'text-violet-500',
    },
    {
      key: 'RPM',
      label: t('RPM'),
      value: String(stats?.rpm || 0),
      icon: <Activity className='size-3.5' />,
      accent: 'text-rose-500',
    },
    {
      key: 'TPM',
      label: t('TPM'),
      value: formatTpm(stats?.tpm || 0),
      icon: <Zap className='size-3.5' />,
      accent: 'text-amber-500',
    },
  ]

  return (
    <div className='flex flex-wrap items-center gap-2'>
      {chips.map((chip) => (
        <StatChip
          key={chip.key}
          label={chip.label}
          value={chip.value}
          icon={chip.icon}
          accent={chip.accent}
          loading={isLoading}
          hidden={!!hiddenStats[chip.key]}
          onToggleHidden={() => toggleHiddenStat(chip.key)}
        />
      ))}

      <span className='border-border/60 bg-muted/25 inline-flex h-8 items-center gap-1.5 rounded-md border px-2 text-xs shadow-xs'>
        <RefreshCw
          className={cn(
            'size-3.5',
            autoRefreshEnabled ? 'text-emerald-500' : 'text-muted-foreground'
          )}
        />
        <span className='text-muted-foreground'>{t('Auto Refresh')}</span>
        <Switch
          size='sm'
          checked={autoRefreshEnabled}
          onCheckedChange={setAutoRefreshEnabled}
          aria-label={t('Auto Refresh')}
        />
        <input
          type='number'
          min={MIN_AUTO_REFRESH_SECONDS}
          step={1}
          value={secondsText}
          disabled={!autoRefreshEnabled}
          onChange={(e) => setSecondsText(e.target.value)}
          onBlur={commitSeconds}
          onKeyDown={(e) => {
            if (e.key === 'Enter') e.currentTarget.blur()
          }}
          aria-label={t('Refresh Interval')}
          className='border-border/60 bg-background/60 text-foreground/85 focus-visible:ring-ring h-5 w-11 rounded border px-1 text-center font-mono tabular-nums outline-none focus-visible:ring-2 disabled:opacity-50'
        />
        <span className='text-muted-foreground'>{t('sec')}</span>
      </span>
    </div>
  )
}
