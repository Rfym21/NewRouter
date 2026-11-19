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
import { useId, useMemo } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

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
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { AuditLogCleanup } from './audit-log-cleanup'
import {
  parseClientRules,
  serializeClientRules,
  validateClientRules,
} from './client-rules'
import { ClientRulesEditor } from './client-rules-editor'
import { HistoryLogCleanup } from './history-log-cleanup'
import { LogExportFields } from './log-export-section'
import { ServerLogFiles } from './server-log-files'

const logSettingsSchema = z.object({
  LogConsumeEnabled: z.boolean(),
  AuditLogEnabled: z.boolean(),
  AdminAuditMode: z.string(),
  LogExportFileRetentionHours: z.number().int().min(1).max(8760),
  log_setting: z.object({
    user_export_enabled: z.boolean(),
    max_range_days: z.number().int().min(1),
    client_rules: z
      .array(
        z.object({ key: z.string(), name: z.string(), keywords: z.string() })
      )
      .superRefine((rules, ctx) => {
        const message = validateClientRules(rules)
        if (message) ctx.addIssue({ code: 'custom', message })
      }),
  }),
  system_task_setting: z.object({
    retention_days: z.number().int().min(1),
  }),
})

type LogSettingsFormValues = z.infer<typeof logSettingsSchema>

type LogSettingsSectionProps = {
  defaultValues: {
    LogConsumeEnabled: boolean
    AuditLogEnabled: boolean
    AdminAuditMode: string
    LogExportFileRetentionHours: number
    'log_setting.user_export_enabled': boolean
    'log_setting.max_range_days': number
    'log_setting.client_rules': string
    'system_task_setting.retention_days': number
  }
}

