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
import {
  Add01Icon,
  Delete02Icon,
  Layers01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import { handleServerError } from '@/lib/handle-server-error'

import { purgeDeletedUsers } from '../api'
import { BatchUserActionDialog } from './batch-user-action-dialog'
import { useUsers } from './users-provider'

/**
 * Render the primary user-management actions and their dialogs.
 *
 * @returns User creation, cleanup, and quota-batch controls.
 */
export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow, triggerRefresh } = useUsers()
  const [showPurgeConfirm, setShowPurgeConfirm] = useState(false)
  const [showBatchDialog, setShowBatchDialog] = useState(false)
  const purgeMutation = useMutation({
    meta: { errorToast: false },
    mutationFn: purgeDeletedUsers,
  })

  const handleCreate = () => {
    setCurrentRow(null)
    setOpen('create')
  }

  const handlePurgeDeleted = async () => {
    try {
      const result = await purgeMutation.mutateAsync()
      if (!result.success) {
        handleServerError(result, t('Failed to clean up deregistered users'))
        return
      }

      toast.success(
        t('Cleaned up {{count}} deregistered users', {
          count: result.data?.deleted ?? 0,
        })
      )
      setShowPurgeConfirm(false)
      triggerRefresh()
    } catch (error: unknown) {
      handleServerError(error, t('Failed to clean up deregistered users'))
    }
  }

  return (
    <>
      <div className='flex flex-wrap justify-end gap-2'>
        <Button size='sm' onClick={handleCreate}>
          <HugeiconsIcon
            icon={Add01Icon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {t('Add User')}
        </Button>
        <Button
          size='sm'
          variant='destructive'
          onClick={() => setShowPurgeConfirm(true)}
        >
          <HugeiconsIcon
            icon={Delete02Icon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {t('Clean Deregistered')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          onClick={() => setShowBatchDialog(true)}
        >
          <HugeiconsIcon
            icon={Layers01Icon}
            data-icon='inline-start'
            aria-hidden='true'
          />
          {t('Batch Operations')}
        </Button>
      </div>

      <ConfirmDialog
        open={showPurgeConfirm}
        onOpenChange={setShowPurgeConfirm}
        title={t('Clean up deregistered users?')}
        desc={t(
          'This will permanently delete all deregistered user records. This action cannot be undone.'
        )}
        destructive
        isLoading={purgeMutation.isPending}
        confirmText={
          purgeMutation.isPending ? (
            <>
              <Spinner data-icon='inline-start' />
              {t('Cleaning...')}
            </>
          ) : (
            t('Clean Up')
          )
        }
        handleConfirm={handlePurgeDeleted}
      />

      <BatchUserActionDialog
        open={showBatchDialog}
        onOpenChange={setShowBatchDialog}
        onSuccess={triggerRefresh}
      />
    </>
  )
}
