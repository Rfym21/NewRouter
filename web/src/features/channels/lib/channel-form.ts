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

import {
  CLAUDE_FIELD_PASSTHROUGH_TYPES,
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_OLLAMA,
  CHANNEL_TYPE_TASK_PLUGIN,
  CHANNEL_TYPE_VLLM,
  CHANNEL_TYPE_SGLANG,
  CHANNEL_STATUS,
  ERROR_MESSAGES,
  FIELD_PASSTHROUGH_TYPES,
  MODEL_FETCHABLE_TYPES,
  OPENAI_FIELD_PASSTHROUGH_TYPES,
} from '../constants'
import type { Channel, ChannelSettings } from '../types'
import {
  CHANNEL_TYPE_ADVANCED_CUSTOM,
  advancedCustomConfigUsesRelativeUpstreamPath,
  hasValidAdvancedCustomModelListRoute,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
} from './advanced-custom'
import { readTaskExtendPluginKeys } from './channel-plugin-extensions'
import {
  CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS,
  CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS_JSON,
  CLAUDE_CODE_SIMULATION_DEFAULT_METADATA,
  CLAUDE_CODE_SIMULATION_DEFAULT_METADATA_JSON,
  CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM,
  CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM_JSON,
} from './claude-code-simulation'
import {
  fieldPassThroughRulesSchema,
  formatFieldPassThroughRules,
  isFieldPassThroughRulesText,
  parseFieldPassThroughRules,
  sanitizeFieldPassThroughRules,
} from './field-passthrough'
import { supportsResponsesWebSocket } from './responses-websocket'

// ============================================================================
// Form Validation Schema
// ============================================================================

const HTTP_HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/

function isValidHttpHeaderValue(value: string): boolean {
  for (const character of value) {
    const codePoint = character.codePointAt(0)
    if (codePoint === undefined) return false
    if ((codePoint <= 31 && codePoint !== 9) || codePoint === 127) return false
  }
  return true
}

const SUPPORTED_PROXY_PROTOCOLS = new Set([
  'http:',
  'https:',
  'socks5:',
  'socks5h:',
])

