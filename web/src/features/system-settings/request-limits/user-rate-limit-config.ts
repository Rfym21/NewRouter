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
import { isObjectRecord } from '../utils/json-validators'

const MAX_RATE_LIMIT_COUNT = 2_147_483_647
const MAX_USER_ID = '9223372036854775807'

export const USER_DEFAULT_RATE_LIMIT_GROUP = '*'

export type UserRateLimitEntry = {
  userId: string
  groupName: string
  maxRequests: number
  maxSuccess: number
}

function isValidRateLimitPair(value: unknown): value is [number, number] {
  return (
    Array.isArray(value) &&
    value.length === 2 &&
    Number.isInteger(value[0]) &&
    Number.isInteger(value[1]) &&
    value[0] >= 0 &&
    value[1] >= 1 &&
    value[0] <= MAX_RATE_LIMIT_COUNT &&
    value[1] <= MAX_RATE_LIMIT_COUNT
  )
}

/**
 * Checks whether a user ID is canonical and fits the backend's 64-bit integer range.
 * @param value User ID text from JSON or form input.
 * @returns Whether the value can be resolved by the backend.
 */
export function isValidUserRateLimitID(value: string): boolean {
  if (!/^[1-9]\d*$/.test(value)) return false
  if (value.length !== MAX_USER_ID.length) {
    return value.length < MAX_USER_ID.length
  }
  return value <= MAX_USER_ID
}

/**
 * Parses and validates persisted per-user rate-limit JSON.
 * @param value JSON in the user ID to group rules shape.
 * @returns Flattened rules, or `null` when the value is invalid.
 */
export function parseUserRateLimitEntries(
  value: string | undefined
): UserRateLimitEntry[] | null {
  if (!value || value.trim() === '') return []

  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    return null
  }
  if (!isObjectRecord(parsed)) return null

  const entries: UserRateLimitEntry[] = []
  for (const [userId, groupLimits] of Object.entries(parsed)) {
    if (!isValidUserRateLimitID(userId) || !isObjectRecord(groupLimits)) {
      return null
    }

    const rules = Object.entries(groupLimits)
    if (rules.length === 0) return null

    for (const [groupName, limits] of rules) {
      if (!groupName.trim() || !isValidRateLimitPair(limits)) return null
      entries.push({
        userId,
        groupName,
        maxRequests: limits[0],
        maxSuccess: limits[1],
      })
    }
  }

  return entries
}

/**
 * Serializes flattened per-user rate-limit rules for option storage.
 * @param entries Validated user rate-limit rules.
 * @returns Pretty-printed JSON, or an empty string when no rules remain.
 */
export function serializeUserRateLimitEntries(
  entries: UserRateLimitEntry[]
): string {
  if (entries.length === 0) return ''

  const config = Object.create(null) as Record<
    string,
    Record<string, [number, number]>
  >
  for (const entry of entries) {
    const groupLimits =
      config[entry.userId] ??
      (Object.create(null) as Record<string, [number, number]>)
    config[entry.userId] = groupLimits
    groupLimits[entry.groupName] = [entry.maxRequests, entry.maxSuccess]
  }
  return JSON.stringify(config, null, 2)
}
