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
export type ClientRuleDraft = {
  key: string
  name: string
  keywords: string
}

let clientRuleKeySeed = 0

export function newClientRuleKey(): string {
  clientRuleKeySeed += 1
  return `client-rule-${clientRuleKeySeed}`
}

export function parseClientRules(raw: string): ClientRuleDraft[] {
  try {
    const parsed = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((rule) => rule && typeof rule === 'object')
      .map((rule) => ({
        key: newClientRuleKey(),
        name: typeof rule.name === 'string' ? rule.name : '',
        keywords: Array.isArray(rule.keywords)
          ? rule.keywords
              .filter((item: unknown) => typeof item === 'string')
              .join(', ')
          : '',
      }))
  } catch {
    return []
  }
}

export function serializeClientRules(rules: ClientRuleDraft[]): string {
  return JSON.stringify(
    rules
      .map((rule) => ({
        name: rule.name.trim(),
        keywords: rule.keywords
          .split(',')
          .map((keyword) => keyword.trim())
          .filter(Boolean),
      }))
      .filter((rule) => rule.name && rule.keywords.length)
  )
}

/** Returns a user-facing error for the draft, or null when it is valid. */
export function validateClientRules(rules: ClientRuleDraft[]): string | null {
  if (rules.length > 100) return 'At most 100 custom client rules are allowed'
  const names = new Set<string>()
  for (const rule of rules) {
    const name = rule.name.trim()
    if (!name) return 'Every custom client rule needs a name'
    if (name.length > 64) return `Client name is too long: ${name}`
    const key = name.toLowerCase()
    if (names.has(key)) return `Client name is used twice: ${name}`
    names.add(key)
    const keywords = rule.keywords
      .split(',')
      .map((keyword) => keyword.trim())
      .filter(Boolean)
    if (keywords.length === 0) {
      return `Add at least one User-Agent keyword for ${name}`
    }
    if (keywords.some((keyword) => keyword.length > 128)) {
      return `A User-Agent keyword is too long for ${name}`
    }
  }
  return null
}
