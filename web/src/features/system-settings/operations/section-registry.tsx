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
import { SystemBehaviorSection } from '../general/system-behavior-section'
import { EmailSettingsSection } from '../integrations/email-settings-section'
import { MonitoringSettingsSection } from '../integrations/monitoring-settings-section'
import { WorkerSettingsSection } from '../integrations/worker-settings-section'
import { LogExportSection } from '../maintenance/log-export-section'
import { LogSettingsSection } from '../maintenance/log-settings-section'
import { PerformanceSection } from '../maintenance/performance-section'
import { UpdateCheckerSection } from '../maintenance/update-checker-section'
import type { OperationsSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { RuntimeSettingsSection } from './runtime-settings-section'

const OPERATIONS_SECTIONS = [
  {
    id: 'behavior',
    titleKey: 'System Behavior',
    build: (settings: OperationsSettings) => (
      <SystemBehaviorSection
        defaultValues={{
          DefaultCollapseSidebar: settings.DefaultCollapseSidebar,
          DemoSiteEnabled: settings.DemoSiteEnabled,
          SelfUseModeEnabled: settings.SelfUseModeEnabled,
        }}
      />
    ),
  },
  {
    id: 'runtime',
    titleKey: 'Runtime Configuration',
    build: (settings: OperationsSettings) => (
      <RuntimeSettingsSection
        defaultValues={{
          'runtime_setting.relay_timeout':
            settings['runtime_setting.relay_timeout'] ?? 0,
          'runtime_setting.streaming_timeout':
            settings['runtime_setting.streaming_timeout'] ?? 300,
          'runtime_setting.max_request_body_mb':
            settings['runtime_setting.max_request_body_mb'] ?? 128,
          'runtime_setting.stream_scanner_max_buffer_mb':
            settings['runtime_setting.stream_scanner_max_buffer_mb'] ?? 128,
          'runtime_setting.relay_max_idle_conns':
            settings['runtime_setting.relay_max_idle_conns'] ?? 500,
          'runtime_setting.relay_max_idle_conns_per_host':
            settings['runtime_setting.relay_max_idle_conns_per_host'] ?? 100,
          'runtime_setting.global_api_rate_limit_enabled':
            settings['runtime_setting.global_api_rate_limit_enabled'] ?? true,
          'runtime_setting.global_api_rate_limit':
            settings['runtime_setting.global_api_rate_limit'] ?? 360,
          'runtime_setting.global_api_rate_limit_duration':
            settings['runtime_setting.global_api_rate_limit_duration'] ?? 180,
          'runtime_setting.global_web_rate_limit_enabled':
            settings['runtime_setting.global_web_rate_limit_enabled'] ?? true,
          'runtime_setting.global_web_rate_limit':
            settings['runtime_setting.global_web_rate_limit'] ?? 120,
          'runtime_setting.global_web_rate_limit_duration':
            settings['runtime_setting.global_web_rate_limit_duration'] ?? 180,
          'runtime_setting.critical_rate_limit_enabled':
            settings['runtime_setting.critical_rate_limit_enabled'] ?? true,
          'runtime_setting.critical_rate_limit':
            settings['runtime_setting.critical_rate_limit'] ?? 20,
          'runtime_setting.critical_rate_limit_duration':
            settings['runtime_setting.critical_rate_limit_duration'] ?? 1200,
          'runtime_setting.search_rate_limit_enabled':
            settings['runtime_setting.search_rate_limit_enabled'] ?? true,
          'runtime_setting.search_rate_limit':
            settings['runtime_setting.search_rate_limit'] ?? 10,
          'runtime_setting.search_rate_limit_duration':
            settings['runtime_setting.search_rate_limit_duration'] ?? 60,
          'runtime_setting.azure_default_api_version':
            settings['runtime_setting.azure_default_api_version'] ??
            '2025-04-01-preview',
          'runtime_setting.max_file_download_mb':
            settings['runtime_setting.max_file_download_mb'] ?? 64,
          'runtime_setting.force_stream_option_enabled':
            settings['runtime_setting.force_stream_option_enabled'] ?? true,
          'runtime_setting.count_token_enabled':
            settings['runtime_setting.count_token_enabled'] ?? true,
          'runtime_setting.get_media_token_enabled':
            settings['runtime_setting.get_media_token_enabled'] ?? true,
          'runtime_setting.get_media_token_not_stream_enabled':
            settings['runtime_setting.get_media_token_not_stream_enabled'] ??
            false,
          'runtime_setting.sync_upstream_base':
            settings['runtime_setting.sync_upstream_base'] ??
            'https://basellm.github.io/llm-metadata',
          'runtime_setting.sync_http_timeout_seconds':
            settings['runtime_setting.sync_http_timeout_seconds'] ?? 15,
          'runtime_setting.sync_http_retry':
            settings['runtime_setting.sync_http_retry'] ?? 3,
          'runtime_setting.sync_http_max_mb':
            settings['runtime_setting.sync_http_max_mb'] ?? 10,
          'runtime_setting.notify_limit_count':
            settings['runtime_setting.notify_limit_count'] ?? 2,
          'runtime_setting.notification_limit_duration':
            settings['runtime_setting.notification_limit_duration'] ?? 10,
          'runtime_setting.channel_test_request_interval':
            settings['runtime_setting.channel_test_request_interval'] ?? 0,
          'runtime_setting.linux_do_token_endpoint':
            settings['runtime_setting.linux_do_token_endpoint'] ??
            'https://connect.linux.do/oauth2/token',
          'runtime_setting.linux_do_user_endpoint':
            settings['runtime_setting.linux_do_user_endpoint'] ??
            'https://connect.linux.do/api/user',
          'runtime_setting.debug_enabled':
            settings['runtime_setting.debug_enabled'] ?? false,
          'runtime_setting.access_log_enabled':
            settings['runtime_setting.access_log_enabled'] ?? true,
          'runtime_setting.error_log_enabled':
            settings['runtime_setting.error_log_enabled'] ?? false,
          'perf_metrics_setting.enabled':
            settings['perf_metrics_setting.enabled'] ?? true,
        }}
      />
    ),
  },
  {
    id: 'alerts',
    titleKey: 'Monitoring & Alerts',
    build: (settings: OperationsSettings) => (
      <MonitoringSettingsSection
        defaultValues={{
          QuotaRemindThreshold: settings.QuotaRemindThreshold,
          'perf_metrics_setting.enabled':
            settings['perf_metrics_setting.enabled'] ?? true,
          'perf_metrics_setting.flush_interval':
            settings['perf_metrics_setting.flush_interval'] ?? 5,
          'perf_metrics_setting.bucket_time':
            settings['perf_metrics_setting.bucket_time'] ?? 'hour',
          'perf_metrics_setting.retention_days':
            settings['perf_metrics_setting.retention_days'] ?? 0,
        }}
      />
    ),
  },
  {
    id: 'email',
    titleKey: 'SMTP Email',
    build: (settings: OperationsSettings) => (
      <EmailSettingsSection
        defaultValues={{
          SMTPServer: settings.SMTPServer,
          SMTPPort: settings.SMTPPort,
          SMTPAccount: settings.SMTPAccount,
          SMTPFrom: settings.SMTPFrom,
          SMTPToken: settings.SMTPToken,
          SMTPSSLEnabled: settings.SMTPSSLEnabled,
          SMTPStartTLSEnabled: settings.SMTPStartTLSEnabled,
          SMTPInsecureSkipVerify: settings.SMTPInsecureSkipVerify,
          SMTPForceAuthLogin: settings.SMTPForceAuthLogin,
        }}
      />
    ),
  },
  {
    id: 'worker',
    titleKey: 'Worker Proxy',
    build: (settings: OperationsSettings) => (
      <WorkerSettingsSection
        defaultValues={{
          WorkerUrl: settings.WorkerUrl,
          WorkerValidKey: settings.WorkerValidKey,
          WorkerAllowHttpImageRequestEnabled:
            settings.WorkerAllowHttpImageRequestEnabled,
        }}
      />
    ),
  },
  {
    id: 'logs',
    titleKey: 'Log Maintenance',
    build: (settings: OperationsSettings) => (
      <LogSettingsSection
        defaultValues={{
          LogConsumeEnabled: Boolean(settings.LogConsumeEnabled),
          AdminAuditMode: String(settings.AdminAuditMode || 'billing_only'),
          'log_setting.user_export_enabled':
            settings['log_setting.user_export_enabled'] ?? true,
          'log_setting.max_range_days':
            settings['log_setting.max_range_days'] ?? 365,
        }}
      />
    ),
  },
  {
    id: 'log-export',
    titleKey: 'Log Export',
    build: (settings: OperationsSettings) => (
      <LogExportSection
        defaultValues={{
          LogExportMaxDays: settings.LogExportMaxDays,
          LogExportFileRetentionHours: settings.LogExportFileRetentionHours,
        }}
      />
    ),
  },
  {
    id: 'performance',
    titleKey: 'Performance',
    build: (settings: OperationsSettings) => (
      <PerformanceSection
        defaultValues={{
          'performance_setting.disk_cache_enabled':
            settings['performance_setting.disk_cache_enabled'] ?? false,
          'performance_setting.disk_cache_threshold_mb':
            settings['performance_setting.disk_cache_threshold_mb'] ?? 10,
          'performance_setting.disk_cache_max_size_mb':
            settings['performance_setting.disk_cache_max_size_mb'] ?? 1024,
          'performance_setting.disk_cache_path':
            settings['performance_setting.disk_cache_path'] ?? '',
          'performance_setting.monitor_enabled':
            settings['performance_setting.monitor_enabled'] ?? false,
          'performance_setting.monitor_cpu_threshold':
            settings['performance_setting.monitor_cpu_threshold'] ?? 90,
          'performance_setting.monitor_memory_threshold':
            settings['performance_setting.monitor_memory_threshold'] ?? 90,
          'performance_setting.monitor_disk_threshold':
            settings['performance_setting.monitor_disk_threshold'] ?? 95,
        }}
      />
    ),
  },
  {
    id: 'update-checker',
    titleKey: 'System maintenance',
    build: (
      _settings: OperationsSettings,
      currentVersion?: string | null,
      startTime?: number | null
    ) => (
      <UpdateCheckerSection
        currentVersion={currentVersion}
        startTime={startTime}
      />
    ),
  },
] as const

export type OperationsSectionId = (typeof OPERATIONS_SECTIONS)[number]['id']

const operationsRegistry = createSectionRegistry<
  OperationsSectionId,
  OperationsSettings,
  [string | null | undefined, number | null | undefined]
>({
  sections: OPERATIONS_SECTIONS,
  defaultSection: 'behavior',
  basePath: '/system-settings/operations',
  urlStyle: 'path',
})

export const OPERATIONS_SECTION_IDS = operationsRegistry.sectionIds
export const OPERATIONS_DEFAULT_SECTION = operationsRegistry.defaultSection
export const getOperationsSectionNavItems =
  operationsRegistry.getSectionNavItems
export const getOperationsSectionContent = operationsRegistry.getSectionContent
export const getOperationsSectionMeta = operationsRegistry.getSectionMeta
