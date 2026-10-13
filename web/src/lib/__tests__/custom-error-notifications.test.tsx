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
import { QueryClientProvider, type QueryClient } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError } from 'axios'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { LogExportPage } from '@/features/log-export'
import { RatioScheduleManager } from '@/features/system-settings/models/ratio-schedule-manager'
import { BatchUserActionDialog } from '@/features/users/components/batch-user-action-dialog'
import { api } from '@/lib/api'
import { createAppQueryClient } from '@/lib/query-client'
import { useAuthStore } from '@/stores/auth-store'

const clients: QueryClient[] = []
function client() {
  const result = createAppQueryClient()
  result.setDefaultOptions({ queries: { retry: false } })
  clients.push(result)
  return result
}
afterEach(() => {
  for (const instance of clients.splice(0)) instance.clear()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('reports a rejected custom export list instead of caching a successful empty list', async () => {
  const notification = vi.spyOn(toast, 'error').mockReturnValue('error')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: false, message: 'Exports unavailable' },
  })
  const queries = client()
  render(
    <QueryClientProvider client={queries}>
      <LogExportPage />
    </QueryClientProvider>
  )
  await waitFor(() =>
    expect(queries.getQueryState(['log-exports', false, 1])?.status).toBe(
      'error'
    )
  )
  expect(notification.mock.calls.map(([message]) => message)).toEqual([
    'Exports unavailable',
  ])
})

it('does not treat a rejected schedule deletion as success or invalidate its list', async () => {
  const notification = vi.spyOn(toast, 'error').mockReturnValue('error')
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: [
        {
          id: 1,
          group_name: 'default',
          start_minute: 0,
          end_minute: 360,
          mode: 'override',
          target_ratio: 0.5,
          stack_with_user_ratio: false,
          enabled: true,
          created_at: 1,
        },
      ],
    },
  })
  vi.spyOn(api, 'delete').mockResolvedValue({
    data: { success: false, message: 'Schedule deletion denied' },
  })
  const queries = client()
  const invalidate = vi.spyOn(queries, 'invalidateQueries')
  render(
    <QueryClientProvider client={queries}>
      <RatioScheduleManager groupNames={[]} />
    </QueryClientProvider>
  )
  await userEvent.click(await screen.findByRole('button', { name: 'Delete' }))
  await waitFor(() => expect(notification).toHaveBeenCalledTimes(1))
  expect(notification).toHaveBeenCalledWith('Schedule deletion denied')
  expect(invalidate).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Delete' })).toBeVisible()
})

it('reports a failed quota batch request once through the production mutation cache', async () => {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  const notification = vi.spyOn(toast, 'error').mockReturnValue('error')
  vi.spyOn(api, 'post').mockRejectedValue(new AxiosError('Network unavailable'))
  render(
    <QueryClientProvider client={client()}>
      <BatchUserActionDialog
        open
        onOpenChange={() => undefined}
        onSuccess={() => undefined}
      />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.type(screen.getAllByPlaceholderText('No maximum')[0], '0')
  await user.click(screen.getByRole('button', { name: 'Preview Matches' }))
  await waitFor(() => expect(notification).toHaveBeenCalledTimes(1))
  expect(notification).toHaveBeenCalledWith('Network unavailable')
})
