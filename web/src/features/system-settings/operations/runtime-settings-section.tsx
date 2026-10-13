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
import { useEffect, useMemo, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Switch } from '@/components/ui/switch'

import {
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

// Empty means "fall back to the backend default", so an untouched blank field
// must stay valid; only reject clearly malformed non-empty endpoints.
const optionalUrlString = z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  return /^https?:\/\//.test(trimmed)
}, 'Provide a valid URL starting with http:// or https://')

const runtimeSchema = z.object({
  runtime_setting: z.object({
    relay_timeout: z.coerce.number().min(0),
    streaming_timeout: z.coerce.number().min(1),
    max_request_body_mb: z.coerce.number().min(1),
    stream_scanner_max_buffer_mb: z.coerce.number().min(1),
    relay_max_idle_conns: z.coerce.number().min(1),
    relay_max_idle_conns_per_host: z.coerce.number().min(1),
    global_api_rate_limit_enabled: z.boolean(),
    global_api_rate_limit: z.coerce.number().min(0),
    global_api_rate_limit_duration: z.coerce.number().min(1),
    global_web_rate_limit_enabled: z.boolean(),
    global_web_rate_limit: z.coerce.number().min(0),
    global_web_rate_limit_duration: z.coerce.number().min(1),
    critical_rate_limit_enabled: z.boolean(),
    critical_rate_limit: z.coerce.number().min(0),
    critical_rate_limit_duration: z.coerce.number().min(1),
    search_rate_limit_enabled: z.boolean(),
    search_rate_limit: z.coerce.number().min(0),
    search_rate_limit_duration: z.coerce.number().min(1),
    azure_default_api_version: z.string(),
    max_file_download_mb: z.coerce.number().min(1),
    force_stream_option_enabled: z.boolean(),
    count_token_enabled: z.boolean(),
    get_media_token_enabled: z.boolean(),
    get_media_token_not_stream_enabled: z.boolean(),
    sync_upstream_base: optionalUrlString,
    sync_http_timeout_seconds: z.coerce.number().min(1),
    sync_http_retry: z.coerce.number().min(1),
    sync_http_max_mb: z.coerce.number().min(1),
    notify_limit_count: z.coerce.number().min(0),
    notification_limit_duration: z.coerce.number().min(1),
    channel_test_request_interval: z.coerce.number().min(0),
    linux_do_token_endpoint: optionalUrlString,
    linux_do_user_endpoint: optionalUrlString,
    debug_enabled: z.boolean(),
    access_log_enabled: z.boolean(),
    error_log_enabled: z.boolean(),
  }),
  perf_metrics_setting: z.object({
    enabled: z.boolean(),
  }),
})

type RuntimeFormInput = z.input<typeof runtimeSchema>
type RuntimeFormValues = z.output<typeof runtimeSchema>

type FlatRuntimeDefaults = {
  'runtime_setting.relay_timeout': number
  'runtime_setting.streaming_timeout': number
  'runtime_setting.max_request_body_mb': number
  'runtime_setting.stream_scanner_max_buffer_mb': number
  'runtime_setting.relay_max_idle_conns': number
  'runtime_setting.relay_max_idle_conns_per_host': number
  'runtime_setting.global_api_rate_limit_enabled': boolean
  'runtime_setting.global_api_rate_limit': number
  'runtime_setting.global_api_rate_limit_duration': number
  'runtime_setting.global_web_rate_limit_enabled': boolean
  'runtime_setting.global_web_rate_limit': number
  'runtime_setting.global_web_rate_limit_duration': number
  'runtime_setting.critical_rate_limit_enabled': boolean
  'runtime_setting.critical_rate_limit': number
  'runtime_setting.critical_rate_limit_duration': number
  'runtime_setting.search_rate_limit_enabled': boolean
  'runtime_setting.search_rate_limit': number
  'runtime_setting.search_rate_limit_duration': number
  'runtime_setting.azure_default_api_version': string
  'runtime_setting.max_file_download_mb': number
  'runtime_setting.force_stream_option_enabled': boolean
  'runtime_setting.count_token_enabled': boolean
  'runtime_setting.get_media_token_enabled': boolean
  'runtime_setting.get_media_token_not_stream_enabled': boolean
  'runtime_setting.sync_upstream_base': string
  'runtime_setting.sync_http_timeout_seconds': number
  'runtime_setting.sync_http_retry': number
  'runtime_setting.sync_http_max_mb': number
  'runtime_setting.notify_limit_count': number
  'runtime_setting.notification_limit_duration': number
  'runtime_setting.channel_test_request_interval': number
  'runtime_setting.linux_do_token_endpoint': string
  'runtime_setting.linux_do_user_endpoint': string
  'runtime_setting.debug_enabled': boolean
  'runtime_setting.access_log_enabled': boolean
  'runtime_setting.error_log_enabled': boolean
  'perf_metrics_setting.enabled': boolean
}

