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
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import {
  Clock,
  Download,
  FileArchive,
  Loader2,
  RefreshCw,
  Share2,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { downloadBlob } from '@/lib/download'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import {
  batchDeleteManagedLogExports,
  deleteLogExport,
  downloadLogExportFile,
  extendManagedLogExportsExpiration,
  listLogExports,
} from './api'
import { LogExportShareDialog } from './components/log-export-share-dialog'
import type { LogExportJob } from './types'

const PAGE_SIZE = 10

function formatFileSize(size: number): string {
  const bytes = Number(size || 0)
  if (!Number.isFinite(bytes) || bytes <= 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) {
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  }
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatTs(ts: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

function getProgress(job: LogExportJob): string {
  const total = Number(job.total || 0)
  const exported = Number(job.exported || 0)
  if (total <= 0) return exported > 0 ? String(exported) : '-'
  return `${Math.min(exported, total)} / ${total}`
}

function statusMeta(
  status: string,
  t: (key: string) => string
): { label: string; variant: StatusBadgeProps['variant'] } {
  switch (status) {
    case 'pending':
      return { label: t('Pending'), variant: 'amber' }
    case 'running':
      return { label: t('Generating'), variant: 'blue' }
    case 'completed':
      return { label: t('Completed'), variant: 'green' }
    case 'failed':
      return { label: t('Failed'), variant: 'red' }
    case 'expired':
      return { label: t('Expired'), variant: 'grey' }
    default:
      return { label: status || t('Unknown'), variant: 'grey' }
  }
}

function InfoItem({ label, value }: { label: string; value: string }) {
  return (
    <div className='bg-muted/40 rounded-md px-3 py-2'>
      <div className='text-muted-foreground text-[11px]'>{label}</div>
      <div className='mt-0.5 text-sm font-medium break-words'>{value}</div>
    </div>
  )
}

export function LogExportPage({
  management = false,
}: {
  management?: boolean
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [downloadingId, setDownloadingId] = useState<number | null>(null)
  const [deletingId, setDeletingId] = useState<number | null>(null)
  const [shareJob, setShareJob] = useState<LogExportJob | null>(null)
  const [shareOpen, setShareOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<LogExportJob | null>(null)
  const [batchDeleteOpen, setBatchDeleteOpen] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [extendHours, setExtendHours] = useState('24')
  const [batchAction, setBatchAction] = useState<'' | 'delete' | 'extend'>('')

  const query = useQuery({
    queryKey: ['log-exports', management, page],
    queryFn: async () =>
      requireServerSuccess(
        await listLogExports({ p: page, page_size: PAGE_SIZE }, management)
      ),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => {
      const jobs = q.state.data?.data?.items ?? []
      return jobs.some(
        (job) => job.status === 'pending' || job.status === 'running'
      )
        ? 5000
        : false
    },
  })

  const jobs = useMemo(() => query.data?.data?.items ?? [], [query.data])
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const refetch = useCallback(() => {
    void query.refetch()
  }, [query])

  // Keep selection scoped to the jobs currently visible on this page.
  const visibleIds = useMemo(() => jobs.map((job) => job.id), [jobs])
  useEffect(() => {
    if (!management) return
    setSelectedIds((prev) => prev.filter((id) => visibleIds.includes(id)))
  }, [management, visibleIds])

  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds])
  const allSelected =
    visibleIds.length > 0 && visibleIds.every((id) => selectedSet.has(id))
  const someSelected = visibleIds.some((id) => selectedSet.has(id))
  const batchBusy = batchAction !== ''

  const toggleSelect = (id: number, checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (checked) next.add(id)
      else next.delete(id)
      return [...next]
    })
  }

  const toggleSelectAll = (checked: boolean) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      visibleIds.forEach((id) => {
        if (checked) next.add(id)
        else next.delete(id)
      })
      return [...next]
    })
  }

  const handleDownload = async (job: LogExportJob) => {
    setDownloadingId(job.id)
    try {
      const { blob, filename } = await downloadLogExportFile(job.id, management)
      downloadBlob(
        blob,
        filename || job.file_name || `usage-logs-${job.id}.csv.gz`
      )
    } catch (e: unknown) {
      handleServerError(e, t('Download failed'))
    } finally {
      setDownloadingId(null)
    }
  }

  const handleDelete = async () => {
    if (!pendingDelete) return
    setDeletingId(pendingDelete.id)
    try {
      const result = await deleteLogExport(pendingDelete.id, management)
      if (result.success) {
        toast.success(t('Deleted successfully'))
        setSelectedIds((prev) => prev.filter((id) => id !== pendingDelete.id))
        refetch()
      } else {
        handleServerError(result, t('Delete failed'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Delete failed'))
    } finally {
      setDeletingId(null)
      setPendingDelete(null)
    }
  }

  const handleBatchDelete = async () => {
    if (selectedIds.length === 0) return
    setBatchAction('delete')
    try {
      const result = await batchDeleteManagedLogExports(selectedIds)
      if (result.success) {
        toast.success(
          t('Deleted {{count}} export records', {
            count: result.data?.count ?? selectedIds.length,
          })
        )
        setSelectedIds([])
        refetch()
      } else {
        handleServerError(result, t('Delete failed'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Delete failed'))
    } finally {
      setBatchAction('')
      setBatchDeleteOpen(false)
    }
  }

  const handleExtend = async () => {
    if (selectedIds.length === 0) {
      toast.error(t('Please select export records to operate on'))
      return
    }
    const hours = Number(extendHours)
    if (!Number.isFinite(hours) || hours < 1) {
      toast.error(t('Extension time must be greater than 0 hours'))
      return
    }
    setBatchAction('extend')
    try {
      const result = await extendManagedLogExportsExpiration(
        selectedIds,
        Math.floor(hours)
      )
      if (result.success) {
        toast.success(
          t('Extended expiration for {{count}} export records', {
            count: result.data?.count ?? selectedIds.length,
          })
        )
        refetch()
      } else {
        handleServerError(result, t('Failed to extend expiration'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to extend expiration'))
    } finally {
      setBatchAction('')
    }
  }

  const openShare = (job: LogExportJob) => {
    setShareJob(job)
    setShareOpen(true)
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {management ? t('Export Management') : t('Log Export')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={refetch}
            disabled={query.isFetching}
            className='gap-1.5'
          >
            <RefreshCw
              className={cn('size-4', query.isFetching && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-3'>
            {management && jobs.length > 0 && (
              <div className='bg-card/50 flex flex-col gap-3 rounded-lg border p-3 xl:flex-row xl:items-center xl:justify-between'>
                <label className='flex cursor-pointer items-center gap-2 text-sm'>
                  <Checkbox
                    checked={allSelected}
                    indeterminate={someSelected && !allSelected}
                    onCheckedChange={(checked) =>
                      toggleSelectAll(checked === true)
                    }
                  />
                  {t('Select current page')}
                  <span className='text-muted-foreground'>
                    ({t('{{count}} selected', { count: selectedIds.length })})
                  </span>
                </label>
                <div className='flex flex-wrap items-center gap-2'>
                  <div className='flex items-center gap-1.5 text-sm'>
                    <span className='text-muted-foreground'>{t('Extend')}</span>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={extendHours}
                      disabled={batchBusy}
                      onChange={(e) => setExtendHours(e.target.value)}
                      className='h-8 w-20'
                    />
                    <span className='text-muted-foreground'>{t('hours')}</span>
                  </div>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={handleExtend}
                    disabled={batchBusy || selectedIds.length === 0}
                    className='gap-1.5'
                  >
                    <Clock className='size-3.5' />
                    {batchAction === 'extend'
                      ? t('Processing...')
                      : t('Extend expiration')}
                  </Button>
                  <Button
                    variant='destructive'
                    size='sm'
                    onClick={() => setBatchDeleteOpen(true)}
                    disabled={batchBusy || selectedIds.length === 0}
                    className='gap-1.5'
                  >
                    <Trash2 className='size-3.5' />
                    {batchAction === 'delete'
                      ? t('Processing...')
                      : t('Batch delete')}
                  </Button>
                </div>
              </div>
            )}

            {jobs.length === 0 ? (
              <div className='text-muted-foreground flex flex-col items-center justify-center gap-2 rounded-lg border py-16 text-sm'>
                <FileArchive className='size-8 opacity-40' />
                {query.isLoading ? t('Loading...') : t('No export records')}
              </div>
            ) : (
              jobs.map((job) => {
                const meta = statusMeta(job.status, t)
                const canDownload = job.status === 'completed'
                return (
                  <section
                    key={job.id}
                    className='bg-card/50 rounded-lg border p-4'
                  >
                    <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
                      <div className='min-w-0 flex-1'>
                        <div className='mb-3 flex flex-wrap items-center gap-2'>
                          {management && (
                            <Checkbox
                              checked={selectedSet.has(job.id)}
                              onCheckedChange={(checked) =>
                                toggleSelect(job.id, checked === true)
                              }
                              aria-label={t('Select export record')}
                            />
                          )}
                          <StatusBadge
                            label={meta.label}
                            variant={meta.variant}
                            size='sm'
                            copyable={false}
                          />
                          <span className='text-sm font-medium'>#{job.id}</span>
                          {management && (
                            <span className='text-muted-foreground text-xs'>
                              {job.username
                                ? `${job.username} (#${job.user_id})`
                                : `#${job.user_id}`}
                            </span>
                          )}
                        </div>

                        <div className='grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-4'>
                          <InfoItem
                            label={t('Export range')}
                            value={`${formatTs(job.start_timestamp)} - ${formatTs(job.end_timestamp)}`}
                          />
                          <InfoItem
                            label={t('Records')}
                            value={getProgress(job)}
                          />
                          <InfoItem
                            label={t('File size')}
                            value={formatFileSize(job.file_size)}
                          />
                          <InfoItem
                            label={t('Expires at')}
                            value={formatTs(job.expires_at)}
                          />
                          {job.share && (
                            <InfoItem
                              label={t('Share download')}
                              value={`${job.share.download_count || 0} / ${job.share.max_downloads || 0}${
                                job.share.require_login
                                  ? ` · ${t('Login required')}`
                                  : ''
                              }`}
                            />
                          )}
                        </div>
                        {job.error && (
                          <div className='text-destructive bg-destructive/10 mt-3 rounded-md px-3 py-2 text-xs'>
                            {job.error}
                          </div>
                        )}
                      </div>

                      <div className='flex flex-wrap gap-2 lg:shrink-0'>
                        <Button
                          size='sm'
                          onClick={() => handleDownload(job)}
                          disabled={!canDownload || downloadingId === job.id}
                          className='gap-1.5'
                        >
                          {downloadingId === job.id ? (
                            <Loader2 className='size-3.5 animate-spin' />
                          ) : (
                            <Download className='size-3.5' />
                          )}
                          {t('Download')}
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={() => openShare(job)}
                          disabled={!canDownload}
                          className='gap-1.5'
                        >
                          <Share2 className='size-3.5' />
                          {job.share ? t('Share settings') : t('Share')}
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={() => setPendingDelete(job)}
                          disabled={deletingId === job.id}
                          className='gap-1.5'
                        >
                          <Trash2 className='size-3.5' />
                          {t('Delete')}
                        </Button>
                      </div>
                    </div>
                  </section>
                )
              })
            )}

            {total > PAGE_SIZE && (
              <div className='flex items-center justify-center gap-3 pt-2'>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={page <= 1 || query.isFetching}
                >
                  {t('Previous')}
                </Button>
                <span className='text-muted-foreground text-sm'>
                  {t('Page {{page}} of {{total}}', { page, total: totalPages })}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
                  disabled={page >= totalPages || query.isFetching}
                >
                  {t('Next')}
                </Button>
              </div>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <LogExportShareDialog
        job={shareJob}
        management={management}
        open={shareOpen}
        onOpenChange={setShareOpen}
        onChanged={refetch}
      />

      <ConfirmDialog
        open={pendingDelete !== null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null)
        }}
        title={t('Delete export record')}
        desc={t('Are you sure you want to delete this export record?')}
        destructive
        confirmText={t('Delete')}
        isLoading={deletingId !== null}
        handleConfirm={handleDelete}
      />

      <ConfirmDialog
        open={batchDeleteOpen}
        onOpenChange={setBatchDeleteOpen}
        title={t('Batch delete')}
        desc={t(
          'Are you sure you want to delete the {{count}} selected export records?',
          {
            count: selectedIds.length,
          }
        )}
        destructive
        confirmText={t('Delete')}
        isLoading={batchAction === 'delete'}
        handleConfirm={handleBatchDelete}
      />
    </>
  )
}