function isOptionalProxyURL(value: string | undefined): boolean {
  const trimmedValue = value?.trim() || ''
  if (!trimmedValue) return true

  const schemeSeparatorIndex = trimmedValue.indexOf('://')
  if (schemeSeparatorIndex <= 0) return false

  const authorityAndSuffix = trimmedValue.slice(schemeSeparatorIndex + 3)
  const suffixIndex = authorityAndSuffix.search(/[/?#]/)
  if (suffixIndex >= 0 && authorityAndSuffix.slice(suffixIndex) !== '/') {
    return false
  }

  try {
    const parsedURL = new URL(trimmedValue)
    return (
      SUPPORTED_PROXY_PROTOCOLS.has(parsedURL.protocol) &&
      Boolean(parsedURL.hostname) &&
      parsedURL.port !== '0'
    )
  } catch {
    return false
  }
}

export const HTTP_PROTOCOL_AUTO = 'auto'
export const HTTP_PROTOCOL_HTTP1 = 'http1'
export const MAX_HTTP2_CONNECTION_SHARDS = 8

export function normalizeHttpProtocol(
  value: string | undefined | null
): 'auto' | 'http1' {
  const normalized = String(value || '')
    .trim()
    .toLowerCase()
  if (normalized === HTTP_PROTOCOL_HTTP1) {
    return HTTP_PROTOCOL_HTTP1
  }
  return HTTP_PROTOCOL_AUTO
}

export function normalizeHttp2ConnectionShards(
  value: number | undefined | null
): number {
  if (value == null || Number.isNaN(value) || value === 0) {
    return 1
  }
  if (value < 1) {
    return 1
  }
  if (value > MAX_HTTP2_CONNECTION_SHARDS) {
    return MAX_HTTP2_CONNECTION_SHARDS
  }
  return value
}

function parseOptionalJson(value: string | undefined): unknown {
  if (!value?.trim()) return undefined
  return JSON.parse(value)
}

function isJsonObjectValue(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalJsonObject(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    return parsed === undefined || isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function isClaudeCodeSimulationHeaders(value: string): boolean {
  try {
    const parsed = JSON.parse(value) as unknown
    if (!isJsonObjectValue(parsed)) return false

    const normalizedNames = new Set<string>()
    for (const [name, headerValue] of Object.entries(parsed)) {
      const normalizedName = name.toLowerCase()
      if (
        !HTTP_HEADER_NAME_PATTERN.test(name) ||
        normalizedNames.has(normalizedName) ||
        typeof headerValue !== 'string' ||
        !isValidHttpHeaderValue(headerValue)
      ) {
        return false
      }
      normalizedNames.add(normalizedName)
    }
    return true
  } catch {
    return false
  }
}

function isClaudeCodeSimulationMetadata(value: string): boolean {
  try {
    const parsed = JSON.parse(value) as unknown
    if (!isJsonObjectValue(parsed)) return false

    return (
      Object.values(parsed).every((item) => typeof item === 'string') &&
      typeof parsed.device_id === 'string' &&
      parsed.device_id.trim().length > 0 &&
      typeof parsed.account_uuid === 'string' &&
      typeof parsed.session_id === 'string' &&
      parsed.session_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isClaudeCodeSimulationSystem(value: string): boolean {
  try {
    const parsed = JSON.parse(value) as unknown
    return (
      Array.isArray(parsed) &&
      parsed.every(
        (item) => isJsonObjectValue(item) && Object.keys(item).length > 0
      )
    )
  } catch {
    return false
  }
}

function stringifyJsonValue(value: unknown, fallback: string): string {
  if (value === undefined || value === null) return fallback
  return JSON.stringify(value, null, 2) ?? fallback
}

function parseJsonOrFallback(value: string, fallback: unknown): unknown {
  try {
    return JSON.parse(value) as unknown
  } catch {
    return fallback
  }
}

function isOptionalModelMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.values(parsed).every((item) => typeof item === 'string')
  } catch {
    return false
  }
}

function isOptionalStatusCodeMapping(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (!isJsonObjectValue(parsed)) return false
    return Object.entries(parsed).every(([from, to]) => {
      const fromCode = Number(from)
      const toCode = Number(to)
      return (
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599
      )
    })
  } catch {
    return false
  }
}

function parseSensitiveWordList(value: unknown): string {
  if (!Array.isArray(value)) return ''

  return value
    .filter((word): word is string => typeof word === 'string')
    .join('\n')
}

function splitSensitiveWordLines(value: string | undefined): string[] {
  return String(value || '')
    .split('\n')
    .map((word) => word.trim())
    .filter(Boolean)
}

function isCodexCredential(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    return (
      isJsonObjectValue(parsed) &&
      typeof parsed.access_token === 'string' &&
      parsed.access_token.trim().length > 0 &&
      typeof parsed.account_id === 'string' &&
      parsed.account_id.trim().length > 0
    )
  } catch {
    return false
  }
}

function isVertexJsonKey(value: string | undefined): boolean {
  try {
    const parsed = parseOptionalJson(value)
    if (parsed === undefined) return true
    if (Array.isArray(parsed)) {
      return parsed.every((item) => isJsonObjectValue(item))
    }
    return isJsonObjectValue(parsed)
  } catch {
    return false
  }
}

function addRequiredIssue(
  ctx: z.RefinementCtx,
  path: string,
  message: string
): void {
  ctx.addIssue({
    code: z.ZodIssueCode.custom,
    path: [path],
    message,
  })
}

export const channelFormSchema = z
  .object({
    name: z.string().min(1, ERROR_MESSAGES.REQUIRED_NAME),
    type: z.number().min(0, ERROR_MESSAGES.REQUIRED_TYPE),
    base_url: z.string().optional(),
    task_plugin_key: z.string().optional(),
    task_extend_plugin_keys: z.array(z.string()).optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, ERROR_MESSAGES.REQUIRED_MODELS),
    group: z.array(z.string()).min(1, ERROR_MESSAGES.REQUIRED_GROUP),
    model_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalModelMapping,
        'Model mapping must be a JSON object with string values'
      ),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z
      .string()
      .optional()
      .refine(
        isOptionalStatusCodeMapping,
        'Status code mapping must use valid HTTP status codes'
      ),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    param_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    header_override: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    settings: z
      .string()
      .optional()
      .refine(isOptionalJsonObject, ERROR_MESSAGES.INVALID_JSON),
    advanced_custom: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z
      .string()
      .optional()
      .refine(isOptionalProxyURL, ERROR_MESSAGES.INVALID_PROXY),
    http_protocol: z.enum(['auto', 'http1']).optional(),
    http2_connection_shards: z.number().int().optional(),
    tls_insecure_skip_verify: z.boolean().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    responses_websocket_enabled: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
    empty_response_no_billing_mode: z.enum(['inherit', 'enabled', 'disabled']),
    sensitive_check_mode: z.enum(['inherit', 'enabled', 'disabled']),
    anonymization_mode: z.enum(['inherit', 'enabled', 'disabled']),
    sensitive_mode: z.enum(['inherit', 'modify', 'override']),
    sensitive_added_words: z.string(),
    sensitive_removed_words: z.string(),
    sensitive_override_words: z.string(),
    // Type-specific settings (stored in settings JSON)
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    body_field_pass_through_rules: fieldPassThroughRulesSchema,
    body_field_pass_through_text: z
      .string()
      .refine(
        isFieldPassThroughRulesText,
        'Field passthrough rules must be a valid JSON array'
      ),
    header_field_pass_through_rules: fieldPassThroughRulesSchema,
    header_field_pass_through_text: z
      .string()
      .refine(
        isFieldPassThroughRulesText,
        'Field passthrough rules must be a valid JSON array'
      ),
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    // Claude upstream (Anthropic/AWS/Vertex) cache & rectifier controls (stored in settings JSON)
    force_cache_enabled: z.boolean().optional(),
    force_cache_ttl: z.enum(['5m', '1h']).optional(),
    force_cache_1h_enabled: z.boolean().optional(),
    cache_1h_bill_as_5m_enabled: z.boolean().optional(),
    cache_read_min_ratio_enabled: z.boolean().optional(),
    cache_read_min_ratio_percent: z.number().min(0).max(100).optional(),
    thinking_signature_rectifier_enabled: z.boolean().optional(),
    claude_code_simulation_enabled: z.boolean().optional(),
    claude_code_simulation_headers: z.string(),
    claude_code_simulation_metadata: z.string(),
    claude_code_simulation_system: z.string(),
    // OpenAI/Azure Responses rectifier (stored in settings JSON)
    responses_encrypted_content_rectifier_enabled: z.boolean().optional(),
    // Unify request/response model name (stored in setting JSON, all upstreams)
    unify_request_response_model_name_enabled: z.boolean().optional(),
    ollama_openai_chat: z.boolean().optional(), // Ollama: OpenAI-compatible /v1/chat/completions instead of native /api/chat
    disable_task_polling_sleep: z.boolean().optional(),
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
  })
  .superRefine((data, ctx) => {
    if (
      [14, 33, 41].includes(data.type) &&
      data.claude_code_simulation_enabled === true
    ) {
      if (
        data.type === 14 &&
        !isClaudeCodeSimulationHeaders(data.claude_code_simulation_headers)
      ) {
        addRequiredIssue(
          ctx,
          'claude_code_simulation_headers',
          'Claude Code simulation headers must be a JSON object with string values'
        )
      }
      if (
        !isClaudeCodeSimulationMetadata(data.claude_code_simulation_metadata)
      ) {
        addRequiredIssue(
          ctx,
          'claude_code_simulation_metadata',
          'Claude Code simulation metadata must contain device_id, account_uuid, and session_id strings'
        )
      }
      if (!isClaudeCodeSimulationSystem(data.claude_code_simulation_system)) {
        addRequiredIssue(
          ctx,
          'claude_code_simulation_system',
          'Claude Code simulation system prompt must be a JSON array'
        )
      }
    }

    if (
      [
        3,
        8,
        36,
        45,
        CHANNEL_TYPE_NEW_API,
        CHANNEL_TYPE_TASK_PLUGIN,
        CHANNEL_TYPE_VLLM,
        CHANNEL_TYPE_SGLANG,
      ].includes(data.type) &&
      !data.base_url?.trim()
    ) {
      addRequiredIssue(
        ctx,
        'base_url',
        'Base URL is required for this channel type'
      )
    }
    if (
      data.type === CHANNEL_TYPE_TASK_PLUGIN &&
      !data.task_plugin_key?.trim()
    ) {
      addRequiredIssue(ctx, 'task_plugin_key', 'Task plugin is required')
    }

    if (data.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
      const advancedCustomConfig = parseAdvancedCustomConfig(
        data.advanced_custom
      )
      const advancedCustomError =
        validateAdvancedCustomConfig(advancedCustomConfig)
      if (advancedCustomError) {
        addRequiredIssue(ctx, 'advanced_custom', advancedCustomError.message)
      }
      if (
        advancedCustomConfigUsesRelativeUpstreamPath(advancedCustomConfig) &&
        !data.base_url?.trim()
      ) {
        addRequiredIssue(
          ctx,
          'base_url',
          'Base URL is required when an advanced route uses an upstream path'
        )
      }
      if (
        data.upstream_model_update_check_enabled === true &&
        !hasValidAdvancedCustomModelListRoute(advancedCustomConfig)
      ) {
        addRequiredIssue(
          ctx,
          'upstream_model_update_check_enabled',
          'OpenAI Models route is required to enable upstream model checks'
        )
      }
    }

    if ([3, 18, 21, 39, 41, 49].includes(data.type) && !data.other?.trim()) {
      addRequiredIssue(
        ctx,
        'other',
        'This channel type requires additional configuration'
      )
    }

    if (data.type === 57) {
      if (data.multi_key_mode && data.multi_key_mode !== 'single') {
        addRequiredIssue(
          ctx,
          'multi_key_mode',
          'Codex channels do not support batch creation'
        )
      }
      if (data.key?.trim() && !isCodexCredential(data.key)) {
        addRequiredIssue(
          ctx,
          'key',
          'Codex credential must be a JSON object with access_token and account_id'
        )
      }
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'json' &&
      data.key?.trim() &&
      !isVertexJsonKey(data.key)
    ) {
      addRequiredIssue(
        ctx,
        'key',
        'Vertex AI service account key must be valid JSON'
      )
    }

    if (
      data.type === 41 &&
      data.vertex_key_type === 'api_key' &&
      data.multi_key_mode &&
      data.multi_key_mode !== 'single'
    ) {
      addRequiredIssue(
        ctx,
        'multi_key_mode',
        'Vertex AI API Key mode does not support batch creation'
      )
    }

    const protocol = normalizeHttpProtocol(data.http_protocol)
    const shards = data.http2_connection_shards ?? 1
    if (shards < 1 || shards > MAX_HTTP2_CONNECTION_SHARDS) {
      addRequiredIssue(
        ctx,
        'http2_connection_shards',
        ERROR_MESSAGES.INVALID_HTTP2_CONNECTION_SHARDS
      )
    }
    if (protocol === HTTP_PROTOCOL_HTTP1 && shards > 1) {
      addRequiredIssue(
        ctx,
        'http2_connection_shards',
        ERROR_MESSAGES.INVALID_HTTP1_WITH_SHARDS
      )
    }
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  task_plugin_key: '',
  task_extend_plugin_keys: [],
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  http_protocol: HTTP_PROTOCOL_AUTO,
  http2_connection_shards: 1,
  tls_insecure_skip_verify: false,
  pass_through_body_enabled: false,
  responses_websocket_enabled: false,
  system_prompt: '',
  system_prompt_override: false,
  empty_response_no_billing_mode: 'inherit',
  sensitive_check_mode: 'inherit',
  anonymization_mode: 'inherit',
  sensitive_mode: 'inherit',
  sensitive_added_words: '',
  sensitive_removed_words: '',
  sensitive_override_words: '',
  // Type-specific settings
  is_enterprise_account: false,
  vertex_key_type: 'json',
  aws_key_type: 'ak_sk',
  azure_responses_version: '',
  // Field passthrough controls
  body_field_pass_through_rules: [],
  body_field_pass_through_text: '[]',
  header_field_pass_through_rules: [],
  header_field_pass_through_text: '[]',
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  force_cache_enabled: false,
  force_cache_ttl: '5m',
  force_cache_1h_enabled: false,
  cache_1h_bill_as_5m_enabled: false,
  cache_read_min_ratio_enabled: false,
  cache_read_min_ratio_percent: 95,
  thinking_signature_rectifier_enabled: false,
  claude_code_simulation_enabled: false,
  claude_code_simulation_headers: CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS_JSON,
  claude_code_simulation_metadata: CLAUDE_CODE_SIMULATION_DEFAULT_METADATA_JSON,
  claude_code_simulation_system: CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM_JSON,
  responses_encrypted_content_rectifier_enabled: false,
  unify_request_response_model_name_enabled: false,
  ollama_openai_chat: false,
  disable_task_polling_sleep: false,
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  advanced_custom: '',
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    task_plugin_key: '',
    task_extend_plugin_keys: [] as string[],
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    http_protocol: HTTP_PROTOCOL_AUTO as 'auto' | 'http1',
    http2_connection_shards: 1,
    tls_insecure_skip_verify: false,
    pass_through_body_enabled: false,
    responses_websocket_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
    unify_request_response_model_name_enabled: false,
    empty_response_no_billing_mode:
      'inherit' as ChannelFormValues['empty_response_no_billing_mode'],
    sensitive_check_mode:
      'inherit' as ChannelFormValues['sensitive_check_mode'],
    anonymization_mode: 'inherit' as ChannelFormValues['anonymization_mode'],
    sensitive_mode: 'inherit' as ChannelFormValues['sensitive_mode'],
    sensitive_added_words: '',
    sensitive_removed_words: '',
    sensitive_override_words: '',
  }

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      let emptyResponseNoBillingMode: ChannelFormValues['empty_response_no_billing_mode'] =
        'inherit'
      if (parsed.empty_response_no_billing_enabled === true) {
        emptyResponseNoBillingMode = 'enabled'
      } else if (parsed.empty_response_no_billing_enabled === false) {
        emptyResponseNoBillingMode = 'disabled'
      }
      let sensitiveCheckMode: ChannelFormValues['sensitive_check_mode'] =
        'inherit'
      if (parsed.sensitive_check_enabled === true) {
        sensitiveCheckMode = 'enabled'
      } else if (parsed.sensitive_check_enabled === false) {
        sensitiveCheckMode = 'disabled'
      }
      let anonymizationMode: ChannelFormValues['anonymization_mode'] = 'inherit'
      if (parsed.anonymization_enabled === true) {
        anonymizationMode = 'enabled'
      } else if (parsed.anonymization_enabled === false) {
        anonymizationMode = 'disabled'
      }
      const sensitiveMode: ChannelFormValues['sensitive_mode'] =
        parsed.sensitive_mode === 'modify' ||
        parsed.sensitive_mode === 'override'
          ? parsed.sensitive_mode
          : 'inherit'
      const protocol = normalizeHttpProtocol(parsed.http_protocol)
      const shards = normalizeHttp2ConnectionShards(
        parsed.http2_connection_shards
      )
      extraSettings = {
        task_plugin_key: parsed.task_plugin_key || '',
        task_extend_plugin_keys: readTaskExtendPluginKeys(channel.type, parsed),
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        http_protocol: protocol,
        http2_connection_shards: protocol === HTTP_PROTOCOL_HTTP1 ? 1 : shards,
        tls_insecure_skip_verify: parsed.tls_insecure_skip_verify === true,
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        responses_websocket_enabled:
          parsed.responses_websocket_enabled === true,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
        unify_request_response_model_name_enabled:
          parsed.unify_request_response_model_name_enabled === true,
        empty_response_no_billing_mode: emptyResponseNoBillingMode,
        sensitive_check_mode: sensitiveCheckMode,
        anonymization_mode: anonymizationMode,
        sensitive_mode: sensitiveMode,
        sensitive_added_words: parseSensitiveWordList(
          parsed.sensitive_added_words
        ),
        sensitive_removed_words: parseSensitiveWordList(
          parsed.sensitive_removed_words
        ),
        sensitive_override_words: parseSensitiveWordList(
          parsed.sensitive_override_words
        ),
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let forceCacheEnabled = false
  let forceCacheTtl: '5m' | '1h' = '5m'
  let forceCache1hEnabled = false
  let cache1hBillAs5mEnabled = false
  let cacheReadMinRatioEnabled = false
  let cacheReadMinRatioPercent = 95
  let thinkingSignatureRectifierEnabled = false
  let claudeCodeSimulationEnabled = false
  let claudeCodeSimulationHeaders = CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS_JSON
  let claudeCodeSimulationMetadata =
    CLAUDE_CODE_SIMULATION_DEFAULT_METADATA_JSON
  let claudeCodeSimulationSystem = CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM_JSON
  let responsesEncryptedContentRectifierEnabled = false
  let bodyFieldPassThroughRules: ChannelFormValues['body_field_pass_through_rules'] =
    []
  let headerFieldPassThroughRules: ChannelFormValues['header_field_pass_through_rules'] =
    []
  let ollamaOpenAIChat = false
  let disableTaskPollingSleep = false
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let advancedCustom = ''

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      forceCacheEnabled = parsed.force_cache_enabled === true
      forceCacheTtl = parsed.force_cache_ttl === '1h' ? '1h' : '5m'
      forceCache1hEnabled = parsed.force_cache_1h_enabled === true
      cache1hBillAs5mEnabled = parsed.cache_1h_bill_as_5m_enabled === true
      cacheReadMinRatioEnabled = parsed.cache_read_min_ratio_enabled === true
      {
        const ratio = Number(parsed.cache_read_min_ratio)
        cacheReadMinRatioPercent =
          Number.isFinite(ratio) && ratio > 0
            ? Math.round(ratio * 10000) / 100
            : 95
      }
      thinkingSignatureRectifierEnabled =
        parsed.thinking_signature_rectifier_enabled === true
      claudeCodeSimulationEnabled =
        parsed.claude_code_simulation_enabled === true
      if (isJsonObjectValue(parsed.claude_code_simulation)) {
        claudeCodeSimulationHeaders = stringifyJsonValue(
          parsed.claude_code_simulation.headers,
          CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS_JSON
        )
        claudeCodeSimulationMetadata = stringifyJsonValue(
          parsed.claude_code_simulation.metadata,
          CLAUDE_CODE_SIMULATION_DEFAULT_METADATA_JSON
        )
        claudeCodeSimulationSystem = stringifyJsonValue(
          parsed.claude_code_simulation.system,
          CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM_JSON
        )
      }
      responsesEncryptedContentRectifierEnabled =
        parsed.responses_encrypted_content_rectifier_enabled === true
      bodyFieldPassThroughRules = parseFieldPassThroughRules(
        parsed.body_field_pass_through
      )
      headerFieldPassThroughRules = parseFieldPassThroughRules(
        parsed.header_field_pass_through
      )
      ollamaOpenAIChat = parsed.ollama_openai_chat === true
      disableTaskPollingSleep = parsed.disable_task_polling_sleep === true
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      if (parsed.advanced_custom) {
        advancedCustom = stringifyAdvancedCustomConfig(parsed.advanced_custom)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: channel.model_mapping || '',
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: channel.status_code_mapping || '',
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    body_field_pass_through_rules: bodyFieldPassThroughRules,
    body_field_pass_through_text: formatFieldPassThroughRules(
      bodyFieldPassThroughRules
    ),
    header_field_pass_through_rules: headerFieldPassThroughRules,
    header_field_pass_through_text: formatFieldPassThroughRules(
      headerFieldPassThroughRules
    ),
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    force_cache_enabled: forceCacheEnabled,
    force_cache_ttl: forceCacheTtl,
    force_cache_1h_enabled: forceCache1hEnabled,
    cache_1h_bill_as_5m_enabled: cache1hBillAs5mEnabled,
    cache_read_min_ratio_enabled: cacheReadMinRatioEnabled,
    cache_read_min_ratio_percent: cacheReadMinRatioPercent,
    thinking_signature_rectifier_enabled: thinkingSignatureRectifierEnabled,
    claude_code_simulation_enabled: claudeCodeSimulationEnabled,
    claude_code_simulation_headers: claudeCodeSimulationHeaders,
    claude_code_simulation_metadata: claudeCodeSimulationMetadata,
    claude_code_simulation_system: claudeCodeSimulationSystem,
    responses_encrypted_content_rectifier_enabled:
      responsesEncryptedContentRectifierEnabled,
    ollama_openai_chat: ollamaOpenAIChat,
    disable_task_polling_sleep: disableTaskPollingSleep,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    advanced_custom: advancedCustom,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
export function buildSettingJSON(formData: ChannelFormValues): string {
  const settingObj: ChannelSettings = {
    task_plugin_key:
      formData.type === CHANNEL_TYPE_TASK_PLUGIN
        ? formData.task_plugin_key?.trim() || ''
        : undefined,
    task_extend_plugin_keys:
      formData.type === CHANNEL_TYPE_NEW_API &&
      formData.task_extend_plugin_keys?.length
        ? formData.task_extend_plugin_keys
        : undefined,
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy?.trim() || '',
    pass_through_body_enabled:
      formData.type !== CHANNEL_TYPE_ADVANCED_CUSTOM &&
      formData.pass_through_body_enabled === true,
    responses_websocket_enabled:
      supportsResponsesWebSocket(formData.type) &&
      formData.responses_websocket_enabled === true,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
    unify_request_response_model_name_enabled:
      formData.unify_request_response_model_name_enabled === true,
  }

  if (formData.empty_response_no_billing_mode === 'enabled') {
    settingObj.empty_response_no_billing_enabled = true
  } else if (formData.empty_response_no_billing_mode === 'disabled') {
    settingObj.empty_response_no_billing_enabled = false
  }

  if (formData.sensitive_check_mode === 'enabled') {
    settingObj.sensitive_check_enabled = true
  } else if (formData.sensitive_check_mode === 'disabled') {
    settingObj.sensitive_check_enabled = false
  }

  if (formData.anonymization_mode === 'enabled') {
    settingObj.anonymization_enabled = true
  } else if (formData.anonymization_mode === 'disabled') {
    settingObj.anonymization_enabled = false
  }

  if (formData.sensitive_mode === 'modify') {
    settingObj.sensitive_mode = 'modify'
    const addedWords = splitSensitiveWordLines(formData.sensitive_added_words)
    const removedWords = splitSensitiveWordLines(
      formData.sensitive_removed_words
    )
    if (addedWords.length > 0) {
      settingObj.sensitive_added_words = addedWords
    }
    if (removedWords.length > 0) {
      settingObj.sensitive_removed_words = removedWords
    }
  } else if (formData.sensitive_mode === 'override') {
    settingObj.sensitive_mode = 'override'
    const overrideWords = splitSensitiveWordLines(
      formData.sensitive_override_words
    )
    if (overrideWords.length > 0) {
      settingObj.sensitive_override_words = overrideWords
    }
  }

  const protocol = normalizeHttpProtocol(formData.http_protocol)
  const shards =
    protocol === HTTP_PROTOCOL_HTTP1
      ? 1
      : normalizeHttp2ConnectionShards(formData.http2_connection_shards)

  // Omit defaults so unchanged channels keep equivalent JSON.
  if (protocol === HTTP_PROTOCOL_HTTP1) {
    settingObj.http_protocol = HTTP_PROTOCOL_HTTP1
  } else if (shards > 1) {
    settingObj.http2_connection_shards = shards
  }
  if (formData.tls_insecure_skip_verify === true) {
    settingObj.tls_insecure_skip_verify = true
  }

  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
  } else if ('vertex_key_type' in settingsObj) {
    delete settingsObj.vertex_key_type
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33)
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
  } else if ('aws_key_type' in settingsObj) {
    delete settingsObj.aws_key_type
  }

  settingsObj.body_field_pass_through = sanitizeFieldPassThroughRules(
    formData.body_field_pass_through_rules
  )
  settingsObj.header_field_pass_through = sanitizeFieldPassThroughRules(
    formData.header_field_pass_through_rules
  )

  // Field passthrough controls:
  // - OpenAI, Anthropic, Codex, and New API: allow_service_tier
  // - OpenAI request fields: OpenAI, Codex, and New API
  // - Claude request fields: Anthropic and New API
  if (FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
  } else {
    if ('disable_store' in settingsObj) {
      delete settingsObj.disable_store
    }
    if ('allow_safety_identifier' in settingsObj) {
      delete settingsObj.allow_safety_identifier
    }
    if ('allow_include_obfuscation' in settingsObj) {
      delete settingsObj.allow_include_obfuscation
    }
  }

  if (
    OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type) ||
    CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)
  ) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else if ('allow_inference_geo' in settingsObj) {
    delete settingsObj.allow_inference_geo
  }

  if (CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.allow_speed = formData.allow_speed === true
  } else if ('allow_speed' in settingsObj) {
    delete settingsObj.allow_speed
  }

  // Only the Anthropic adaptor supports forcing the Claude beta query.
  if (formData.type === 14) {
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else {
    if ('claude_beta_query' in settingsObj) {
      delete settingsObj.claude_beta_query
    }
  }

  // Claude-capable upstreams (Anthropic 14, AWS Bedrock 33, Vertex 41): cache & rectifier controls.
  const CLAUDE_CAPABLE_CHANNEL_TYPES = [14, 33, 41]
  if (CLAUDE_CAPABLE_CHANNEL_TYPES.includes(formData.type)) {
    settingsObj.force_cache_enabled = formData.force_cache_enabled === true
    settingsObj.force_cache_ttl =
      formData.force_cache_ttl === '1h' ? '1h' : '5m'
    settingsObj.force_cache_1h_enabled =
      formData.force_cache_1h_enabled === true
    settingsObj.cache_1h_bill_as_5m_enabled =
      formData.cache_1h_bill_as_5m_enabled === true
    settingsObj.cache_read_min_ratio_enabled =
      formData.cache_read_min_ratio_enabled === true
    // UI edits a 0-100 percent; backend stores a 0-1 fraction.
    const percent = Number(formData.cache_read_min_ratio_percent)
    const clamped = Number.isFinite(percent)
      ? Math.min(100, Math.max(0, percent))
      : 0
    settingsObj.cache_read_min_ratio = clamped / 100
    settingsObj.thinking_signature_rectifier_enabled =
      formData.thinking_signature_rectifier_enabled === true
    settingsObj.claude_code_simulation_enabled =
      formData.claude_code_simulation_enabled === true
    const claudeCodeSimulationHeaders = parseJsonOrFallback(
      formData.claude_code_simulation_headers,
      CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS
    )
    const claudeCodeSimulationMetadata = parseJsonOrFallback(
      formData.claude_code_simulation_metadata,
      CLAUDE_CODE_SIMULATION_DEFAULT_METADATA
    )
    const claudeCodeSimulationSystem = parseJsonOrFallback(
      formData.claude_code_simulation_system,
      CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM
    )
    const claudeCodeSimulation: Record<string, unknown> = {
      metadata: claudeCodeSimulationMetadata,
      system: claudeCodeSimulationSystem,
    }
    if (formData.type === 14) {
      claudeCodeSimulation.headers = claudeCodeSimulationHeaders
    }
    const hasCustomClaudeCodeSimulation =
      (formData.type === 14 &&
        JSON.stringify(claudeCodeSimulationHeaders) !==
          JSON.stringify(CLAUDE_CODE_SIMULATION_DEFAULT_HEADERS)) ||
      JSON.stringify(claudeCodeSimulationMetadata) !==
        JSON.stringify(CLAUDE_CODE_SIMULATION_DEFAULT_METADATA) ||
      JSON.stringify(claudeCodeSimulationSystem) !==
        JSON.stringify(CLAUDE_CODE_SIMULATION_DEFAULT_SYSTEM)
    if (
      formData.claude_code_simulation_enabled === true ||
      hasCustomClaudeCodeSimulation
    ) {
      settingsObj.claude_code_simulation = claudeCodeSimulation
    } else {
      delete settingsObj.claude_code_simulation
    }
  } else {
    delete settingsObj.force_cache_enabled
    delete settingsObj.force_cache_ttl
    delete settingsObj.force_cache_1h_enabled
    delete settingsObj.cache_1h_bill_as_5m_enabled
    delete settingsObj.cache_read_min_ratio_enabled
    delete settingsObj.cache_read_min_ratio
    delete settingsObj.thinking_signature_rectifier_enabled
    delete settingsObj.claude_code_simulation_enabled
    delete settingsObj.claude_code_simulation
  }

  // Responses encrypted-content rectifier applies to OpenAI (1) / Azure (3) only.
  if (formData.type === 1 || formData.type === 3) {
    settingsObj.responses_encrypted_content_rectifier_enabled =
      formData.responses_encrypted_content_rectifier_enabled === true
  } else if ('responses_encrypted_content_rectifier_enabled' in settingsObj) {
    delete settingsObj.responses_encrypted_content_rectifier_enabled
  }

  // Only the Ollama adaptor can switch chat completions to the OpenAI-compatible endpoint.
  if (formData.type === CHANNEL_TYPE_OLLAMA) {
    settingsObj.ollama_openai_chat = formData.ollama_openai_chat === true
  } else if ('ollama_openai_chat' in settingsObj) {
    delete settingsObj.ollama_openai_chat
  }

  settingsObj.disable_task_polling_sleep =
    formData.disable_task_polling_sleep === true

  // Upstream model update settings (for model-fetchable channel types)
  if (MODEL_FETCHABLE_TYPES.has(formData.type)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = [
      ...new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      ),
    ]
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  }

  if (formData.type === CHANNEL_TYPE_ADVANCED_CUSTOM) {
    const advancedCustomConfig = parseAdvancedCustomConfig(
      formData.advanced_custom
    )
    if (advancedCustomConfig) {
      settingsObj.advanced_custom = advancedCustomConfig
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  return JSON.stringify(settingsObj)
}

function normalizeBaseUrl(value: string | undefined): string {
  return String(value || '')
    .trim()
    .replace(/\/+$/, '')
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: normalizeBaseUrl(formData.base_url) || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = normalizeBaseUrl(formData.base_url) || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}