type RuntimeSettingsSectionProps = {
  defaultValues: FlatRuntimeDefaults
}

// Backend accepts out-of-band writes below the schema minimums (it clamps or
// applies them raw at read time), so seed values must be clamped up to the
// schema floor — otherwise one bad stored value fails zod and blocks saving
// every field in this section.
const clampMin = (value: number, min: number): number =>
  Number.isFinite(value) ? Math.max(min, value) : min

const buildFormDefaults = (
  defaults: FlatRuntimeDefaults
): RuntimeFormInput => ({
  runtime_setting: {
    relay_timeout: clampMin(defaults['runtime_setting.relay_timeout'], 0),
    streaming_timeout: clampMin(
      defaults['runtime_setting.streaming_timeout'],
      1
    ),
    max_request_body_mb: clampMin(
      defaults['runtime_setting.max_request_body_mb'],
      1
    ),
    stream_scanner_max_buffer_mb: clampMin(
      defaults['runtime_setting.stream_scanner_max_buffer_mb'],
      1
    ),
    relay_max_idle_conns: clampMin(
      defaults['runtime_setting.relay_max_idle_conns'],
      1
    ),
    relay_max_idle_conns_per_host: clampMin(
      defaults['runtime_setting.relay_max_idle_conns_per_host'],
      1
    ),
    global_api_rate_limit_enabled:
      defaults['runtime_setting.global_api_rate_limit_enabled'],
    global_api_rate_limit: clampMin(
      defaults['runtime_setting.global_api_rate_limit'],
      0
    ),
    global_api_rate_limit_duration: clampMin(
      defaults['runtime_setting.global_api_rate_limit_duration'],
      1
    ),
    global_web_rate_limit_enabled:
      defaults['runtime_setting.global_web_rate_limit_enabled'],
    global_web_rate_limit: clampMin(
      defaults['runtime_setting.global_web_rate_limit'],
      0
    ),
    global_web_rate_limit_duration: clampMin(
      defaults['runtime_setting.global_web_rate_limit_duration'],
      1
    ),
    critical_rate_limit_enabled:
      defaults['runtime_setting.critical_rate_limit_enabled'],
    critical_rate_limit: clampMin(
      defaults['runtime_setting.critical_rate_limit'],
      0
    ),
    critical_rate_limit_duration: clampMin(
      defaults['runtime_setting.critical_rate_limit_duration'],
      1
    ),
    search_rate_limit_enabled:
      defaults['runtime_setting.search_rate_limit_enabled'],
    search_rate_limit: clampMin(
      defaults['runtime_setting.search_rate_limit'],
      0
    ),
    search_rate_limit_duration: clampMin(
      defaults['runtime_setting.search_rate_limit_duration'],
      1
    ),
    azure_default_api_version:
      defaults['runtime_setting.azure_default_api_version'],
    max_file_download_mb: clampMin(
      defaults['runtime_setting.max_file_download_mb'],
      1
    ),
    force_stream_option_enabled:
      defaults['runtime_setting.force_stream_option_enabled'],
    count_token_enabled: defaults['runtime_setting.count_token_enabled'],
    get_media_token_enabled:
      defaults['runtime_setting.get_media_token_enabled'],
    get_media_token_not_stream_enabled:
      defaults['runtime_setting.get_media_token_not_stream_enabled'],
    sync_upstream_base: defaults['runtime_setting.sync_upstream_base'],
    sync_http_timeout_seconds: clampMin(
      defaults['runtime_setting.sync_http_timeout_seconds'],
      1
    ),
    sync_http_retry: clampMin(defaults['runtime_setting.sync_http_retry'], 1),
    sync_http_max_mb: clampMin(defaults['runtime_setting.sync_http_max_mb'], 1),
    notify_limit_count: clampMin(
      defaults['runtime_setting.notify_limit_count'],
      0
    ),
    notification_limit_duration: clampMin(
      defaults['runtime_setting.notification_limit_duration'],
      1
    ),
    channel_test_request_interval: clampMin(
      defaults['runtime_setting.channel_test_request_interval'],
      0
    ),
    linux_do_token_endpoint:
      defaults['runtime_setting.linux_do_token_endpoint'],
    linux_do_user_endpoint: defaults['runtime_setting.linux_do_user_endpoint'],
    debug_enabled: defaults['runtime_setting.debug_enabled'],
    access_log_enabled: defaults['runtime_setting.access_log_enabled'],
    error_log_enabled: defaults['runtime_setting.error_log_enabled'],
  },
  perf_metrics_setting: {
    enabled: defaults['perf_metrics_setting.enabled'],
  },
})

