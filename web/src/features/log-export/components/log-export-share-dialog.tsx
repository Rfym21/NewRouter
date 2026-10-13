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
import { Copy, Link2, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'

import { deleteLogExportShare, upsertLogExportShare } from '../api'
import type { LogExportJob, LogExportShare } from '../types'

interface LogExportShareDialogProps {
  job: LogExportJob | null
  management: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Refetch the records list after a share is created, updated, or revoked. */
  onChanged: () => void
}

function buildShareUrl(share: LogExportShare | null): string {
  if (!share?.download_path) return ''
  if (/^https?:\/\//i.test(share.download_path)) return share.download_path
  return `${window.location.origin}${share.download_path}`
}

export function LogExportShareDialog({
  job,
  management,
  open,
  onOpenChange,
  onChanged,
}: LogExportShareDialogProps) {
  const { t } = useTranslation()
  const [share, setShare] = useState<LogExportShare | null>(job?.share ?? null)
  const [maxDownloads, setMaxDownloads] = useState('1')
  const [requireLogin, setRequireLogin] = useState(false)
  const [saving, setSaving] = useState(false)
  const [revoking, setRevoking] = useState(false)

  // Reseed local form whenever the target job changes.
  useEffect(() => {
    setShare(job?.share ?? null)
    setMaxDownloads(String(job?.share?.max_downloads || 1))
    setRequireLogin(Boolean(job?.share?.require_login))
  }, [job?.id, job?.share])

  if (!job) return null

  const shareUrl = buildShareUrl(share)

  const handleSave = async () => {
    const parsed = Number(maxDownloads)
    if (!Number.isFinite(parsed) || parsed < 1) {
      toast.error(t('Download count must be greater than 0'))
      return
    }
    setSaving(true)
    try {
      const result = await upsertLogExportShare(
        job.id,
        { max_downloads: Math.floor(parsed), require_login: requireLogin },
        management
      )
      if (result.success && result.data) {
        setShare(result.data)
        toast.success(t('Share settings saved'))
        onChanged()
      } else {
        handleServerError(result, t('Failed to save share settings'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to save share settings'))
    } finally {
      setSaving(false)
    }
  }

  const handleRevoke = async () => {
    setRevoking(true)
    try {
      const result = await deleteLogExportShare(job.id, management)
      if (result.success) {
        setShare(null)
        toast.success(t('Share link revoked'))
        onChanged()
      } else {
        handleServerError(result, t('Failed to revoke share'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to revoke share'))
    } finally {
      setRevoking(false)
    }
  }

  const handleCopy = async () => {
    if (!shareUrl) return
    try {
      await navigator.clipboard.writeText(shareUrl)
      toast.success(t('Share link copied'))
    } catch {
      toast.error(t('Copy failed, please copy manually'))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Share Download Link')}
      description={t('Anyone with the link can download this exported file.')}
      contentClassName='sm:max-w-lg'
      footer={
        <>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {share ? t('Save share settings') : t('Create share link')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='bg-muted/40 rounded-md border px-3 py-2 text-sm'>
          <div className='font-medium'>#{job.id}</div>
          <div className='text-muted-foreground mt-0.5 text-xs break-all'>
            {job.file_name || t('Log export file')}
          </div>
        </div>

        <div className='space-y-1.5'>
          <Label className='text-xs'>{t('Download count')}</Label>
          <Input
            type='number'
            min={1}
            step={1}
            value={maxDownloads}
            disabled={saving}
            onChange={(e) => setMaxDownloads(e.target.value)}
          />
        </div>

        <label className='bg-muted/40 flex cursor-pointer items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm'>
          <span>{t('Require login to download')}</span>
          <Switch
            checked={requireLogin}
            onCheckedChange={setRequireLogin}
            disabled={saving}
            aria-label={t('Require login to download')}
          />
        </label>

        {share && (
          <div className='space-y-2 rounded-md border p-3'>
            <div className='flex items-center gap-1.5 text-sm font-medium'>
              <Link2 className='size-3.5' />
              {t('Share link')}
            </div>
            <div className='flex flex-col gap-2 sm:flex-row'>
              <Input
                readOnly
                value={shareUrl}
                onFocus={(e) => e.currentTarget.select()}
                className='flex-1'
              />
              <Button
                type='button'
                variant='outline'
                onClick={handleCopy}
                className='gap-1.5'
              >
                <Copy className='size-3.5' />
                {t('Copy link')}
              </Button>
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('Downloaded')} {share.download_count || 0} /{' '}
              {share.max_downloads || 0}
              {share.require_login ? ` · ${t('Login required')}` : ''}
            </div>
            <Button
              type='button'
              variant='destructive'
              size='sm'
              onClick={handleRevoke}
              disabled={revoking}
              className='gap-1.5'
            >
              <Trash2 className='size-3.5' />
              {t('Revoke share')}
            </Button>
          </div>
        )}
      </div>
    </Dialog>
  )
}
