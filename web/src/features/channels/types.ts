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
import { z } from 'zod'

// ============================================================================
// Channel Schema & Types
// ============================================================================

export const channelInfoSchema = z.object({
  is_multi_key: z.boolean().default(false),
  multi_key_size: z.number().default(0),
  multi_key_status_list: z.record(z.string(), z.number()).optional(),
  multi_key_disabled_reason: z.record(z.string(), z.string()).optional(),
  multi_key_disabled_time: z.record(z.string(), z.number()).optional(),
  multi_key_polling_index: z.number().default(0),
  multi_key_mode: z.enum(['random', 'polling']).default('random'),
})

export type ChannelInfo = z.infer<typeof channelInfoSchema>

export const channelSchema = z.object({
  id: z.number(),
  type: z.number(),
  key: z.string(),
  openai_organization: z.string().nullish(),
  test_model: z.string().nullish(),
  status: z.number(), // 1: enabled, 0: manual disabled, 2: auto disabled
  name: z.string(),
  weight: z.number().nullish(),
  created_time: z.number(),
  test_time: z.number(),
  response_time: z.number(), // in milliseconds
  base_url: z.string().nullish(),
  other: z.string().default(''),
  balance: z.number().default(0), // in USD
  balance_updated_time: z.number(),
  models: z.string().default(''),
  group: z.string().default('default'),
  used_quota: z.number().default(0),
  model_mapping: z.string().nullish(),
  status_code_mapping: z.string().nullish(),
  priority: z.number().nullish(),
  auto_ban: z.number().nullish(),
  other_info: z.string().default(''),
  tag: z.string().nullish(),
  setting: z.string().nullish(),
  param_override: z.string().nullish(),
  header_override: z.string().nullish(),
  remark: z.string().default(''),
  max_input_tokens: z.number().default(0),
  channel_info: channelInfoSchema.default({
    is_multi_key: false,
    multi_key_size: 0,
    multi_key_polling_index: 0,
    multi_key_mode: 'random',
  }),
  settings: z.string().default('{}'), // other_settings JSON
})

export type Channel = z.infer<typeof channelSchema>

// ============================================================================
// Channel Settings Types
// ============================================================================

export interface ChannelSettings {
  task_plugin_key?: string
  task_extend_plugin_keys?: string[]
  force_format?: boolean
  thinking_to_content?: boolean
  proxy?: string
  pass_through_body_enabled?: boolean
  responses_websocket_enabled?: boolean
  system_prompt?: string
  system_prompt_override?: boolean
  empty_response_no_billing_enabled?: boolean
  sensitive_check_enabled?: boolean
  /** Tri-state channel override for request/response anonymization: undefined inherits the global switch. */
  anonymization_enabled?: boolean
  sensitive_mode?: 'modify' | 'override'
  sensitive_added_words?: string[]
  sensitive_removed_words?: string[]
  sensitive_override_words?: string[]
  /** Rewrites response model fields back to the client-requested model name (all upstreams). */
  unify_request_response_model_name_enabled?: boolean
  http_protocol?: 'auto' | 'http1' | string
  http2_connection_shards?: number
  tls_insecure_skip_verify?: boolean
}

export interface ClaudeCodeSimulationMetadata {
  [key: string]: string
  device_id: string
  account_uuid: string
  session_id: string
}

export interface ClaudeCodeSimulationConfig {
  headers?: Record<string, string>
  metadata?: ClaudeCodeSimulationMetadata
  system?: unknown[]
}

