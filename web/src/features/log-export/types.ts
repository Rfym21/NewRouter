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
export type LogExportStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'expired'

export interface LogExportShare {
  id: number
  job_id: number
  token: string
  created_by_user_id: number
  created_by_username: string
  max_downloads: number
  download_count: number
  require_login: boolean
  created_at: number
  updated_at: number
  last_downloaded_at: number
  /** Server-relative download path, e.g. /api/log/export-share/{token}/download */
  download_path: string
  /** Share link expiry follows the export file expiry. */
  expires_at: number
}

export interface LogExportJob {
  id: number
  user_id: number
  username: string
  is_admin: boolean
  status: LogExportStatus | string
  file_name: string
  file_size: number
  start_timestamp: number
  end_timestamp: number
  columns: string
  filters: string
  total: number
  exported: number
  error: string
  created_at: number
  updated_at: number
  completed_at: number
  expires_at: number
  share?: LogExportShare | null
}

export interface LogExportListData {
  items: LogExportJob[]
  page: number
  page_size: number
  total: number
}

export interface LogExportListResponse {
  success: boolean
  message?: string
  data?: LogExportListData
}

export interface CreateLogExportPayload {
  type?: number
  username?: string
  token_name?: string
  model_name?: string
  start_timestamp: number
  end_timestamp: number
  channel?: number
  group?: string
  request_id?: string
  columns: string[]
}

export interface LogExportShareRequest {
  max_downloads: number
  require_login: boolean
}
