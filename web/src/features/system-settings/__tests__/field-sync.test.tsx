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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { pricingOptions } from '@/features/model-pricing/pricing'
import { api } from '@/lib/api'

import { UpstreamFieldSync } from '../models/upstream-field-sync'

vi.mock('../api', () => ({
  getUpstreamChannels: async () => ({
    success: true,
    data: [{ id: 7, name: 'Source', base_url: 'https://example.test' }],
  }),
  fetchUpstreamRatios: async () => ({
    success: true,
    data: {
      differences: {
        m: { completion_ratio: { current: 2, upstreams: { Source: 4 } } },
      },
      test_results: [],
    },
  }),
}))
vi.mock('../models/channel-selector-dialog', () => ({
  ChannelSelectorDialog: (props: {
    open: boolean
    onConfirm: (ids: number[]) => void
  }) =>
    props.open ? (
      <button type='button' onClick={() => props.onConfirm([7])}>
        Load source
      </button>
    ) : null,
}))
vi.mock('../models/upstream-field-sync-table', () => ({
  UpstreamFieldSyncTable: (props: {
    differences: Record<string, unknown>
    onSelectValue: (...args: unknown[]) => void
  }) =>
    Object.keys(props.differences).length ? (
      <button
        type='button'
        onClick={() =>
          props.onSelectValue('m', 'completion_ratio', 4, 'Source')
        }
      >
        Select output field
      </button>
    ) : null,
}))

afterEach(() => vi.restoreAllMocks())

it('saves one selected field atomically with the original model version and preserves other prices', async () => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        entries: [
          {
            model_name: 'm',
            version: 'version-before-selection',
            configured: { ModelRatio: 1, CompletionRatio: 2, CacheRatio: 0.1 },
          },
        ],
        empty_version: 'empty',
        options: pricingOptions({
          ModelRatio: '{"m":1,"unselected":3}',
          CompletionRatio: '{"m":2}',
          CacheRatio: '{"m":0.1}',
        }),
      },
    },
  })
  const patch = vi
    .spyOn(api, 'patch')
    .mockResolvedValue({ data: { success: true } })
  const put = vi.spyOn(api, 'put')
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UpstreamFieldSync />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Select Sync Channels' }))
  await user.click(await screen.findByRole('button', { name: 'Load source' }))
  await user.click(
    await screen.findByRole('button', { name: 'Select output field' })
  )
  await user.click(screen.getByRole('button', { name: 'Apply Sync' }))
  await waitFor(() =>
    expect(patch).toHaveBeenCalledWith('/api/option/model_pricing', {
      changes: [
        {
          model_name: 'm',
          expected_version: 'version-before-selection',
          pricing: { ModelRatio: 1, CompletionRatio: 4, CacheRatio: 0.1 },
        },
      ],
    })
  )
  expect(put).not.toHaveBeenCalled()
})