const normalizeDefaults = (
  defaults: FlatRuntimeDefaults
): FlatRuntimeDefaults => ({
  ...defaults,
  'runtime_setting.azure_default_api_version':
    defaults['runtime_setting.azure_default_api_version'].trim(),
  'runtime_setting.sync_upstream_base':
    defaults['runtime_setting.sync_upstream_base'].trim(),
  'runtime_setting.linux_do_token_endpoint':
    defaults['runtime_setting.linux_do_token_endpoint'].trim(),
  'runtime_setting.linux_do_user_endpoint':
    defaults['runtime_setting.linux_do_user_endpoint'].trim(),
})

const normalizeFormValues = (
  values: RuntimeFormValues
): FlatRuntimeDefaults => ({
  'runtime_setting.relay_timeout': values.runtime_setting.relay_timeout,
  'runtime_setting.streaming_timeout': values.runtime_setting.streaming_timeout,
  'runtime_setting.max_request_body_mb':
    values.runtime_setting.max_request_body_mb,
  'runtime_setting.stream_scanner_max_buffer_mb':
    values.runtime_setting.stream_scanner_max_buffer_mb,
  'runtime_setting.relay_max_idle_conns':
    values.runtime_setting.relay_max_idle_conns,
  'runtime_setting.relay_max_idle_conns_per_host':
    values.runtime_setting.relay_max_idle_conns_per_host,
  'runtime_setting.global_api_rate_limit_enabled':
    values.runtime_setting.global_api_rate_limit_enabled,
  'runtime_setting.global_api_rate_limit':
    values.runtime_setting.global_api_rate_limit,
  'runtime_setting.global_api_rate_limit_duration':
    values.runtime_setting.global_api_rate_limit_duration,
  'runtime_setting.global_web_rate_limit_enabled':
    values.runtime_setting.global_web_rate_limit_enabled,
  'runtime_setting.global_web_rate_limit':
    values.runtime_setting.global_web_rate_limit,
  'runtime_setting.global_web_rate_limit_duration':
    values.runtime_setting.global_web_rate_limit_duration,
  'runtime_setting.critical_rate_limit_enabled':
    values.runtime_setting.critical_rate_limit_enabled,
  'runtime_setting.critical_rate_limit':
    values.runtime_setting.critical_rate_limit,
  'runtime_setting.critical_rate_limit_duration':
    values.runtime_setting.critical_rate_limit_duration,
  'runtime_setting.search_rate_limit_enabled':
    values.runtime_setting.search_rate_limit_enabled,
  'runtime_setting.search_rate_limit': values.runtime_setting.search_rate_limit,
  'runtime_setting.search_rate_limit_duration':
    values.runtime_setting.search_rate_limit_duration,
  'runtime_setting.azure_default_api_version':
    values.runtime_setting.azure_default_api_version.trim(),
  'runtime_setting.max_file_download_mb':
    values.runtime_setting.max_file_download_mb,
  'runtime_setting.force_stream_option_enabled':
    values.runtime_setting.force_stream_option_enabled,
  'runtime_setting.count_token_enabled':
    values.runtime_setting.count_token_enabled,
  'runtime_setting.get_media_token_enabled':
    values.runtime_setting.get_media_token_enabled,
  'runtime_setting.get_media_token_not_stream_enabled':
    values.runtime_setting.get_media_token_not_stream_enabled,
  'runtime_setting.sync_upstream_base':
    values.runtime_setting.sync_upstream_base.trim(),
  'runtime_setting.sync_http_timeout_seconds':
    values.runtime_setting.sync_http_timeout_seconds,
  'runtime_setting.sync_http_retry': values.runtime_setting.sync_http_retry,
  'runtime_setting.sync_http_max_mb': values.runtime_setting.sync_http_max_mb,
  'runtime_setting.notify_limit_count':
    values.runtime_setting.notify_limit_count,
  'runtime_setting.notification_limit_duration':
    values.runtime_setting.notification_limit_duration,
  'runtime_setting.channel_test_request_interval':
    values.runtime_setting.channel_test_request_interval,
  'runtime_setting.linux_do_token_endpoint':
    values.runtime_setting.linux_do_token_endpoint.trim(),
  'runtime_setting.linux_do_user_endpoint':
    values.runtime_setting.linux_do_user_endpoint.trim(),
  'runtime_setting.debug_enabled': values.runtime_setting.debug_enabled,
  'runtime_setting.access_log_enabled':
    values.runtime_setting.access_log_enabled,
  'runtime_setting.error_log_enabled': values.runtime_setting.error_log_enabled,
  'perf_metrics_setting.enabled': values.perf_metrics_setting.enabled,
})