export interface ChannelOtherSettings {
  azure_responses_version?: string
  vertex_key_type?: 'json' | 'api_key'
  openrouter_enterprise?: boolean
  aws_key_type?: 'ak_sk' | 'api_key'
  allow_service_tier?: boolean
  disable_store?: boolean
  allow_safety_identifier?: boolean
  allow_include_obfuscation?: boolean
  allow_inference_geo?: boolean
  allow_speed?: boolean
  claude_beta_query?: boolean
  /** Explicit body rules override legacy provider defaults for matching fields. */
  body_field_pass_through?: FieldPassThroughRule[]
  /** Header rules are restricted by the backend's unsafe-header denylist. */
  header_field_pass_through?: FieldPassThroughRule[]
  ollama_openai_chat?: boolean
  disable_task_polling_sleep?: boolean
  upstream_model_update_check_enabled?: boolean
  upstream_model_update_auto_sync_enabled?: boolean
  upstream_model_update_ignored_models?: string[]
  upstream_model_update_last_check_time?: number
  upstream_model_update_last_detected_models?: string[]
  advanced_custom?: AdvancedCustomConfig
  /** Claude force-cache: inject ephemeral cache_control when the request carries none (Claude upstreams). */
  force_cache_enabled?: boolean
  /** TTL for injected cache_control: '' / '5m' => 5 minutes, '1h' => 1 hour. */
  force_cache_ttl?: '5m' | '1h'
  /** Rewrites every existing cache_control ttl to 1h (Claude upstreams). */
  force_cache_1h_enabled?: boolean
  /** Folds 1h cache creation tokens into the 5m bucket for billing (Claude upstreams). */
  cache_1h_bill_as_5m_enabled?: boolean
  /** Enforces a minimum cache-read share of read+write (Claude upstreams). */
  cache_read_min_ratio_enabled?: boolean
  /** Minimum cache-read ratio as a 0-1 fraction. */
  cache_read_min_ratio?: number
  /** Two-stage thinking/signature strip-and-retry on >=400 (Claude upstreams). */
  thinking_signature_rectifier_enabled?: boolean
  /** Full Claude Code CLI request simulation (Claude channel only). */
  claude_code_simulation_enabled?: boolean
  /** Configurable Claude Code request fingerprint; omitted fields use backend defaults. */
  claude_code_simulation?: ClaudeCodeSimulationConfig
  /** Strip encrypted reasoning items and replay once on verification errors (OpenAI/Azure Responses). */
  responses_encrypted_content_rectifier_enabled?: boolean
}

/** FieldPassThroughRule is a persisted allow/block decision for one field. */
export interface FieldPassThroughRule {
  field: string
  enabled: boolean
  comment?: string
}

export interface AdvancedCustomConfig {
  advanced_routes?: AdvancedCustomRoute[]
}

export interface AdvancedCustomRoute {
  incoming_path?: string
  upstream_path?: string
  converter?: AdvancedCustomConverter
  models?: string[]
  auth?: AdvancedCustomRouteAuth
  pass_through_body_enabled?: boolean
}

export interface AdvancedCustomRouteAuth {
  type?: AdvancedCustomAuthType
  name?: string
  value?: string
}

export type AdvancedCustomConverter =
  | 'none'
  | 'anthropic_messages_to_openai_chat_completions'
  | 'openai_chat_completions_to_anthropic_messages'
  | 'openai_chat_completions_to_openai_responses'
  | 'openai_responses_to_openai_chat_completions'
  | 'openai_responses_to_gemini_generate_content'
  | 'gemini_generate_content_to_openai_chat_completions'
  | 'openai_chat_completions_to_gemini_generate_content'
  | 'claude_messages_to_openai_responses'
  | 'gemini_generate_content_to_openai_responses'

export type AdvancedCustomAuthType = 'none' | 'header' | 'query'

// ============================================================================
// API Response Types
// ============================================================================

export interface GetChannelsResponse {
  success: boolean
  message?: string
  data?: {
    items: Channel[]
    total: number
    page: number
    page_size: number
    type_counts?: Record<string, number>
  }
}

export interface SearchChannelsResponse {
  success: boolean
  message?: string
  data?: {
    items: Channel[]
    total: number
    type_counts?: Record<string, number>
  }
}

export interface GetChannelResponse {
  success: boolean
  message?: string
  data?: Channel
}

export interface ChannelOpsResponse {
  success: boolean
  message?: string
  data?: {
    retry_times: number
    request_policy?: {
      automatic_disable: boolean
      source: string
    }
  }
}

