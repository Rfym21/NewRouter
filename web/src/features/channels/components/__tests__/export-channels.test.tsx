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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { downloadBlob } from '@/lib/download'
import { useAuthStore } from '@/stores/auth-store'

import { ChannelsPrimaryButtons } from '../channels-primary-buttons'

vi.mock('@/lib/download', () => ({ downloadBlob: vi.fn() }))
vi.mock('../channels-provider', () => ({
  useChannels: () => ({
    setOpen: vi.fn(),
    setCurrentRow: vi.fn(),
    enableTagMode: false,
    setEnableTagMode: vi.fn(),
    idSort: false,
    setIdSort: vi.fn(),
    batchMode: false,
    setBatchMode: vi.fn(),
    upstream: { detectAllUpdates: vi.fn(), applyAllUpdates: vi.fn() },
  }),
}))

afterEach(() => {
  vi.restoreAllMocks()
  vi.mocked(downloadBlob).mockClear()
  useAuthStore.getState().auth.reset()
})

function mount() {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role: 100 })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        scope: 'channel.key.export',
        methods: [{ method: '2fa', available: true }],
        oauth_providers: [],
        password_encryption_enabled: false,
      },
    },
  })
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <ChannelsPrimaryButtons />
    </QueryClientProvider>
  )
}

it('binds channel export to a dedicated proof and downloads only after verification', async () => {
  const post = vi.spyOn(api, 'post').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/verify'
          ? {
              proof_token: 'export-proof',
              scope: 'channel.key.export',
              method: '2fa',
              expires_at: Math.floor(Date.now() / 1000) + 60,
            }
          : {
              version: 1,
              channels: [{ name: 'preserved-channel', key: 'test-key' }],
            },
    },
  }))
  mount()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Export Channels' })
  )
  expect(post).not.toHaveBeenCalled()
  await user.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))
  await waitFor(() => expect(downloadBlob).toHaveBeenCalledTimes(1))
  expect(post).toHaveBeenNthCalledWith(
    1,
    '/api/verify',
    {
      scope: 'channel.key.export',
      context: { channel_ids: [] },
      method: '2fa',
      code: '123456',
    },
    expect.anything()
  )
  expect(post).toHaveBeenNthCalledWith(
    2,
    '/api/channel/export',
    { ids: [] },
    expect.objectContaining({
      headers: { 'X-Security-Proof': 'export-proof' },
      signal: expect.any(AbortSignal),
    })
  )
})

it('cancels a pending export before downloading when its view unmounts', async () => {
  let finish: ((value: unknown) => void) | undefined
  const pending = new Promise((resolve) => {
    finish = resolve
  })
  vi.spyOn(api, 'post').mockImplementation(async (url) => {
    if (url === '/api/verify') {
      return {
        data: {
          success: true,
          data: {
            proof_token: 'export-proof',
            scope: 'channel.key.export',
            method: '2fa',
            expires_at: Math.floor(Date.now() / 1000) + 60,
          },
        },
      }
    }
    return pending
  })
  const view = mount()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Open menu' }))
  await user.click(
    await screen.findByRole('menuitem', { name: 'Export Channels' })
  )
  await user.type(
    await screen.findByLabelText('Authenticator code or backup code'),
    '123456'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/channel/export',
      { ids: [] },
      expect.anything()
    )
  )
  view.unmount()
  await act(async () => {
    finish?.({ data: { success: true, data: { version: 1, channels: [] } } })
    await pending
  })
  expect(downloadBlob).not.toHaveBeenCalled()
})
