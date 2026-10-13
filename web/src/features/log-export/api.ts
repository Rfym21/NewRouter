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
import { api } from '@/lib/api'

import type {
  CreateLogExportPayload,
  LogExportListResponse,
  LogExportShare,
  LogExportShareRequest,
} from './types'

interface ApiResult<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

function endpointBase(management: boolean): string {
  return management ? '/api/log/export-management' : '/api/log/export'
}

/**
 * Extract a filename from a Content-Disposition header, preferring the RFC 5987
 * UTF-8 form; falls back to the ASCII form and finally the provided default.
 */
function parseFilename(
  disposition: string | undefined,
  fallback: string
): string {
  if (!disposition) return fallback
  const utf8 = disposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (utf8?.[1]) {
    try {
      return decodeURIComponent(utf8[1])
    } catch {
      return fallback
    }
  }
  const ascii = disposition.match(/filename="?([^"]+)"?/i)
  return ascii?.[1] || fallback
}

/** Create a server-side compressed usage-log export job. */
export async function createLogExport(
  payload: CreateLogExportPayload
): Promise<ApiResult> {
  const res = await api.post('/api/log/export', payload)
  return res.data
}

/** List export jobs (own records, or all records in management scope). */
export async function listLogExports(
  params: { p: number; page_size: number },
  management: boolean
): Promise<LogExportListResponse> {
  const res = await api.get(endpointBase(management), {
    params,
    disableDuplicate: true,
  })
  return res.data
}

/**
 * Download a completed export file as a Blob. The endpoint streams a file on
 * success but a JSON envelope on failure (expired/cleaned up), so decode and
 * rethrow the business message when the response is JSON.
 */
export async function downloadLogExportFile(
  id: number,
  management: boolean
): Promise<{ blob: Blob; filename: string }> {
  const res = await api.get(`${endpointBase(management)}/${id}/download`, {
    responseType: 'blob',
    disableDuplicate: true,
    skipBusinessError: true,
    skipErrorHandler: true,
  })
  const blob = res.data as Blob
  const contentType = String(res.headers?.['content-type'] || '')
  if (contentType.includes('application/json')) {
    const text = await blob.text()
    let message = ''
    try {
      message = (JSON.parse(text) as { message?: string })?.message || ''
    } catch {
      /* keep empty */
    }
    throw new Error(message || 'download failed')
  }
  const filename = parseFilename(
    res.headers?.['content-disposition'] as string | undefined,
    `usage-logs-${id}.csv.gz`
  )
  return { blob, filename }
}

/** Delete an export job and its generated file. */
export async function deleteLogExport(
  id: number,
  management: boolean
): Promise<ApiResult> {
  const res = await api.delete(`${endpointBase(management)}/${id}`, {
    skipErrorHandler: true,
  })
  return res.data
}

/** Create or update a shareable download link for a completed export job. */
export async function upsertLogExportShare(
  id: number,
  body: LogExportShareRequest,
  management: boolean
): Promise<ApiResult<LogExportShare>> {
  const res = await api.post(`${endpointBase(management)}/${id}/share`, body, {
    skipErrorHandler: true,
  })
  return res.data
}

/** Revoke a shareable download link. */
export async function deleteLogExportShare(
  id: number,
  management: boolean
): Promise<ApiResult> {
  const res = await api.delete(`${endpointBase(management)}/${id}/share`, {
    skipErrorHandler: true,
  })
  return res.data
}

/** Batch delete export jobs (management scope only). */
export async function batchDeleteManagedLogExports(
  ids: number[]
): Promise<ApiResult<{ count: number }>> {
  const res = await api.post('/api/log/export-management/batch-delete', { ids })
  return res.data
}

/** Extend export file expiration for selected jobs (management scope only). */
export async function extendManagedLogExportsExpiration(
  ids: number[],
  extendHours: number
): Promise<ApiResult<{ count: number }>> {
  const res = await api.post('/api/log/export-management/extend-expiration', {
    ids,
    extend_hours: extendHours,
  })
  return res.data
}