export interface ChannelTestResponse {
  success: boolean
  message?: string
  error_code?: string
  time?: number
  /** Upstream HTTP status code; absent = no upstream HTTP response was ever received */
  upstream_status_code?: number
  /**
   * True when this key's own credential already took part in a real upstream
   * call: the server raises it at the `adaptor.DoRequest` boundary in
   * `controller/channel-test.go`, so a failure carrying it is a verdict on THIS
   * key. Absent rather than false when the server never got that far —
   * everything before that boundary is request construction and fails
   * identically for every key of the channel.
   */
  key_attempted?: boolean
  /** Echoed back only when the request targeted one key of a multi-key channel */
  key_index?: number
  data?: {
    response_time?: number
    error?: string
  }
}

export interface ChannelBalanceResponse {
  success: boolean
  message?: string
  balance?: number
  currency?: string
  raw_response?: string
}

export interface FetchModelsResponse {
  success: boolean
  message?: string
  data?: string[]
}

export interface CopyChannelResponse {
  success: boolean
  message?: string
  data?: {
    id: number
  }
}

// ============================================================================
// Channel Export / Import Types
// ============================================================================

/**
 * One channel entry inside an export file. Mirrors the backend
 * channelExportItem: config-only fields, no runtime statistics.
 */
export const channelExportItemSchema = z.object({
  type: z.number().int(),
  name: z.string().min(1),
  key: z.string().default(''),
  base_url: z.string().nullish(),
  other: z.string().nullish(),
  models: z.string().default(''),
  group: z.string().default('default'),
  model_mapping: z.string().nullish(),
  status_code_mapping: z.string().nullish(),
  priority: z.number().nullish(),
  weight: z.number().nullish(),
  auto_ban: z.number().nullish(),
  test_model: z.string().nullish(),
  openai_organization: z.string().nullish(),
  tag: z.string().nullish(),
  setting: z.string().nullish(),
  param_override: z.string().nullish(),
  header_override: z.string().nullish(),
  remark: z.string().nullish(),
  status: z.number().default(1),
  channel_info: z
    .object({
      is_multi_key: z.boolean().default(false),
      multi_key_mode: z.enum(['random', 'polling']).nullish(),
    })
    .nullish(),
  settings: z.string().nullish(),
})

export type ChannelExportItem = z.infer<typeof channelExportItemSchema>

/** Accepts both the versioned export envelope and a bare channel array. */
export const channelImportFileSchema = z.union([
  z.object({
    version: z.number().optional(),
    channels: z.array(channelExportItemSchema).min(1),
  }),
  z.array(channelExportItemSchema).min(1),
])

export interface ExportChannelsResponse {
  success: boolean
  message?: string
  data?: {
    version: number
    channels: ChannelExportItem[]
  }
}

// ============================================================================
// Multi-Key Management Types
// ============================================================================

export interface KeyStatus {
  index: number
  status: number // 1: enabled, 2: manual disabled, 3: auto disabled
  disabled_time?: number
  reason?: string
  key_preview?: string
}

export type MultiKeyConfirmAction = {
  type:
    | 'enable'
    | 'disable'
    | 'delete'
    | 'enable-all'
    | 'disable-all'
    | 'delete-disabled'
  keyIndex?: number
}

export interface MultiKeyStatusResponse {
  success: boolean
  message?: string
  data?: {
    keys: KeyStatus[]
    total: number
    page: number
    page_size: number
    total_pages: number
    enabled_count: number
    manual_disabled_count: number
    auto_disabled_count: number
  }
}

/**
 * Outcome counters of one batch action. `affected_count` and `skipped_count` are
 * reported separately because the server skips keys that are already in the
 * target state, so the requested count alone would make the toast lie.
 */
export interface MultiKeyBatchCounts {
  affected_count?: number
  skipped_count?: number
  remaining_count?: number
}