export function LogSettingsSection(props: LogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const recordingTitleId = useId()
  const clientTitleId = useId()
  const searchTitleId = useId()
  const taskHistoryTitleId = useId()
  const formDefaults = useMemo<LogSettingsFormValues>(
    () => ({
      LogConsumeEnabled: props.defaultValues.LogConsumeEnabled,
      AuditLogEnabled: props.defaultValues.AuditLogEnabled,
      AdminAuditMode: props.defaultValues.AdminAuditMode,
      LogExportFileRetentionHours:
        props.defaultValues.LogExportFileRetentionHours,
      log_setting: {
        user_export_enabled:
          props.defaultValues['log_setting.user_export_enabled'],
        max_range_days: props.defaultValues['log_setting.max_range_days'],
        client_rules: parseClientRules(
          props.defaultValues['log_setting.client_rules']
        ),
      },
      system_task_setting: {
        retention_days:
          props.defaultValues['system_task_setting.retention_days'],
      },
    }),
    [props.defaultValues]
  )
  const form = useForm<LogSettingsFormValues>({
    resolver: zodResolver(logSettingsSchema),
    defaultValues: formDefaults,
  })

  const isSaving = form.formState.isSubmitting || updateOption.isPending
  const auditLogEnabled = useWatch({
    control: form.control,
    name: 'AuditLogEnabled',
  })
  useResetForm(form, isSaving ? undefined : formDefaults)

  const onSubmit = async (values: LogSettingsFormValues) => {
    const clientRules = serializeClientRules(values.log_setting.client_rules)
    const defaultClientRules = serializeClientRules(
      parseClientRules(props.defaultValues['log_setting.client_rules'])
    )
    const changes = [
      ['LogConsumeEnabled', values.LogConsumeEnabled],
      ['AuditLogEnabled', values.AuditLogEnabled],
      ['AdminAuditMode', values.AdminAuditMode],
      ['LogExportFileRetentionHours', values.LogExportFileRetentionHours],
      [
        'log_setting.user_export_enabled',
        values.log_setting.user_export_enabled,
      ],
      ['log_setting.max_range_days', values.log_setting.max_range_days],
      ['log_setting.client_rules', clientRules],
      [
        'system_task_setting.retention_days',
        values.system_task_setting.retention_days,
      ],
    ] as const
    for (const [key, value] of changes) {
      const current =
        key === 'log_setting.client_rules'
          ? defaultClientRules
          : props.defaultValues[key]
      if (value === current) continue
      try {
        await updateOption.mutateAsync({ key, value })
      } catch {
        // The mutation reports the error. Keep the form values for a retry.
        return
      }
    }
  }

  return (
    <SettingsSection title={t('Log Maintenance')} className='gap-6'>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={isSaving}
            saveLabel='Save log settings'
          />
          <div
            role='group'
            aria-labelledby={recordingTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={recordingTitleId} className='font-medium'>
              {t('Log recording')}
            </h4>
            <FormField
              control={form.control}
              name='LogConsumeEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Record quota usage')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Track per-request consumption to power usage analytics. Keeping this on increases database writes.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='AuditLogEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Record audit logs')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Record login, security, administrative operations, and access token activity. Turning this off stops new audit entries and keeps existing logs. Usage logs are controlled separately.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={isSaving}
                    />
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='AdminAuditMode'
              render={({ field }) => (
                <SettingsSwitchItem
                  data-disabled={!auditLogEnabled || isSaving || undefined}
                >
                  <SettingsSwitchContent>
                    <FormLabel>{t('Admin operation audit scope')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Choose between explicitly audited operations and all admin/root write operations.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Select
                      items={[
                        {
                          value: 'billing_only',
                          label: t('Key operations only'),
                        },
                        {
                          value: 'full_audit',
                          label: t('All admin write operations'),
                        },
                      ]}
                      value={field.value}
                      disabled={!auditLogEnabled || isSaving}
                      onValueChange={(value) =>
                        value !== null && field.onChange(value)
                      }
                    >
                      <SelectTrigger className='w-[220px]'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='billing_only'>
                            {t('Key operations only')}
                          </SelectItem>
                          <SelectItem value='full_audit'>
                            {t('All admin write operations')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </FormControl>
                  <FormMessage />
                </SettingsSwitchItem>
              )}
            />
          </div>

          <Separator />

          <div
            role='group'
            aria-labelledby={clientTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <div className='space-y-1'>
              <h4 id={clientTitleId} className='font-medium'>
                {t('Request client detection')}
              </h4>
              <p className='text-muted-foreground text-sm'>
                {t(
                  'The client column in usage logs is detected from the request User-Agent. Add keywords here to group a custom or private client; custom rules are checked before the built-in library.'
                )}
              </p>
            </div>
            <FormField
              control={form.control}
              name='log_setting.client_rules'
              render={({ field, fieldState }) => (
                <FormItem>
                  <ClientRulesEditor
                    value={field.value}
                    onChange={field.onChange}
                    disabled={isSaving}
                  />
                  {fieldState.error?.message && (
                    <p className='text-destructive text-xs' role='alert'>
                      {fieldState.error.message}
                    </p>
                  )}
                </FormItem>
              )}
            />
          </div>

          <Separator />

          <div
            role='group'
            aria-labelledby={searchTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={searchTitleId} className='font-medium'>
              {t('Log search and export')}
            </h4>
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='log_setting.user_export_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Allow users to export logs')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Users can download their own logs from the usage log page. Administrators can always export.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                    <FormMessage />
                  </SettingsSwitchItem>
                )}
              />
              <FormField
                control={form.control}
                name='log_setting.max_range_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Log time range limit (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Applies to user log and dashboard queries, direct CSV downloads, and background log exports.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <LogExportFields />
            </SettingsFormGrid>
          </div>

          <Separator />

          <div
            role='group'
            aria-labelledby={taskHistoryTitleId}
            className='flex min-w-0 flex-col gap-4'
          >
            <h4 id={taskHistoryTitleId} className='font-medium'>
              {t('System task records')}
            </h4>
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='system_task_setting.retention_days'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Keep finished tasks for (days)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        placeholder='30'
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Finished system tasks older than this are deleted automatically every day.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </div>
        </SettingsForm>
      </Form>

      <Separator />

      <HistoryLogCleanup />

      <Separator />

      <AuditLogCleanup />

      <Separator />

      <ServerLogFiles />
    </SettingsSection>
  )
}
