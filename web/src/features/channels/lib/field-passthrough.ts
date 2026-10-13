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

import type { FieldPassThroughRule } from '../types'

const fieldPassThroughRuleSchema = z.object({
  field: z.string(),
  enabled: z.boolean(),
  comment: z.string().optional(),
})

/** Validates normalized field passthrough rules stored in the channel form. */
export const fieldPassThroughRulesSchema = z.array(fieldPassThroughRuleSchema)

/**
 * Normalizes persisted passthrough rules while tolerating malformed entries.
 *
 * @param value - Raw value read from channel settings.
 * @returns Normalized rules, or an empty array when the top-level value is not an array.
 */
export function parseFieldPassThroughRules(
  value: unknown
): FieldPassThroughRule[] {
  if (!Array.isArray(value)) return []

  return value
    .filter(
      (item): item is Record<string, unknown> =>
        typeof item === 'object' && item !== null && !Array.isArray(item)
    )
    .map((item) => ({
      field: typeof item.field === 'string' ? item.field : '',
      enabled: item.enabled === true,
      comment: typeof item.comment === 'string' ? item.comment : '',
    }))
}

/**
 * Parses JSON text used by the passthrough rule editor.
 *
 * @param value - JSON array text; blank text represents an empty rule list.
 * @returns Normalized rules, or null when JSON is invalid or not an array.
 */
export function parseFieldPassThroughRulesText(
  value: string | undefined
): FieldPassThroughRule[] | null {
  if (!value?.trim()) return []

  try {
    const parsed: unknown = JSON.parse(value)
    return Array.isArray(parsed) ? parseFieldPassThroughRules(parsed) : null
  } catch {
    return null
  }
}

/**
 * Checks whether editor text contains a JSON rule array.
 *
 * @param value - JSON text to validate.
 * @returns True when the value is blank or parses to an array.
 */
export function isFieldPassThroughRulesText(
  value: string | undefined
): boolean {
  return parseFieldPassThroughRulesText(value) !== null
}

/**
 * Formats passthrough rules for the JSON editor.
 *
 * @param rules - Rules to serialize.
 * @returns Pretty-printed JSON array text.
 */
export function formatFieldPassThroughRules(
  rules: FieldPassThroughRule[]
): string {
  return JSON.stringify(rules, null, 2)
}

/**
 * Removes blank rules and normalizes fields before channel submission.
 *
 * @param rules - Rules from the channel form.
 * @returns Rules safe to persist in channel settings.
 */
export function sanitizeFieldPassThroughRules(
  rules: FieldPassThroughRule[]
): FieldPassThroughRule[] {
  return rules
    .filter((rule) => rule.field.trim().length > 0)
    .map((rule) => ({
      field: rule.field.trim(),
      enabled: rule.enabled === true,
      comment: rule.comment || '',
    }))
}