export function RuntimeSettingsSection({
  defaultValues,
}: RuntimeSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<FlatRuntimeDefaults>(
    normalizeDefaults(defaultValues)
  )
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(normalizeDefaults(defaultValues))
  )

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<RuntimeFormInput, unknown, RuntimeFormValues>({
    resolver: zodResolver(runtimeSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  useEffect(() => {
    const normalized = normalizeDefaults(defaultValues)
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = normalized
    baselineSerializedRef.current = serialized
  }, [defaultValues])

  const globalApiEnabled = form.watch(
    'runtime_setting.global_api_rate_limit_enabled'
  )
  const globalWebEnabled = form.watch(
    'runtime_setting.global_web_rate_limit_enabled'
  )
  const criticalEnabled = form.watch(
    'runtime_setting.critical_rate_limit_enabled'
  )
  const searchEnabled = form.watch('runtime_setting.search_rate_limit_enabled')

  const onSubmit = async (values: RuntimeFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof FlatRuntimeDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of updates) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
  }

  return (
    <SettingsSection title={t('Runtime Configuration')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <div>
            <h4 className='font-medium'>{t('Relay Runtime')}</h4>
          </div>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='runtime_setting.relay_timeout'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Relay request timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>{t('0 means no limit')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.streaming_timeout'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Streaming idle timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.max_request_body_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max request body (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.stream_scanner_max_buffer_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Stream buffer (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Max buffer for a single streaming event')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.relay_max_idle_conns'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max idle connections')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.relay_max_idle_conns_per_host'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max idle connections per host')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div>
            <h4 className='font-medium'>{t('Global Rate Limiting')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Set requests to 0 to bypass. Changes apply to new requests immediately.'
              )}
            </p>
          </div>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='runtime_setting.global_api_rate_limit_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable global API rate limit')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='runtime_setting.global_api_rate_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Requests')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!globalApiEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='runtime_setting.global_api_rate_limit_duration'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Window (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!globalApiEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SettingsControlGroup>

            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='runtime_setting.global_web_rate_limit_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable global web rate limit')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='runtime_setting.global_web_rate_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Requests')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!globalWebEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='runtime_setting.global_web_rate_limit_duration'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Window (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!globalWebEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SettingsControlGroup>

            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='runtime_setting.critical_rate_limit_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>
                        {t('Enable critical operation rate limit')}
                      </FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='runtime_setting.critical_rate_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Requests')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!criticalEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='runtime_setting.critical_rate_limit_duration'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Window (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!criticalEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SettingsControlGroup>

            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='runtime_setting.search_rate_limit_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable search rate limit')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='runtime_setting.search_rate_limit'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Requests')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={0}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!searchEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='runtime_setting.search_rate_limit_duration'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Window (seconds)')}</FormLabel>
                      <FormControl>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          {...safeNumberFieldProps(field)}
                          disabled={!searchEnabled}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SettingsControlGroup>
          </div>

          <div>
            <h4 className='font-medium'>{t('Model & Billing Runtime')}</h4>
          </div>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='runtime_setting.azure_default_api_version'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Azure default API version')}</FormLabel>
                  <FormControl>
                    <Input
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.max_file_download_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Max file download (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.force_stream_option_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Force stream usage')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.count_token_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable token counting')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.get_media_token_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Count media tokens')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.get_media_token_not_stream_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Count media tokens (non-stream)')}
                    </FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div>
            <h4 className='font-medium'>
              {t('Sync, Notification & Diagnostics')}
            </h4>
          </div>
          <div className='grid grid-cols-1 gap-4'>
            <FormField
              control={form.control}
              name='runtime_setting.sync_upstream_base'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Model sync upstream')}</FormLabel>
                  <FormControl>
                    <Input
                      type='url'
                      inputMode='url'
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.linux_do_token_endpoint'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Linux DO token endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      type='url'
                      inputMode='url'
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.linux_do_user_endpoint'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Linux DO user endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      type='url'
                      inputMode='url'
                      value={field.value}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='runtime_setting.sync_http_timeout_seconds'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Sync timeout (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.sync_http_retry'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Sync retry count')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.sync_http_max_mb'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Sync response limit (MB)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.notify_limit_count'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Notification limit count')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.notification_limit_duration'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Notification limit window (minutes)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.channel_test_request_interval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel test interval (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Wait between two channels in a batch test')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='runtime_setting.debug_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Debug mode')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.error_log_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Error log recording')}</FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='runtime_setting.access_log_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Access log')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Logs every HTTP request; adds CPU and IO overhead under high concurrency'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='perf_metrics_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Enable model performance metrics')}
                    </FormLabel>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
