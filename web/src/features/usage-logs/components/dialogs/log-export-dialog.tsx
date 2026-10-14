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
import { getRouteApi } from '@tanstack/react-router'
import { Download } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { createLogExport } from '@/features/log-export/api'
import type { CreateLogExportPayload } from '@/features/log-export/types'
import { useStatus } from '@/hooks/use-status'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import { exportLogs } from '../../api'
import {
  downloadCsv,
  getLogCsvHeader,
  getLogCsvRow,
  toCsvLine,
} from '../../lib/export'
import { parseLogOther } from '../../lib/format'
import { buildApiParams, getDefaultTimeRange } from '../../lib/utils'
import { useLogsViewScope } from '../usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

// Column keys mirror the backend `logExportColumns`; admin-only columns are
// dropped for non-admins to match `NormalizeLogExportColumns`.
const COLUMN_DEFS: Array<{
  key: string
  labelKey: string
  adminOnly?: boolean
}> = [
  { key: 'time', labelKey: 'Time' },
  { key: 'channel', labelKey: 'Channel', adminOnly: true },
  { key: 'username', labelKey: 'User', adminOnly: true },
  { key: 'token', labelKey: 'Token' },
  { key: 'group', labelKey: 'Group' },
  { key: 'type', labelKey: 'Type' },
  { key: 'model', labelKey: 'Model' },
  { key: 'use_time', labelKey: 'Duration / First token' },
  { key: 'prompt', labelKey: 'Input' },
  { key: 'completion', labelKey: 'Output' },
  { key: 'cost', labelKey: 'Cost' },
  { key: 'retry', labelKey: 'Retry', adminOnly: true },
  { key: 'ip', labelKey: 'IP' },
  { key: 'details', labelKey: 'Details' },
  { key: 'stream', labelKey: 'Stream' },
  { key: 'cache_read', labelKey: 'Cache Read' },
  { key: 'cache_write', labelKey: 'Cache Write' },
  { key: 'request_id', labelKey: 'Request ID' },
]

const QUICK_RANGES: Array<{ labelKey: string; seconds: number }> = [
  { labelKey: '24 hours', seconds: 24 * 3600 },
  { labelKey: '7 days', seconds: 7 * 24 * 3600 },
  { labelKey: '30 days', seconds: 30 * 24 * 3600 },
]

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

