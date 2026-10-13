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

/** Ratio adjustment mode within a daily window. */
export type RatioScheduleMode = 'override' | 'multiply'

export type RatioSchedule = {
  id: number
  group_name: string
  start_minute: number
  end_minute: number
  mode: RatioScheduleMode
  target_ratio: number
  stack_with_user_ratio: boolean
  enabled: boolean
  created_at: number
}

export type AddRatioScheduleParams = {
  group_name: string
  start_minute: number
  end_minute: number
  mode: RatioScheduleMode
  target_ratio: number
  stack_with_user_ratio: boolean
  enabled: boolean
}

/**
 * List all group-ratio schedule tasks.
 */
export async function getRatioSchedules(): Promise<{
  success: boolean
  message?: string
  data?: RatioSchedule[]
}> {
  const res = await api.get('/api/option/ratio_schedules')
  return res.data
}

/**
 * Create a new group-ratio schedule task.
 */
export async function addRatioSchedule(
  params: AddRatioScheduleParams
): Promise<{ success: boolean; message?: string; data?: RatioSchedule }> {
  const res = await api.post('/api/option/ratio_schedules', params)
  return res.data
}

/**
 * Delete a group-ratio schedule task by id.
 */
export async function deleteRatioSchedule(
  id: number
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete(`/api/option/ratio_schedules/${id}`)
  return res.data
}