/**
 * Stable, machine-readable rejection codes of the multi-key actions, mirroring
 * the Go constants in `controller/channel.go:1856-1864`. The server pairs each
 * one with a Chinese `message` that only exists as a fallback for non-UI
 * clients, so the UI must translate off these instead.
 *
 * Declared as a closed union on purpose: the caller switches on it, so a
 * mistyped literal is a compile error rather than a silent fall-through that
 * toasts the Chinese message at an English-speaking admin. A code added server
 * side and not yet listed here still lands on that same fallback at runtime,
 * which is why the switch keeps its default branch.
 */
export type MultiKeyBatchErrorCode =
  | 'key_list_changed'
  | 'key_indexes_empty'
  | 'key_index_out_of_range'
  | 'no_keys_to_disable'
  | 'cannot_delete_all_keys'
  | 'all_keys_auto_disabled'
  | 'key_already_disabled'

/** Shared response shape of the three multi-key batch actions. */
export interface MultiKeyBatchResponse {
  success: boolean
  message?: string
  /**
   * The only machine-readable failure field the server writes; present on every
   * rejection of every multi-key action, batch and single-key alike. There is no
   * sibling `code` field on the wire.
   */
  error_code?: MultiKeyBatchErrorCode
  data?: MultiKeyBatchCounts
}

// ============================================================================
// API Request Parameters
// ============================================================================

export type ChannelSortBy =
  | 'id'
  | 'name'
  | 'priority'
  | 'balance'
  | 'response_time'
  | 'test_time'

export type ChannelSortOrder = 'asc' | 'desc'

export interface GetChannelsParams {
  p?: number
  page_size?: number
  status?: string // 'enabled', 'disabled', or empty for all
  type?: number
  group?: string
  id_sort?: boolean
  tag_mode?: boolean
  sort_by?: ChannelSortBy
  sort_order?: ChannelSortOrder
}

export interface SearchChannelsParams {
  keyword?: string
  group?: string
  model?: string
  status?: string
  type?: number
  id_sort?: boolean
  tag_mode?: boolean
  sort_by?: ChannelSortBy
  sort_order?: ChannelSortOrder
  p?: number
  page_size?: number
}

export interface ChannelTestParams {
  test_model?: string
}

export interface CopyChannelParams {
  suffix?: string
  reset_balance?: boolean
}

export interface MultiKeyManageParams {
  channel_id: number
  action:
    | 'get_key_status'
    | 'disable_key'
    | 'enable_key'
    | 'enable_all_keys'
    | 'disable_all_keys'
    | 'delete_key'
    | 'delete_disabled_keys'
    | 'enable_keys'
    | 'disable_keys'
    | 'delete_keys'
  key_index?: number
  /** Target indexes for the batch actions; deduplicated and bounds-checked server side */
  key_indexes?: number[]
  /** Disabled reason recorded per key by disable_keys; max 255 runes */
  reason?: string
  /** Concurrency precondition for delete_keys: rejected unless it matches the current key count */
  expected_key_count?: number
  page?: number
  page_size?: number
  status?: number // 1=enabled, 2=manual_disabled, 3=auto_disabled
}

export interface BatchDeleteParams {
  ids: number[]
}

export interface BatchSetTagParams {
  ids: number[]
  tag: string | null
}

export interface TagOperationParams {
  tag: string
  new_tag?: string
  priority?: number
  weight?: number
  model_mapping?: string
  models?: string
  groups?: string
}

// ============================================================================
// Form Data Types
// ============================================================================

export interface ChannelFormData {
  name: string
  type: number
  base_url: string
  key: string
  openai_organization?: string
  models: string
  group: string
  model_mapping?: string
  priority?: number
  weight?: number
  test_model?: string
  auto_ban?: number
  status: number
  status_code_mapping?: string
  tag?: string
  remark?: string
  setting?: string
  param_override?: string
  header_override?: string
  settings?: string
  other?: string
  // Multi-key specific
  multi_key_mode?: 'single' | 'batch' | 'multi_to_single'
  multi_key_type?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
}

// ============================================================================
// Add Channel Request (special structure)
// ============================================================================

export interface AddChannelRequest {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
}
