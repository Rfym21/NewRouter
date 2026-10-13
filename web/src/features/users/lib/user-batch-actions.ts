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

import { parseQuotaFromDollars } from '@/lib/format'

import type { BatchUserAction, BatchUserActionPayload } from '../types'

const optionalQuota = z
  .number()
  .finite('Quota values must be valid numbers')
  .min(0, 'Quota values cannot be negative')
  .refine(
    (value) => Number.isSafeInteger(parseQuotaFromDollars(value)),
    'Quota values must be valid numbers'
  )
  .optional()

export const batchUserActionFormSchema = z
  .object({
    used_quota_min: optionalQuota,
    used_quota_max: optionalQuota,
    remaining_quota_min: optionalQuota,
    remaining_quota_max: optionalQuota,
    total_quota_min: optionalQuota,
    total_quota_max: optionalQuota,
  })
  .superRefine((values, context) => {
    const pairs = [
      ['used_quota_min', 'used_quota_max'],
      ['remaining_quota_min', 'remaining_quota_max'],
      ['total_quota_min', 'total_quota_max'],
    ] as const

    const hasFilter = Object.values(values).some((value) => value !== undefined)
    if (!hasFilter) {
      context.addIssue({
        code: 'custom',
        message: 'At least one quota filter is required',
        path: ['used_quota_min'],
      })
    }

    for (const [minimumKey, maximumKey] of pairs) {
      const minimum = values[minimumKey]
      const maximum = values[maximumKey]
      if (minimum !== undefined && maximum !== undefined && minimum > maximum) {
        context.addIssue({
          code: 'custom',
          message: 'Minimum quota cannot exceed maximum quota',
          path: [maximumKey],
        })
      }
    }
  })

export type BatchUserActionFormValues = z.infer<
  typeof batchUserActionFormSchema
>

export const BATCH_USER_ACTION_DEFAULT_VALUES: BatchUserActionFormValues = {
  used_quota_min: undefined,
  used_quota_max: undefined,
  remaining_quota_min: undefined,
  remaining_quota_max: undefined,
  total_quota_min: undefined,
  total_quota_max: undefined,
}

/**
 * Convert quota-range form values from the active display unit to API units.
 *
 * @param values - Validated quota-range form values.
 * @param action - Batch action to preview or execute.
 * @param dryRun - Whether the backend should only count matching users.
 * @returns A request payload that preserves explicit zero boundaries.
 */
export function createBatchUserActionPayload(
  values: BatchUserActionFormValues,
  action: BatchUserAction,
  dryRun = false
): BatchUserActionPayload {
  const payload: BatchUserActionPayload = { action }

  for (const [key, value] of Object.entries(values) as Array<
    [keyof BatchUserActionFormValues, number | undefined]
  >) {
    if (value !== undefined) {
      const quota = parseQuotaFromDollars(value)
      if (!Number.isSafeInteger(quota) || quota < 0) {
        throw new RangeError('Quota values must be valid numbers')
      }
      payload[key] = quota
    }
  }

  if (dryRun) {
    payload.dry_run = true
  }

  return payload
}
