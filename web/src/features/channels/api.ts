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
import { getGroups as getUserGroups } from '@/features/users/api'
import { api, type ApiRequestConfig } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

import type { InferenceStatus } from './lib/inference-status'
import type {
  AddChannelRequest,
  BatchDeleteParams,
  BatchSetTagParams,
  Channel,
  ChannelBalanceResponse,
  ChannelOpsResponse,
  ChannelTestResponse,
  CopyChannelParams,
  CopyChannelResponse,
  ExportChannelsResponse,
  FetchModelsResponse,
  GetChannelResponse,
  GetChannelsParams,
  GetChannelsResponse,
  MultiKeyBatchResponse,
  MultiKeyManageParams,
  MultiKeyStatusResponse,
  SearchChannelsParams,
  SearchChannelsResponse,
  TagOperationParams,
} from './types'

const channelActionConfig = (
  config: ApiRequestConfig = {}
): ApiRequestConfig => ({
  ...config,
  skipBusinessError: true,
  skipErrorHandler: true,
})

export async function getInferenceStatus(
  channelId: number,
  provider: 'vllm' | 'sglang',
  signal?: AbortSignal
): Promise<InferenceStatus> {
  const response = await api.get<{ success: boolean; data: InferenceStatus }>(
    `/api/channel/${channelId}/${provider}/status`,
    { signal, disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}

export type TaskPluginOption = {
  sortPriority?: number
  website?: string
  key: string
  name: string
  description?: Record<string, string> | null
  icon?: string
  hasIcon?: boolean
  baseUrl?: string
  models: string[]
  channelTypes?: number[] | null
  upstreams?: string[] | null
}

export async function getTaskPluginOptions(): Promise<TaskPluginOption[]> {
  const response = await api.get<{
    success: boolean
    data: TaskPluginOption[]
  }>('/api/task_plugin_options')
  return requireServerSuccess(response.data).data
}

export type CodexUsageResponse = {
  success: boolean
  message?: string
  upstream_status?: number
  data?: Record<string, unknown>
}

export type CodexResetCreditsResponse = CodexUsageResponse

export type CodexUsageResetResponse = CodexUsageResponse

export type CodexCredentialRefreshResponse = {
  success: boolean
  message?: string
  data?: {
    expires_at?: string
    last_refresh?: string
    account_id?: string
    email?: string
    channel_id?: number
    channel_type?: number
    channel_name?: string
  }
}

// ============================================================================
// Base Channel CRUD Operations
// ============================================================================

/**
 * Get paginated list of channels
 */
export async function getChannels(
  params: GetChannelsParams = {}
): Promise<GetChannelsResponse> {
  const res = await api.get('/api/channel', { params })
  return res.data
}

/**
 * Search channels with filters
 */
export async function searchChannels(
  params: SearchChannelsParams
): Promise<SearchChannelsResponse> {
  const res = await api.get('/api/channel/search', { params })
  return res.data
}

/**
 * Get single channel by ID
 */
export async function getChannel(id: number): Promise<GetChannelResponse> {
  const res = await api.get(`/api/channel/${id}`)
  return res.data
}

/**
 * Get channel operations summary for administrators
 */
export async function getChannelOps(
  autoBan?: boolean
): Promise<ChannelOpsResponse> {
  const res = await api.get('/api/channel/ops', {
    ...channelActionConfig(),
    params: autoBan === undefined ? undefined : { auto_ban: autoBan },
  })
  return res.data
}

export async function getChannelDefaultBaseURLs(): Promise<
  Partial<Record<number, string>>
> {
  const response = await api.get<{
    success: boolean
    data: Partial<Record<number, string>>
  }>('/api/channel/default_base_urls')
  return requireServerSuccess(response.data).data
}

/**
 * Create new channel(s)
 * Supports single, batch, and multi-key modes
 */
export async function createChannel(
  data: AddChannelRequest
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post('/api/channel', data, channelActionConfig())
  return res.data
}

/**
 * Update existing channel
 */
export async function updateChannel(
  id: number,
  data: Partial<Channel>
): Promise<{ success: boolean; message?: string; data?: Channel }> {
  const res = await api.put(
    '/api/channel/',
    { id, ...data },
    channelActionConfig()
  )
  return res.data
}

/**
 * Update channel enabled/disabled status.
 */
export async function updateChannelStatus(
  id: number,
  status: number
): Promise<{ success: boolean; message?: string; data?: boolean }> {
  const res = await api.post(
    `/api/channel/${id}/status`,
    { status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Batch update channel enabled/disabled status.
 */
export async function batchUpdateChannelStatus(
  ids: number[],
  status: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/status/batch',
    { ids, status },
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete single channel
 */
export async function deleteChannel(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/channel/${id}`, channelActionConfig())
  return res.data
}

/**
 * Batch delete channels
 */
export async function batchDeleteChannels(
  data: BatchDeleteParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post('/api/channel/batch', data, channelActionConfig())
  return res.data
}

/**
 * Batch set tag for channels
 */
export async function batchSetChannelTag(
  data: BatchSetTagParams
): Promise<{ success: boolean; message?: string; data?: number }> {
  const res = await api.post(
    '/api/channel/batch/tag',
    data,
    channelActionConfig()
  )
  return res.data
}

// ============================================================================
// Channel Operations
// ============================================================================

/**
 * Tests channel connectivity.
 *
 * @param id - Channel identifier.
 * @param params - Optional model, endpoint, stream, and key selection.
 * @param options - Optional per-request abort signal, timeout, and duplicate-GET opt-out.
 * @returns The channel test result.
 */
export async function testChannel(
  id: number,
  params?: {
    model?: string
    endpoint_type?: string
    stream?: boolean
    key_index?: number
  },
  options?: {
    signal?: AbortSignal
    timeout?: number
    disableDuplicate?: boolean
  }
): Promise<ChannelTestResponse> {
  const res = await api.get(
    `/api/channel/test/${id}`,
    channelActionConfig({ params, ...options })
  )
  return res.data
}

/**
 * Update channel balance
 */
export async function updateChannelBalance(
  id: number
): Promise<ChannelBalanceResponse> {
  const res = await api.get(
    `/api/channel/update_balance/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Fetch available models from upstream provider
 */
export async function fetchUpstreamModels(
  id: number
): Promise<FetchModelsResponse> {
  const res = await api.get(
    `/api/channel/fetch_models/${id}`,
    channelActionConfig()
  )
  return res.data
}

/**
 * Copy/clone a channel
 */
export async function copyChannel(
  id: number,
  params: CopyChannelParams = {}
): Promise<CopyChannelResponse> {
  const res = await api.post(
    `/api/channel/copy/${id}`,
    null,
    channelActionConfig({ params })
  )
  return res.data
}

/**
 * Fix channel abilities
 */
export async function fixChannelAbilities(): Promise<{
  success: boolean
  message?: string
  data?: { success: number; fails: number }
}> {
  const res = await api.post(
    '/api/channel/fix',
    undefined,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete all disabled channels
 */
export async function deleteDisabledChannels(): Promise<{
  success: boolean
  message?: string
  data?: number
}> {
  const res = await api.delete('/api/channel/disabled', channelActionConfig())
  return res.data
}

/**
 * Get channel key (requires 2FA verification)
 */
export async function getChannelKey(
  id: number,
  proofToken: string,
  signal?: AbortSignal
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const res = await api.post(
    `/api/channel/${id}/key`,
    undefined,
    channelActionConfig({
      headers: { 'X-Security-Proof': proofToken },
      signal,
    })
  )
  return res.data
}

/**
 * Export channel configurations including plaintext keys.
 * Root-only and gated by secure verification, same as getChannelKey.
 *
 * @param ids - Channel ids to export; empty exports all channels.
 * @param proofToken - Security proof issued by the verification flow.
 * @returns The versioned export payload.
 */
export async function exportChannels(
  ids: number[] = [],
  proofToken: string,
  signal?: AbortSignal
): Promise<ExportChannelsResponse> {
  const res = await api.post(
    '/api/channel/export',
    { ids },
    channelActionConfig({
      headers: { 'X-Security-Proof': proofToken },
      signal,
    })
  )
  return res.data
}

// ============================================================================
// Codex Channel Operations
// ============================================================================

export async function refreshCodexCredential(
  channelId: number
): Promise<CodexCredentialRefreshResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/refresh`,
    {},
    channelActionConfig()
  )
  return res.data
}

export async function getCodexUsage(
  channelId: number
): Promise<CodexUsageResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function getCodexResetCredits(
  channelId: number
): Promise<CodexResetCreditsResponse> {
  const res = await api.get(
    `/api/channel/${channelId}/codex/usage/reset-credits`,
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

export async function resetCodexUsage(
  channelId: number
): Promise<CodexUsageResetResponse> {
  const res = await api.post(
    `/api/channel/${channelId}/codex/usage/reset`,
    {},
    channelActionConfig({ disableDuplicate: true })
  )
  return res.data
}

// ============================================================================
// Multi-Key Management
// ============================================================================

/**
 * Manage multi-key channel operations
 *
 * @param params - The action and its arguments.
 * @param options - Optional per-request abort signal, forwarded to axios.
 */
export async function manageMultiKeys(
  params: MultiKeyManageParams,
  options?: { signal?: AbortSignal }
): Promise<MultiKeyStatusResponse | { success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/multi_key/manage',
    params,
    channelActionConfig(options)
  )
  return res.data
}

/**
 * Get key status for multi-key channel.
 *
 * Pass `options.signal` when reading a channel's keys page by page: the dialog's
 * walk is up to ten sequential requests, each one rebuilding the whole key-status
 * list server-side and serialising up to a page of rows whose `reason` can carry
 * an entire upstream error body. Without a signal, closing the dialog only stops
 * react-query from caring about the answer — the walk keeps asking.
 *
 * @param channelId - Channel whose keys are read.
 * @param page - 1-based page number.
 * @param pageSize - Keys per page.
 * @param status - Optional status filter; omit to read every key.
 * @param options - Optional per-request abort signal.
 * @returns One page of key statuses plus the channel-wide counters.
 */
export async function getMultiKeyStatus(
  channelId: number,
  page = 1,
  pageSize = 50,
  status?: number,
  options?: { signal?: AbortSignal }
): Promise<MultiKeyStatusResponse> {
  return manageMultiKeys(
    {
      channel_id: channelId,
      action: 'get_key_status',
      page,
      page_size: pageSize,
      status,
    },
    options
  ) as Promise<MultiKeyStatusResponse>
}

// The three single-key actions below share one response envelope with the batch
// actions: HTTP 200 plus `success`, an optional `message`, a machine-readable
// `error_code` and an optional `data` block. They are typed as
// `MultiKeyBatchResponse` so callers can route them through the same
// `error_code` translation the batch actions use.
//
// They also share the batch actions' `expectedKeyCount` precondition, and its
// limit: the server compares only the CURRENT key COUNT, so it catches a key
// added or removed since the caller snapshotted the list, but a count-preserving
// reshuffle (delete one plus append one, or a same-size `KeyMode='replace'`)
// still passes and a stale index then lands on a different key. Treat it as a
// cheap staleness guard, not a full optimistic-concurrency check.

/**
 * Enable a specific key in multi-key channel
 * `expectedKeyCount` is a concurrency precondition: the action is rejected with
 * `error_code: 'key_list_changed'` when the stored key count differs, so an
 * index captured from a stale list never enables somebody else's key.
 */
export async function enableMultiKey(
  channelId: number,
  keyIndex: number,
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_key',
    key_index: keyIndex,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

/**
 * Disable a specific key in multi-key channel, recording an optional reason.
 * Passing `undefined` for the reason lets the server fall back to its own
 * default; it stays positional (mirroring `disableKeys`) so the required
 * `expectedKeyCount` can follow it.
 * `expectedKeyCount` is a concurrency precondition: the action is rejected with
 * `error_code: 'key_list_changed'` when the stored key count differs, so the
 * reviewed reason is never stamped onto a neighbouring key.
 */
export async function disableMultiKey(
  channelId: number,
  keyIndex: number,
  reason: string | undefined,
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_key',
    key_index: keyIndex,
    reason,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

/**
 * Delete a specific key in multi-key channel
 * `expectedKeyCount` is a concurrency precondition: the action is rejected with
 * `error_code: 'key_list_changed'` when the stored key count differs, so a
 * stale index never permanently deletes a key the operator did not select.
 */
export async function deleteMultiKey(
  channelId: number,
  keyIndex: number,
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_key',
    key_index: keyIndex,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

/**
 * Enable all keys in multi-key channel
 */
export async function enableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Disable all keys in multi-key channel
 */
export async function disableAllMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_all_keys',
  }) as Promise<{ success: boolean; message?: string }>
}

/**
 * Delete all disabled keys in multi-key channel
 */
export async function deleteDisabledMultiKeys(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: number }> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_disabled_keys',
  }) as Promise<{ success: boolean; message?: string; data?: number }>
}

/**
 * Enable the given keys in a multi-key channel.
 * `expectedKeyCount` is a concurrency precondition: the whole batch is rejected
 * with `error_code: 'key_list_changed'` when the stored key count differs.
 */
export async function enableKeys(
  channelId: number,
  keyIndexes: number[],
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'enable_keys',
    key_indexes: keyIndexes,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

/**
 * Disable the given keys in a multi-key channel, recording a reason per key.
 * `expectedKeyCount` is a concurrency precondition: the whole batch is rejected
 * with `error_code: 'key_list_changed'` when the stored key count differs, so a
 * batch computed against a stale list never stamps the reason onto other keys.
 */
export async function disableKeys(
  channelId: number,
  keyIndexes: number[],
  reason: string | undefined,
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'disable_keys',
    key_indexes: keyIndexes,
    reason,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

/**
 * Delete the given keys in a multi-key channel.
 * `expectedKeyCount` is a concurrency precondition: the whole batch is rejected
 * with `error_code: 'key_list_changed'` when the stored key count differs.
 */
export async function deleteKeys(
  channelId: number,
  keyIndexes: number[],
  expectedKeyCount: number
): Promise<MultiKeyBatchResponse> {
  return manageMultiKeys({
    channel_id: channelId,
    action: 'delete_keys',
    key_indexes: keyIndexes,
    expected_key_count: expectedKeyCount,
  }) as Promise<MultiKeyBatchResponse>
}

// ============================================================================
// Tag Operations
// ============================================================================

/**
 * Enable all channels with a specific tag
 */
export async function enableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/enabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Disable all channels with a specific tag
 */
export async function disableTagChannels(
  tag: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.post(
    '/api/channel/tag/disabled',
    { tag },
    channelActionConfig()
  )
  return res.data
}

/**
 * Edit all channels with a specific tag
 */
export async function editTagChannels(
  params: TagOperationParams
): Promise<{ success: boolean; message?: string }> {
  const res = await api.put('/api/channel/tag', params, channelActionConfig())
  return res.data
}

/**
 * Get models for a specific tag
 */
export async function getTagModels(
  tag: string
): Promise<{ success: boolean; message?: string; data?: string }> {
  const res = await api.get('/api/channel/tag/models', { params: { tag } })
  return res.data
}

// ============================================================================
// Utility Functions
// ============================================================================

/**
 * Fetch models from the current unsaved channel form configuration.
 */
export async function fetchModels(data: {
  base_url: string
  type: number
  key?: string
  channel_id?: number
  advanced_custom?: string
  header_override?: string
  proxy?: string
}): Promise<FetchModelsResponse> {
  const res = await api.post(
    '/api/channel/fetch_models',
    data,
    channelActionConfig()
  )
  return res.data
}

/**
 * Delete an Ollama model from a channel
 */
export async function deleteOllamaModel(params: {
  channel_id: number
  model_name: string
}): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(
    '/api/channel/ollama/delete',
    channelActionConfig({ data: params })
  )
  return res.data
}

/**
 * Test all enabled channels
 */
export async function testAllChannels(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get('/api/channel/test', channelActionConfig())
  return res.data
}

/**
 * Update balance for all enabled channels
 */
export async function updateAllChannelsBalance(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.get(
    '/api/channel/update_balance',
    channelActionConfig()
  )
  return res.data
}

/**
 * Get all available models
 */
export async function getAllModels(): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: string; [key: string]: unknown }>
}> {
  const res = await api.get('/api/channel/models')
  return res.data
}

/**
 * Get all enabled models
 */
export async function getEnabledModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/channel/models_enabled')
  return res.data
}

// ============================================================================
// Ollama Utilities
// ============================================================================

/**
 * Check Ollama version for a given channel
 */
export async function getOllamaVersion(
  channelId: number
): Promise<{ success: boolean; message?: string; data?: { version: string } }> {
  const res = await api.get(`/api/channel/ollama/version/${channelId}`)
  return res.data
}

// ============================================================================
// Group Management
// ============================================================================

/**
 * Get all available groups (re-exported from users API for convenience)
 */
export const getGroups = getUserGroups

// ============================================================================
// Prefill Groups (Model Groups)
// ============================================================================

/**
 * Get prefill groups for quick model selection
 */
export async function getPrefillGroups(
  type: 'model' | 'group' = 'model'
): Promise<{
  success: boolean
  message?: string
  data?: Array<{ id: number; name: string; items: string | string[] }>
}> {
  const res = await api.get('/api/prefill_group', { params: { type } })
  return res.data
}