/** Format a Date into a value accepted by <input type="datetime-local">. */
function toLocalInput(date: Date): string {
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function toSeconds(value: string): number {
  const ms = Date.parse(value)
  return Number.isNaN(ms) ? 0 : Math.floor(ms / 1000)
}

interface LogExportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function LogExportDialog({ open, onOpenChange }: LogExportDialogProps) {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const searchParams = route.useSearch()
  const { status } = useStatus()

  const maxDays = useMemo(() => {
    const raw = Number(
      (status as unknown as { log_export_max_days?: number } | null)
        ?.log_export_max_days
    )
    return Math.min(
      Number.isFinite(raw) && raw >= 1 ? Math.floor(raw) : 30,
      status?.log_max_range_days || 365
    )
  }, [status])
  const maxRangeSeconds = maxDays * 24 * 3600

  const columns = useMemo(
    () => COLUMN_DEFS.filter((c) => isAdmin || !c.adminOnly),
    [isAdmin]
  )

  const [start, setStart] = useState(() =>
    toLocalInput(
      searchParams.startTime
        ? new Date(searchParams.startTime)
        : getDefaultTimeRange().start
    )
  )
  const [end, setEnd] = useState(() =>
    toLocalInput(
      searchParams.endTime
        ? new Date(searchParams.endTime)
        : getDefaultTimeRange().end
    )
  )
  const [selected, setSelected] = useState<string[]>(() =>
    COLUMN_DEFS.filter((c) => !c.adminOnly).map((c) => c.key)
  )
  const [submitting, setSubmitting] = useState(false)
  const [exportedCount, setExportedCount] = useState<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const handleOpenChange = (next: boolean) => {
    if (!next) abortRef.current?.abort()
    onOpenChange(next)
  }

  const handleDownload = async () => {
    if (rangeError || exportedCount !== null) return
    const controller = new AbortController()
    abortRef.current = controller
    setExportedCount(0)
    // Keep the official CSV formatting and protect spreadsheet formula cells.
    const keys = [
      'time',
      'type',
      ...(isAdmin ? ['username'] : []),
      'token',
      'group',
      'model',
      ...(isAdmin ? ['channel'] : []),
      'stream',
      'prompt',
      'completion',
      'cache_read',
      'cache_write',
      'cost',
      'use_time',
      'ip',
      'request_id',
      'details',
      'retry',
    ]
    const indices = keys.flatMap((key, i) =>
      selected.includes(key) && (key !== 'retry' || isAdmin) ? [i] : []
    )
    const lines = [
      toCsvLine(
        indices.map((i) => [...getLogCsvHeader(isAdmin, t), t('Retry')][i])
      ),
    ]
    try {
      const count = await exportLogs(
        {
          ...buildApiParams({ page: 1, pageSize: 1, searchParams, isAdmin }),
          p: undefined,
          page_size: undefined,
          start_timestamp: toSeconds(start),
          end_timestamp: toSeconds(end),
        },
        isAdmin,
        {
          signal: controller.signal,
          onLog: (log) => {
            const row = [
              ...getLogCsvRow(log, isAdmin, t),
              (
                parseLogOther(log.other ?? '')?.admin_info?.use_channel ?? []
              ).join('->'),
            ]
            lines.push(toCsvLine(indices.map((i) => row[i])))
            if (lines.length % 500 === 0) setExportedCount(lines.length - 1)
          },
        }
      )
      downloadCsv(`usage-logs-${toSeconds(start)}-${toSeconds(end)}.csv`, lines)
      toast.success(t('Exported {{count}} logs', { count }))
      handleOpenChange(false)
    } catch (error) {
      if (!controller.signal.aborted) {
        handleServerError(error, t('Failed to export logs'))
      }
    } finally {
      abortRef.current = null
      setExportedCount(null)
    }
  }

  const allChecked =
    columns.length > 0 && columns.every((c) => selected.includes(c.key))
  const someChecked = columns.some((c) => selected.includes(c.key))

  const rangeError = useMemo(() => {
    const startTs = toSeconds(start)
    const endTs = toSeconds(end)
    if (!startTs || !endTs) return t('Please select a valid export time range')
    if (endTs <= startTs) return t('End time must be later than start time')
    if (endTs - startTs > maxRangeSeconds) {
      return t('Export time range cannot exceed {{days}} days', {
        days: maxDays,
      })
    }
    return ''
  }, [start, end, maxRangeSeconds, maxDays, t])

  const applyQuickRange = (seconds: number) => {
    const now = new Date()
    setStart(toLocalInput(new Date(now.getTime() - seconds * 1000)))
    setEnd(toLocalInput(now))
  }

  const toggleColumn = (key: string, checked: boolean) => {
    setSelected((prev) => {
      if (checked) return prev.includes(key) ? prev : [...prev, key]
      return prev.filter((k) => k !== key)
    })
  }

  const toggleAll = (checked: boolean) => {
    setSelected(checked ? columns.map((c) => c.key) : [])
  }

  const handleSubmit = async () => {
    if (rangeError) {
      toast.error(rangeError)
      return
    }
    const activeColumns = columns
      .filter((c) => selected.includes(c.key))
      .map((c) => c.key)
    if (activeColumns.length === 0) {
      toast.error(t('Please select at least one export column'))
      return
    }

    const payload: CreateLogExportPayload = {
      ...buildApiParams({ page: 1, pageSize: 1, searchParams, isAdmin }),
      scope: isAdmin ? 'all' : 'self',
      start_timestamp: toSeconds(start),
      end_timestamp: toSeconds(end),
      columns: activeColumns,
    }
    if (isAdmin) {
      payload.username = searchParams.username || undefined
      payload.channel = searchParams.channel
        ? Number(searchParams.channel) || 0
        : 0
    }

    setSubmitting(true)
    try {
      const result = await createLogExport(payload)
      if (result.success) {
        toast.success(
          t('Export task created, download it from the export records page')
        )
        onOpenChange(false)
      } else {
        handleServerError(result, t('Failed to create export task'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to create export task'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleOpenChange}
      title={t('Export Usage Logs')}
      description={t(
        'Create a background task that exports the filtered logs as a compressed CSV file.'
      )}
      contentClassName='sm:max-w-2xl'
      footer={
        <>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            variant='outline'
            onClick={handleDownload}
            disabled={
              submitting ||
              exportedCount !== null ||
              !!rangeError ||
              selected.length === 0
            }
          >
            {t('Download CSV')}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={
              submitting ||
              exportedCount !== null ||
              !!rangeError ||
              selected.length === 0
            }
            className='gap-1.5'
          >
            <Download className='size-4' />
            {submitting ? t('Exporting') : t('Export')}
          </Button>
        </>
      }
    >
      <div className='space-y-5'>
        {exportedCount !== null && (
          <p aria-live='polite'>
            {t('Exported {{count}} logs', { count: exportedCount })}
          </p>
        )}
        <section className='space-y-3'>
          <div className='flex items-center justify-between'>
            <div className='text-sm font-semibold'>{t('Export time')}</div>
            <div className='text-muted-foreground text-xs'>
              {t('Up to {{days}} days', { days: maxDays })}
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <span className='text-muted-foreground text-xs'>
              {t('Quick select')}
            </span>
            {QUICK_RANGES.map((range) => {
              const disabled = range.seconds > maxRangeSeconds
              return (
                <Button
                  key={range.labelKey}
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={disabled}
                  onClick={() => applyQuickRange(range.seconds)}
                >
                  {t(range.labelKey)}
                </Button>
              )
            })}
          </div>
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label className='text-xs'>{t('Start time')}</Label>
              <input
                type='datetime-local'
                value={start}
                max={end || undefined}
                onChange={(e) => setStart(e.target.value)}
                className='border-input bg-background focus-visible:ring-ring h-9 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2'
              />
            </div>
            <div className='space-y-1.5'>
              <Label className='text-xs'>{t('End time')}</Label>
              <input
                type='datetime-local'
                value={end}
                min={start || undefined}
                onChange={(e) => setEnd(e.target.value)}
                className='border-input bg-background focus-visible:ring-ring h-9 w-full rounded-md border px-3 text-sm outline-none focus-visible:ring-2'
              />
            </div>
          </div>
          {rangeError && (
            <div role='alert' className='text-destructive text-xs'>
              {rangeError}
            </div>
          )}
        </section>

        <section className='space-y-2'>
          <div className='flex items-center justify-between'>
            <div className='text-sm font-semibold'>{t('Export columns')}</div>
            <label className='flex cursor-pointer items-center gap-2 text-xs'>
              <Checkbox
                checked={allChecked}
                indeterminate={someChecked && !allChecked}
                onCheckedChange={(checked) => toggleAll(checked === true)}
              />
              {t('Select all')}
            </label>
          </div>
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {columns.map((column) => {
              const checked = selected.includes(column.key)
              return (
                <label
                  key={column.key}
                  className={cn(
                    'flex cursor-pointer items-center gap-2 rounded-md border px-2.5 py-2 text-sm transition-colors',
                    checked
                      ? 'border-primary/40 bg-muted/60'
                      : 'border-transparent hover:bg-muted/40'
                  )}
                >
                  <Checkbox
                    checked={checked}
                    onCheckedChange={(value) =>
                      toggleColumn(column.key, value === true)
                    }
                  />
                  <span className='whitespace-nowrap'>
                    {t(column.labelKey)}
                  </span>
                </label>
              )
            })}
          </div>
        </section>
      </div>
    </Dialog>
  )
}
