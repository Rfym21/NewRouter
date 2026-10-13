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
import { Loader2, Wallet } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { adjustUserQuota } from '@/features/users/api'
import type { QuotaAdjustMode } from '@/features/users/types'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import {
  formatQuota,
  formatCompactNumber,
  parseQuotaFromDollars,
} from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { ROLE, getRoleLabel } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getUserInfo } from '../../api'
import type { UserInfo } from '../../types'

interface UserInfoDialogProps {
  userId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function roleVariant(role?: number): StatusBadgeProps['variant'] {
  if ((role ?? 0) >= ROLE.SUPER_ADMIN) return 'orange'
  if ((role ?? 0) >= ROLE.ADMIN) return 'blue'
  return 'neutral'
}

const QUOTA_ADJUST_MODES: readonly QuotaAdjustMode[] = [
  'add',
  'subtract',
  'override',
]

function quotaModeLabel(mode: QuotaAdjustMode, t: (key: string) => string) {
  if (mode === 'add') return t('Add')
  if (mode === 'subtract') return t('Subtract')
  return t('Override')
}

function InfoItem({ label, value }: { label: string; value: string | number }) {
  return (
    <div className='space-y-1.5'>
      <Label className='text-muted-foreground text-xs'>{label}</Label>
      <div className='text-sm font-semibold'>{value}</div>
    </div>
  )
}

/**
 * Admin-only quota adjustment panel. Rendered inside the user dialog for super
 * admins so a log reviewer can correct a user's balance without leaving the
 * logs view. Mirrors the standalone user-quota-dialog contract
 * (/api/user/manage, action=add_quota).
 */
function QuotaAdjustPanel(props: {
  userId: number
  currentQuota: number
  onAdjusted: () => void
}) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)
  const currencyLabel = getCurrencyLabel()
  const { meta: currencyMeta } = getCurrencyDisplay()
  const tokensOnly = currencyMeta.kind === 'tokens'

  const amountValue = Number.parseFloat(amount) || 0
  const quotaValue = parseQuotaFromDollars(Math.abs(amountValue))

  const previewText = (() => {
    const current = props.currentQuota
    switch (mode) {
      case 'add':
        return `${t('Current quota')}: ${formatQuota(current)}  +${formatQuota(quotaValue)} = ${formatQuota(current + quotaValue)}`
      case 'subtract':
        return `${t('Current quota')}: ${formatQuota(current)}  -${formatQuota(quotaValue)} = ${formatQuota(current - quotaValue)}`
      case 'override': {
        const overrideQuota = parseQuotaFromDollars(amountValue)
        return `${t('Current quota')}: ${formatQuota(current)} → ${formatQuota(overrideQuota)}`
      }
      default:
        return ''
    }
  })()

  const handleConfirm = async () => {
    if (!amount && mode !== 'override') return
    if (quotaValue <= 0 && mode !== 'override') return

    setLoading(true)
    try {
      const value =
        mode === 'override' ? parseQuotaFromDollars(amountValue) : quotaValue
      const result = await adjustUserQuota({
        id: props.userId,
        action: 'add_quota',
        mode,
        value: mode === 'override' ? value : Math.abs(value),
      })
      if (result.success) {
        toast.success(t('Quota adjusted successfully'))
        setAmount('')
        setMode('add')
        props.onAdjusted()
      } else {
        handleServerError(result, t('Failed to adjust quota'))
      }
    } catch (e: unknown) {
      handleServerError(e, t('Failed to adjust quota'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <Label className='flex items-center gap-1.5 text-xs font-semibold'>
          <Wallet className='size-3.5' aria-hidden='true' />
          {t('Adjust Quota')}
        </Label>
        <Button size='sm' onClick={handleConfirm} disabled={loading}>
          {loading ? t('Processing...') : t('Confirm')}
        </Button>
      </div>

      <div className='text-muted-foreground text-xs'>{previewText}</div>

      <div className='flex flex-wrap gap-1'>
        {QUOTA_ADJUST_MODES.map((m) => (
          <Button
            key={m}
            type='button'
            variant='outline'
            size='sm'
            className={cn(
              mode === m &&
                'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
            )}
            onClick={() => {
              setMode(m)
              setAmount('')
            }}
          >
            {quotaModeLabel(m, t)}
          </Button>
        ))}
      </div>

      <Input
        type='number'
        step={tokensOnly ? 1 : 0.000001}
        min={mode === 'override' ? undefined : 0}
        placeholder={
          tokensOnly
            ? t('Enter amount in tokens')
            : t('Enter amount in {{currency}}', { currency: currencyLabel })
        }
        value={amount}
        onChange={(e) => setAmount(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') handleConfirm()
        }}
      />
    </div>
  )
}

export function UserInfoDialog({
  userId,
  open,
  onOpenChange,
}: UserInfoDialogProps) {
  const { t } = useTranslation()
  const [userInfo, setUserInfo] = useState<UserInfo | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const { user: currentUser } = useAuthStore((state) => state.auth)
  const isSuperAdmin = (currentUser?.role ?? 0) >= ROLE.SUPER_ADMIN

  const fetchUserInfo = useCallback(
    async (id: number) => {
      setIsLoading(true)
      try {
        const result = await getUserInfo(id)
        if (result.success) {
          setUserInfo(result.data || null)
        } else {
          handleServerError(result, t('Failed to fetch user information'))
        }
      } catch (error) {
        handleServerError(error, t('Failed to fetch user information'))
      } finally {
        setIsLoading(false)
      }
    },
    [t]
  )

  useEffect(() => {
    if (open && userId) {
      fetchUserInfo(userId)
    }
  }, [open, userId, fetchUserInfo])

  let dialogBody = (
    <div className='text-muted-foreground py-8 text-center text-sm'>
      {t('No user information available')}
    </div>
  )

  if (isLoading) {
    dialogBody = (
      <div className='flex items-center justify-center py-8'>
        <Loader2 className='text-muted-foreground size-6 animate-spin' />
      </div>
    )
  } else if (userInfo) {
    dialogBody = (
      <div className='space-y-4 py-4'>
        {/* Identity tags: id, status, role, email */}
        <div className='flex flex-wrap items-center gap-1.5'>
          <StatusBadge
            label={`ID: ${userInfo.id}`}
            variant='neutral'
            size='sm'
            copyable={false}
          />
          {userInfo.status != null && (
            <StatusBadge
              label={userInfo.status === 1 ? t('Enabled') : t('Disabled')}
              variant={userInfo.status === 1 ? 'green' : 'red'}
              size='sm'
              copyable={false}
            />
          )}
          {userInfo.role != null && (
            <StatusBadge
              label={getRoleLabel(userInfo.role)}
              variant={roleVariant(userInfo.role)}
              size='sm'
              copyable={false}
            />
          )}
          {userInfo.email && (
            <StatusBadge
              label={userInfo.email}
              variant='neutral'
              size='sm'
              copyable={false}
            />
          )}
        </div>

        {/* Basic Info */}
        <div className='grid grid-cols-2 gap-4'>
          <InfoItem label={t('Username')} value={userInfo.username} />
          {userInfo.display_name && (
            <InfoItem label={t('Display Name')} value={userInfo.display_name} />
          )}
        </div>

        {/* Balance Info */}
        <div className='grid grid-cols-2 gap-4'>
          <InfoItem label={t('Balance')} value={formatQuota(userInfo.quota)} />
          <InfoItem
            label={t('Used Quota')}
            value={formatQuota(userInfo.used_quota)}
          />
        </div>

        {/* Statistics */}
        <div className='grid grid-cols-2 gap-4'>
          <InfoItem
            label={t('Request Count')}
            value={formatCompactNumber(userInfo.request_count)}
          />
          {userInfo.group && (
            <InfoItem label={t('User Group')} value={userInfo.group} />
          )}
        </div>

        {/* Invitation Info */}
        {(userInfo.aff_code ||
          userInfo.aff_count !== undefined ||
          (userInfo.aff_quota !== undefined && userInfo.aff_quota > 0)) && (
          <>
            <div className='grid grid-cols-2 gap-4'>
              {userInfo.aff_code && (
                <InfoItem
                  label={t('Invitation Code')}
                  value={userInfo.aff_code}
                />
              )}
              {userInfo.aff_count !== undefined && (
                <InfoItem
                  label={t('Invited Users')}
                  value={formatCompactNumber(userInfo.aff_count)}
                />
              )}
            </div>

            {userInfo.aff_quota !== undefined && userInfo.aff_quota > 0 && (
              <InfoItem
                label={t('Invitation Quota')}
                value={formatQuota(userInfo.aff_quota)}
              />
            )}
          </>
        )}

        {/* Remark */}
        {userInfo.remark && (
          <div className='space-y-1.5'>
            <Label className='text-muted-foreground text-xs'>
              {t('Remark')}
            </Label>
            <div className='text-sm leading-relaxed font-semibold break-words'>
              {userInfo.remark}
            </div>
          </div>
        )}

        {/* Admin management panel (super admin only) */}
        {isSuperAdmin && userInfo.id > 0 && (
          <QuotaAdjustPanel
            userId={userInfo.id}
            currentQuota={userInfo.quota}
            onAdjusted={() => fetchUserInfo(userInfo.id)}
          />
        )}
      </div>
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('User Information')}
      description={t(
        'View detailed information about this user including balance, usage statistics, and invitation details.'
      )}
      contentClassName='sm:max-w-lg'
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      {dialogBody}
    </Dialog>
  )
}
