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
import * as z from 'zod'

import type { AnonymizationRule } from '../api'

export const entityLabels = {
  CUSTOM: 'Custom',
  PERSON: 'Person',
  ORGANIZATION: 'Organization',
  EMAIL_ADDRESS: 'Email address',
  EMAIL: 'Email address',
  PHONE_NUMBER: 'Phone number',
  CN_ID_CARD: 'Chinese ID card',
  ADDRESS: 'Address',
  LOCATION: 'Location',
  CREDENTIAL: 'Credential',
  TOKEN: 'Token',
  IDENTIFIER: 'Identifier',
  HASH: 'Hash',
  IP_ADDRESS: 'IP address',
  CIDR: 'Network range',
  HOSTNAME: 'Hostname',
  DOMAIN: 'Domain',
  URL: 'URL',
  USERNAME: 'Username',
  MAC_ADDRESS: 'MAC address',
  PATH: 'File path',
} as const

const ruleSchema = z
  .object({
    name: z.string(),
    mode: z.enum(['literal', 'regex']),
    pattern: z.string(),
    enabled: z.boolean().default(false),
    entity_type: z
      .string()
      .transform((value) => value.trim().toUpperCase() || 'CUSTOM')
      .default('CUSTOM'),
    capture_group: z.number().default(0),
  })
  .strict()

export function parseRules(value: string): AnonymizationRule[] | null {
  try {
    const parsed = z.array(ruleSchema).max(128).safeParse(JSON.parse(value))
    return parsed.success ? parsed.data : null
  } catch {
    return null
  }
}

export function validateAnonymizationRules(value: string): string | null {
  const rules = parseRules(value)
  if (!rules || new TextEncoder().encode(value).length > 128 * 1024) {
    return 'Invalid anonymization rules'
  }
  const names = new Set<string>()
  for (const rule of rules) {
    const name = rule.name.trim()
    if (
      !name ||
      [...name].length > 80 ||
      [...rule.name].some((character) => {
        const code = character.charCodeAt(0)
        return code < 32 || (code >= 127 && code <= 159)
      })
    ) {
      return 'Invalid anonymization rules'
    }
    if (names.has(name.toLowerCase())) {
      return 'Rule names must be unique'
    }
    names.add(name.toLowerCase())
    if (
      !rule.pattern.trim() ||
      new TextEncoder().encode(rule.pattern).length > 4096
    ) {
      return 'Patterns must not be empty or exceed the size limit'
    }
    if (
      !(rule.entity_type ?? 'CUSTOM') ||
      !Object.hasOwn(entityLabels, rule.entity_type ?? 'CUSTOM')
    ) {
      return 'Invalid anonymization rules'
    }
    if (
      !Number.isInteger(rule.capture_group) ||
      (rule.capture_group ?? 0) < 0
    ) {
      return 'Invalid anonymization rules'
    }
    if (rule.mode === 'literal' && rule.capture_group !== 0) {
      return 'Invalid anonymization rules'
    }
  }
  return null
}
