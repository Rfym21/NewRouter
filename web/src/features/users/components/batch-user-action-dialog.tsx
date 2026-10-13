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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  Alert02Icon,
  EyeIcon,
  UserMinus01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useMutation } from '@tanstack/react-query'
import { useState, type ChangeEvent, type ReactNode } from 'react'
import { useForm, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { handleServerError } from '@/lib/handle-server-error'
import { useAuthStore } from '@/stores/auth-store'

import { batchUserAction } from '../api'
import {
  BATCH_USER_ACTION_DEFAULT_VALUES,
  batchUserActionFormSchema,
  createBatchUserActionPayload,
  type BatchUserActionFormValues,
} from '../lib/user-batch-actions'
import type { BatchUserAction, BatchUserActionPayload } from '../types'

type QuotaRangeFieldName = keyof BatchUserActionFormValues

interface QuotaRangeFieldsProps {
  form: UseFormReturn<BatchUserActionFormValues>
  label: string
  minimumName: QuotaRangeFieldName
  maximumName: QuotaRangeFieldName
  step: number
  disabled: boolean
  onFilterChange: () => void
}

function QuotaRangeFields(props: QuotaRangeFieldsProps) {
  const { t } = useTranslation()
  const minimumId = `batch-user-${props.minimumName}`
  const maximumId = `batch-user-${props.maximumName}`
  const minimumRegistration = props.form.register(props.minimumName, {
    setValueAs: (value: string) => (value === '' ? undefined : Number(value)),
  })
  const maximumRegistration = props.form.register(props.maximumName, {
    setValueAs: (value: string) => (value === '' ? undefined : Number(value)),
  })
  const minimumError = props.form.formState.errors[props.minimumName]?.message
  const maximumError = props.form.formState.errors[props.maximumName]?.message

  const handleChange = (
    event: ChangeEvent<HTMLInputElement>,
    registration: typeof minimumRegistration
  ) => {
    void registration.onChange(event)
    props.onFilterChange()
  }

  return (
    <FieldSet className='gap-2' disabled={props.disabled}>
      <FieldLegend variant='label'>{props.label}</FieldLegend>
      <FieldGroup className='grid gap-3 sm:grid-cols-2'>
        <Field
          data-disabled={props.disabled}
          data-invalid={Boolean(minimumError)}
        >
          <FieldLabel htmlFor={minimumId}>{t('Minimum')}</FieldLabel>
          <Input
            {...minimumRegistration}
            id={minimumId}
            type='number'
            min={0}
            step={props.step}
            disabled={props.disabled}
            placeholder={t('No minimum')}
            aria-invalid={Boolean(minimumError)}
            onChange={(event) => handleChange(event, minimumRegistration)}
          />
          <FieldError>{minimumError ? t(minimumError) : undefined}</FieldError>
        </Field>
        <Field
          data-disabled={props.disabled}
          data-invalid={Boolean(maximumError)}
        >
          <FieldLabel htmlFor={maximumId}>{t('Maximum')}</FieldLabel>
          <Input
            {...maximumRegistration}
            id={maximumId}
            type='number'
            min={0}
            step={props.step}
            disabled={props.disabled}
            placeholder={t('No maximum')}
            aria-invalid={Boolean(maximumError)}
            onChange={(event) => handleChange(event, maximumRegistration)}
          />
          <FieldError>{maximumError ? t(maximumError) : undefined}</FieldError>
        </Field>
      </FieldGroup>
    </FieldSet>
  )
}

interface BatchUserActionDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

interface PendingBatchAction {
  action: BatchUserAction
  payload: BatchUserActionPayload
}

/**
 * Render quota-filtered preview, disable, and deregistration controls.
 *
 * @param props - Dialog state and refresh callbacks.
 * @returns The batch action dialog and its destructive confirmation.
 */
export function BatchUserActionDialog(props: BatchUserActionDialogProps) {
  const { t } = useTranslation()
  const currentUser = useAuthStore((state) => state.auth.user)
  const canManage = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.USER,
    ADMIN_PERMISSION_ACTIONS.MANAGE
  )
  const canDelete =
    canManage &&
    hasPermission(
      currentUser,
      ADMIN_PERMISSION_RESOURCES.USER,
      ADMIN_PERMISSION_ACTIONS.DELETE
    )
  const [matched, setMatched] = useState<number | null>(null)
  const [pendingAction, setPendingAction] = useState<PendingBatchAction | null>(
    null
  )
  const form = useForm<BatchUserActionFormValues>({
    resolver: zodResolver(batchUserActionFormSchema),
    defaultValues: BATCH_USER_ACTION_DEFAULT_VALUES,
  })
  const mutation = useMutation({
    meta: { errorToast: false },
    mutationFn: (payload: BatchUserActionPayload) => {
      if (
        !canManage ||
        (payload.action === 'cancel' && !payload.dry_run && !canDelete)
      ) {
        throw new Error(t('No permission to perform this action'))
      }
      return batchUserAction(payload)
    },
  })
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const inputStep = currencyMeta.kind === 'tokens' ? 1 : 0.000001

  const resetDialog = () => {
    form.reset(BATCH_USER_ACTION_DEFAULT_VALUES)
    mutation.reset()
    setMatched(null)
    setPendingAction(null)
  }

  const handleOpenChange = (open: boolean) => {
    if (!open && mutation.isPending) return
    if (!open) resetDialog()
    props.onOpenChange(open)
  }

  const handlePreview = form.handleSubmit(async (values) => {
    const previewValues = { ...values }
    try {
      const result = await mutation.mutateAsync(
        createBatchUserActionPayload(previewValues, 'disable', true)
      )
      if (!result.success) {
        handleServerError(result, t('Failed to preview matching users'))
        return
      }
      if (JSON.stringify(previewValues) !== JSON.stringify(form.getValues())) {
        return
      }
      setMatched(result.data?.matched ?? 0)
    } catch (error: unknown) {
      let message = t('Failed to preview matching users')
      if (error instanceof RangeError) {
        message = t(error.message)
      } else if (error instanceof Error) {
        message = error.message
      }
      handleServerError(error, undefined, { title: message })
    }
  })

  const requestAction = (action: BatchUserAction) => {
    void form.handleSubmit((values) => {
      setPendingAction({
        action,
        payload: createBatchUserActionPayload(values, action),
      })
    })()
  }

  const handleConfirmAction = async () => {
    if (!pendingAction) return

    try {
      const result = await mutation.mutateAsync(pendingAction.payload)
      if (!result.success) {
        handleServerError(result, t('Failed to perform batch user operation'))
        return
      }

      toast.success(
        t('Batch operation affected {{count}} users', {
          count: result.data?.affected ?? 0,
        })
      )
      props.onSuccess()
      resetDialog()
      props.onOpenChange(false)
    } catch (error: unknown) {
      let message = t('Failed to perform batch user operation')
      if (error instanceof RangeError) {
        message = t(error.message)
      } else if (error instanceof Error) {
        message = error.message
      }
      handleServerError(error, undefined, { title: message })
    }
  }

  const isPreviewing =
    mutation.isPending && mutation.variables?.dry_run === true
  const confirmTitle =
    pendingAction?.action === 'cancel'
      ? t('Deregister matching users?')
      : t('Disable matching users?')
  const confirmDescription =
    pendingAction?.action === 'cancel'
      ? t(
          'All regular users matching these quota ranges will be deregistered. This action cannot be undone.'
        )
      : t('All regular users matching these quota ranges will be disabled.')
  let confirmText: ReactNode = t('Disable Users')
  if (mutation.isPending) {
    confirmText = (
      <>
        <Spinner data-icon='inline-start' />
        {t('Processing...')}
      </>
    )
  } else if (pendingAction?.action === 'cancel') {
    confirmText = t('Deregister Users')
  }

  return (
    <>
      <Dialog
        open={props.open}
        onOpenChange={handleOpenChange}
        title={t('Batch User Operations')}
        description={t(
          'Disable or deregister regular users that match the quota ranges. Admin and root users are always excluded.'
        )}
        contentHeight='auto'
        contentClassName='sm:max-w-xl'
        bodyClassName='flex flex-col gap-5'
        footerClassName='sm:justify-stretch'
        footer={
          <div className='flex w-full flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
            <Badge variant={matched === null ? 'outline' : 'secondary'}>
              {matched === null
                ? t('No preview yet')
                : t('{{count}} users matched', { count: matched })}
            </Badge>
            <div className='flex flex-wrap justify-end gap-2'>
              <Button
                form='batch-user-action-form'
                type='submit'
                variant='outline'
                disabled={mutation.isPending || !canManage}
              >
                {isPreviewing ? (
                  <Spinner data-icon='inline-start' />
                ) : (
                  <HugeiconsIcon
                    icon={EyeIcon}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                )}
                {isPreviewing ? t('Previewing...') : t('Preview Matches')}
              </Button>
              <Button
                type='button'
                variant='secondary'
                disabled={mutation.isPending || !canManage}
                onClick={() => requestAction('disable')}
              >
                {t('Disable Matching')}
              </Button>
              <Button
                type='button'
                variant='destructive'
                disabled={mutation.isPending || !canDelete}
                onClick={() => requestAction('cancel')}
              >
                <HugeiconsIcon
                  icon={UserMinus01Icon}
                  data-icon='inline-start'
                  aria-hidden='true'
                />
                {t('Deregister Matching')}
              </Button>
            </div>
          </div>
        }
      >
        <Alert>
          <HugeiconsIcon icon={Alert02Icon} aria-hidden='true' />
          <AlertTitle>{t('Quota filters')}</AlertTitle>
          <AlertDescription>
            {t('Quota filters use the current display unit: {{unit}}.', {
              unit: currencyLabel,
            })}
          </AlertDescription>
        </Alert>

        <form
          id='batch-user-action-form'
          className='flex flex-col gap-5'
          onSubmit={handlePreview}
        >
          <QuotaRangeFields
            form={form}
            label={t('Used quota')}
            minimumName='used_quota_min'
            maximumName='used_quota_max'
            step={inputStep}
            disabled={mutation.isPending}
            onFilterChange={() => setMatched(null)}
          />
          <QuotaRangeFields
            form={form}
            label={t('Remaining quota')}
            minimumName='remaining_quota_min'
            maximumName='remaining_quota_max'
            step={inputStep}
            disabled={mutation.isPending}
            onFilterChange={() => setMatched(null)}
          />
          <QuotaRangeFields
            form={form}
            label={t('Total quota')}
            minimumName='total_quota_min'
            maximumName='total_quota_max'
            step={inputStep}
            disabled={mutation.isPending}
            onFilterChange={() => setMatched(null)}
          />
        </form>
      </Dialog>

      <ConfirmDialog
        open={pendingAction !== null}
        onOpenChange={(open) => {
          if (!open && !mutation.isPending) setPendingAction(null)
        }}
        title={confirmTitle}
        desc={confirmDescription}
        destructive={pendingAction?.action === 'cancel'}
        isLoading={mutation.isPending}
        disabled={
          !canManage || (pendingAction?.action === 'cancel' && !canDelete)
        }
        confirmText={confirmText}
        handleConfirm={handleConfirmAction}
      />
    </>
  )
}
