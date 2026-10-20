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
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DateTimePicker } from '@/components/datetime-picker'
import { Button } from '@/components/ui/button'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE } from '@/lib/roles'
import { createServerError } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import { clearAuditLogs } from '../api'

type CleanupTarget = { timestamp: number; date: string }

export function AuditLogCleanup() {
  const { t } = useTranslation()
  const titleId = useId()
  const queryClient = useQueryClient()
  const isRoot = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  const verification = useSecureVerification()
  const cancelVerification = verification.cancel
  const operation = useRef<AbortController | null>(null)
  const [purgeDate, setPurgeDate] = useState<Date | undefined>(() => {
    const date = new Date()
    date.setDate(date.getDate() - 30)
    return date
  })
  const [target, setTarget] = useState<CleanupTarget | null>(null)
  const [isClearing, setIsClearing] = useState(false)

  useEffect(() => () => operation.current?.abort(), [])
  useEffect(() => {
    if (isRoot) return
    operation.current?.abort()
    operation.current = null
    cancelVerification()
    setTarget(null)
    setIsClearing(false)
  }, [isRoot, cancelVerification])

  const requestCleanup = () => {
    if (!isRoot || operation.current) return
    const timestamp = purgeDate
      ? Math.floor(purgeDate.getTime() / 1000)
      : Number.NaN
    if (
      !Number.isFinite(timestamp) ||
      timestamp <= 0 ||
      timestamp >= Math.floor(Date.now() / 1000)
    ) {
      toast.error(t('Select a past timestamp before clearing audit logs.'))
      return
    }
    setTarget({
      timestamp,
      date: formatTimestampToDate(timestamp, 'seconds'),
    })
  }

  const handleCleanup = async () => {
    if (!isRoot || !target || operation.current) return
    const selectedTarget = target
    const current = new AbortController()
    operation.current = current
    setTarget(null)
    setIsClearing(true)
    try {
      const proof = await verification.requestVerification({
        scope: 'audit_log.clear',
        context: { target_timestamp: selectedTarget.timestamp },
        title: t('Verify to clear audit logs'),
        description: t(
          'Confirm your identity to permanently remove audit records created before {{date}}.',
          { date: selectedTarget.date }
        ),
      })
      if (
        !proof ||
        current.signal.aborted ||
        useAuthStore.getState().auth.user?.role !== ROLE.SUPER_ADMIN
      ) {
        return
      }
      const result = await clearAuditLogs(
        selectedTarget.timestamp,
        proof.proof_token,
        current.signal
      )
      current.signal.throwIfAborted()
      if (!result.success || !result.data) {
        throw createServerError(result, t('Failed to clear audit logs'))
      }
      const count = result.data.deleted_count
      toast.success(
        count > 0
          ? t('{{count}} audit records removed.', { count })
          : t('No audit records matched the selected time.')
      )
      await queryClient.invalidateQueries({ queryKey: ['audit'] })
    } catch (error) {
      if (!current.signal.aborted) {
        handleServerError(error, t('Failed to clear audit logs'))
      }
    } finally {
      if (operation.current === current) {
        operation.current = null
        setIsClearing(false)
      }
    }
  }

  if (!isRoot) return null

  return (
    <div
      role='group'
      aria-labelledby={titleId}
      className='flex min-w-0 flex-col gap-4'
    >
      <div>
        <h4 id={titleId} className='font-medium'>
          {t('Clean audit logs')}
        </h4>
        <p className='text-muted-foreground mt-1 text-xs'>
          {t(
            'Remove audit records created before the selected timestamp. Identity verification is required.'
          )}
        </p>
      </div>
      <fieldset disabled={isClearing || target !== null} className='min-w-0'>
        <DateTimePicker value={purgeDate} onChange={setPurgeDate} />
      </fieldset>
      <div>
        <Button
          type='button'
          variant='destructive'
          onClick={requestCleanup}
          disabled={isClearing}
        >
          {isClearing ? t('Cleaning...') : t('Clear audit logs')}
        </Button>
      </div>
      <ConfirmDialog
        open={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        title={t('Confirm audit log cleanup')}
        desc={t(
          'This will permanently remove audit records created before {{date}}. This action cannot be undone.',
          { date: target?.date ?? '' }
        )}
        destructive
        confirmText={t('Continue')}
        handleConfirm={handleCleanup}
      />
      <SecureVerificationDialog {...verification.dialogProps} />
    </div>
  )
}
