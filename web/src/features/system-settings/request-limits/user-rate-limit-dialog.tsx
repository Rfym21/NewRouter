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
import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import {
  isValidUserRateLimitID,
  USER_DEFAULT_RATE_LIMIT_GROUP,
  type UserRateLimitEntry,
} from './user-rate-limit-config'

const USER_RATE_LIMIT_FORM_ID = 'user-rate-limit-form'

type UserRateLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: UserRateLimitEntry) => boolean
  editData?: UserRateLimitEntry | null
}

const createUserRateLimitDialogSchema = (t: (key: string) => string) =>
  z.object({
    userId: z
      .string()
      .trim()
      .refine(isValidUserRateLimitID, t('User ID must be a positive integer')),
    groupName: z.string().trim().min(1, t('Group name is required')),
    maxRequests: z
      .number()
      .int(t('Must be an integer'))
      .min(0, t('Must be at least 0'))
      .max(2_147_483_647, t('Must be no more than 2,147,483,647')),
    maxSuccess: z
      .number()
      .int(t('Must be an integer'))
      .min(1, t('Must be at least 1'))
      .max(2_147_483_647, t('Must be no more than 2,147,483,647')),
  })

/**
 * Renders the add/edit dialog for a user-specific rate-limit rule.
 * @param props Dialog state, existing rule, and save callbacks.
 * @returns The user rate-limit dialog.
 */
export function UserRateLimitDialog(props: UserRateLimitDialogProps) {
  const { t } = useTranslation()
  const isEditMode = props.editData !== null && props.editData !== undefined
  const schema = useMemo(() => createUserRateLimitDialogSchema(t), [t])

  const form = useForm<UserRateLimitEntry>({
    resolver: zodResolver(schema),
    defaultValues: {
      userId: '',
      groupName: USER_DEFAULT_RATE_LIMIT_GROUP,
      maxRequests: 0,
      maxSuccess: 1000,
    },
  })

  useEffect(() => {
    if (!props.open) return
    form.reset(
      props.editData ?? {
        userId: '',
        groupName: USER_DEFAULT_RATE_LIMIT_GROUP,
        maxRequests: 0,
        maxSuccess: 1000,
      }
    )
  }, [form, props.editData, props.open])

  const handleSubmit = (values: UserRateLimitEntry) => {
    if (!props.onSave(values)) return
    props.onOpenChange(false)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={isEditMode ? t('Edit user rate limit') : t('Add user rate limit')}
      description={t(
        'Configure a rate-limit override for one user and optional group.'
      )}
      contentClassName='sm:max-w-[520px]'
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={USER_RATE_LIMIT_FORM_ID}>
            {isEditMode ? t('Update') : t('Add')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={USER_RATE_LIMIT_FORM_ID}
          onSubmit={form.handleSubmit(handleSubmit)}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='userId'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('User ID')}</FormLabel>
                <FormControl>
                  <Input
                    inputMode='numeric'
                    maxLength={19}
                    placeholder={t('e.g., 123')}
                    {...field}
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>
                  {t('Positive numeric ID of the user to override.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='groupName'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Group Name')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('* or e.g., vip')}
                    {...field}
                    disabled={isEditMode}
                  />
                </FormControl>
                <FormDescription>
                  {t("Use * for this user's default rule.")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='maxRequests'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Max Requests (including failures)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={2_147_483_647}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(
                          Number.isNaN(event.target.valueAsNumber)
                            ? 0
                            : event.target.valueAsNumber
                        )
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Total requests allowed per period. 0 = unlimited.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='maxSuccess'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max Successful Requests')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={2_147_483_647}
                      step={1}
                      {...field}
                      onChange={(event) =>
                        field.onChange(
                          Number.isNaN(event.target.valueAsNumber)
                            ? 1
                            : event.target.valueAsNumber
                        )
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Only successful requests count toward this limit.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </form>
      </Form>
    </Dialog>
  )
}
