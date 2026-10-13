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
import { useStatus } from '@/hooks/use-status'

import { SettingsPage } from '../components/settings-page'
import type { OperationsSettings } from '../types'
import {
  OPERATIONS_DEFAULT_SECTION,
  getOperationsSectionContent,
  getOperationsSectionMeta,
} from './section-registry.tsx'

const defaultOperationsSettings: OperationsSettings = {
  DefaultCollapseSidebar: false,
  DemoSiteEnabled: false,
  SelfUseModeEnabled: false,
  QuotaRemindThreshold: '',
  SMTPServer: '',
  SMTPPort: '',
  SMTPAccount: '',
  SMTPFrom: '',
  SMTPToken: '',
  SMTPSSLEnabled: false,
  SMTPStartTLSEnabled: false,
  SMTPInsecureSkipVerify: false,
  SMTPForceAuthLogin: false,
  WorkerUrl: '',
  WorkerValidKey: '',
  WorkerAllowHttpImageRequestEnabled: false,
  LogConsumeEnabled: false,
  AdminAuditMode: 'billing_only',
  LogExportMaxDays: 30,
  LogExportFileRetentionHours: 24,
  'log_setting.user_export_enabled': true,
  'log_setting.max_range_days': 365,
  'performance_setting.disk_cache_enabled': false,
  'performance_setting.disk_cache_threshold_mb': 10,
  'performance_setting.disk_cache_max_size_mb': 1024,
  'performance_setting.disk_cache_path': '',
  'performance_setting.monitor_enabled': false,
  'performance_setting.monitor_cpu_threshold': 90,
  'performance_setting.monitor_memory_threshold': 90,
  'performance_setting.monitor_disk_threshold': 95,
  'perf_metrics_setting.enabled': true,
  'perf_metrics_setting.flush_interval': 5,
  'perf_metrics_setting.bucket_time': 'hour',
  'perf_metrics_setting.retention_days': 0,
  'runtime_setting.relay_timeout': 0,
  'runtime_setting.streaming_timeout': 300,
  'runtime_setting.max_request_body_mb': 128,
  'runtime_setting.stream_scanner_max_buffer_mb': 128,
  'runtime_setting.relay_max_idle_conns': 500,
  'runtime_setting.relay_max_idle_conns_per_host': 100,
  'runtime_setting.global_api_rate_limit_enabled': true,
  'runtime_setting.global_api_rate_limit': 360,
  'runtime_setting.global_api_rate_limit_duration': 180,
  'runtime_setting.global_web_rate_limit_enabled': true,
  'runtime_setting.global_web_rate_limit': 120,
  'runtime_setting.global_web_rate_limit_duration': 180,
  'runtime_setting.critical_rate_limit_enabled': true,
  'runtime_setting.critical_rate_limit': 20,
  'runtime_setting.critical_rate_limit_duration': 1200,
  'runtime_setting.search_rate_limit_enabled': true,
  'runtime_setting.search_rate_limit': 10,
  'runtime_setting.search_rate_limit_duration': 60,
  'runtime_setting.azure_default_api_version': '2025-04-01-preview',
  'runtime_setting.max_file_download_mb': 64,
  'runtime_setting.force_stream_option_enabled': true,
  'runtime_setting.count_token_enabled': true,
  'runtime_setting.get_media_token_enabled': true,
  'runtime_setting.get_media_token_not_stream_enabled': false,
  'runtime_setting.sync_upstream_base':
    'https://basellm.github.io/llm-metadata',
  'runtime_setting.sync_http_timeout_seconds': 15,
  'runtime_setting.sync_http_retry': 3,
  'runtime_setting.sync_http_max_mb': 10,
  'runtime_setting.notify_limit_count': 2,
  'runtime_setting.notification_limit_duration': 10,
  'runtime_setting.channel_test_request_interval': 0,
  'runtime_setting.linux_do_token_endpoint':
    'https://connect.linux.do/oauth2/token',
  'runtime_setting.linux_do_user_endpoint': 'https://connect.linux.do/api/user',
  'runtime_setting.debug_enabled': false,
  'runtime_setting.access_log_enabled': true,
  'runtime_setting.error_log_enabled': false,
}

export function OperationsSettings() {
  const { status } = useStatus()

  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/operations/$section'
      defaultSettings={defaultOperationsSettings}
      defaultSection={OPERATIONS_DEFAULT_SECTION}
      getSectionContent={getOperationsSectionContent}
      getSectionMeta={getOperationsSectionMeta}
      extraArgs={[
        status?.version as string | undefined,
        status?.start_time as number | null | undefined,
      ]}
      loadingMessage='Loading maintenance settings...'
    />
  )
}
