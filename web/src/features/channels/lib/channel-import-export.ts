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
import {
  channelImportFileSchema,
  type AddChannelRequest,
  type ChannelExportItem,
} from '../types'

/**
 * Parse and validate the JSON text of a channel export file.
 *
 * @param text - Raw file contents.
 * @returns The normalized list of channel entries.
 * @throws {Error} When the JSON is malformed or fails schema validation.
 */
export function parseChannelImportFile(text: string): ChannelExportItem[] {
  let raw: unknown
  try {
    raw = JSON.parse(text)
  } catch {
    throw new Error('invalid-json')
  }

  const result = channelImportFileSchema.safeParse(raw)
  if (!result.success) {
    throw new Error('invalid-schema')
  }

  return Array.isArray(result.data) ? result.data : result.data.channels
}

/**
 * Build an AddChannel request payload from a single export entry.
 * Multi-key channels are re-created via the multi_to_single mode so their
 * newline-separated keys are restored, otherwise a plain single-channel add.
 *
 * @param item - One validated export entry.
 * @returns The request body for createChannel.
 */
export function buildImportChannelRequest(
  item: ChannelExportItem
): AddChannelRequest {
  const isMultiKey = item.channel_info?.is_multi_key === true
  const multiKeyMode = item.channel_info?.multi_key_mode ?? 'random'

  return {
    mode: isMultiKey ? 'multi_to_single' : 'single',
    multi_key_mode: isMultiKey ? multiKeyMode : undefined,
    channel: {
      type: item.type,
      name: item.name,
      key: item.key ?? '',
      base_url: item.base_url ?? null,
      other: item.other ?? '',
      models: item.models ?? '',
      group: item.group || 'default',
      model_mapping: item.model_mapping ?? null,
      status_code_mapping: item.status_code_mapping ?? null,
      priority: item.priority ?? null,
      weight: item.weight ?? null,
      auto_ban: item.auto_ban ?? 1,
      test_model: item.test_model ?? null,
      openai_organization: item.openai_organization ?? null,
      tag: item.tag ?? null,
      setting: item.setting ?? null,
      param_override: item.param_override ?? null,
      header_override: item.header_override ?? null,
      remark: item.remark ?? '',
      status: item.status ?? 1,
      settings: item.settings ?? '{}',
    },
  }
}
