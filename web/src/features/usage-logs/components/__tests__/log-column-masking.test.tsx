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
import type { ColumnDef } from '@tanstack/react-table'
import { act, render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it } from 'vitest'

import { DataTableView, useDataTable } from '@/components/data-table'

import { usageLogSchema } from '../../data/schema'
import { getAnonymizationCount } from '../../lib/anonymization'
import { useLogColumnMasking } from '../../lib/column-masking'
import { AnonymizationBadge } from '../anonymization-badge'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { LogColumnOptions } from '../log-column-options'
import { UsageLogsMobileList } from '../usage-logs-mobile-card'
import { UsageLogsProvider } from '../usage-logs-provider'

const rows = [{ value: 'private-display-value', count: 4 }]
const columns: ColumnDef<(typeof rows)[number]>[] = [
  {
    accessorKey: 'value',
    header: 'Private value',
    size: 170,
    cell: ({ getValue }) => (
      <a href='/private-link' title={getValue<string>()}>
        {getValue<string>()}
      </a>
    ),
  },
  {
    accessorKey: 'count',
    header: 'Anonymization',
    cell: ({ getValue }) => <AnonymizationBadge count={getValue<number>()} />,
  },
  {
    id: 'created_at',
    header: 'Time',
    accessorFn: () => 'private-time',
    enableHiding: false,
  },
]

function DesktopFixture({
  storageKey = 'test-masking:user-1',
}: {
  storageKey?: string
}) {
  const masking = useLogColumnMasking(columns, storageKey)
  const { table } = useDataTable({
    columns: masking.columns,
    data: rows,
    enableRowSelection: false,
  })
  return (
    <>
      <LogColumnOptions table={table} masking={masking} />
      <button type='button' onClick={() => masking.onMaskChange('value', true)}>
        Mask value
      </button>
      <button
        type='button'
        onClick={() => masking.onMaskChange('value', false)}
      >
        Reveal value
      </button>
      <DataTableView table={table} />
    </>
  )
}

afterEach(() => localStorage.clear())

it('retains column heading and removes contents, links and tooltip values while masked', async () => {
  const user = userEvent.setup()
  const { container } = render(<DesktopFixture />)
  expect(
    screen.getByRole('link', { name: 'private-display-value' })
  ).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Mask value' }))
  expect(
    screen.getByRole('columnheader', { name: 'Private value' })
  ).toBeVisible()
  expect(screen.getByText('Anonymized: 4')).toBeVisible()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  expect(container.innerHTML).not.toContain('private-display-value')
  expect(container.innerHTML).not.toContain('/private-link')
  expect(screen.getByLabelText('Column contents hidden')).toBeVisible()
  expect(
    JSON.parse(localStorage.getItem('test-masking:user-1') ?? '[]')
  ).toEqual(['value'])
  await user.click(screen.getByRole('button', { name: 'Reveal value' }))
  expect(
    screen.getByRole('link', { name: 'private-display-value' })
  ).toBeVisible()
})

it('persists content masks across reloads and isolates account/view preferences', async () => {
  const user = userEvent.setup()
  const first = render(<DesktopFixture />)
  await user.click(screen.getByRole('button', { name: 'Mask value' }))
  first.unmount()
  const second = render(<DesktopFixture />)
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  second.rerender(<DesktopFixture storageKey='test-masking:user-2' />)
  expect(screen.getByRole('link')).toBeVisible()
  second.rerender(<DesktopFixture />)
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})

it('uses separate menu actions for content masking and whole-column visibility', async () => {
  const user = userEvent.setup()
  render(<DesktopFixture />)
  await user.click(screen.getByRole('button', { name: 'Columns' }))
  await user.click(
    screen.getByRole('menuitem', { name: 'Hide column contents' })
  )
  const maskItem = await screen.findByRole('menuitemcheckbox', {
    name: 'Private value',
  })
  act(() => maskItem.focus())
  await user.keyboard('{Enter}')
  expect(
    screen.getByRole('columnheader', { name: 'Private value' })
  ).toBeVisible()
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
  await user.keyboard('{Escape}{Escape}')
  await user.click(screen.getByRole('button', { name: 'Columns' }))
  await user.click(
    screen.getByRole('menuitem', { name: 'Show all column contents' })
  )
  expect(screen.getByRole('link')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Columns' }))
  await user.click(screen.getByRole('menuitem', { name: 'Toggle columns' }))
  const visibilityItem = await screen.findByRole('menuitemcheckbox', {
    name: 'Private value',
  })
  act(() => visibilityItem.focus())
  await user.keyboard('{Enter}')
  expect(
    screen.queryByRole('columnheader', { name: 'Private value' })
  ).not.toBeInTheDocument()
})

it('can mask contents of required columns while keeping them out of the visibility menu', async () => {
  const user = userEvent.setup()
  render(<DesktopFixture />)
  await user.click(screen.getByRole('button', { name: 'Columns' }))
  await user.click(
    screen.getByRole('menuitem', { name: 'Hide column contents' })
  )
  const timeItem = await screen.findByRole('menuitemcheckbox', { name: 'Time' })
  act(() => timeItem.focus())
  await user.keyboard('{Enter}')
  expect(screen.getByRole('columnheader', { name: 'Time' })).toBeVisible()
  expect(screen.queryByText('private-time')).not.toBeInTheDocument()
  await user.keyboard('{Escape}{Escape}')
  await user.click(screen.getByRole('button', { name: 'Columns' }))
  await user.click(screen.getByRole('menuitem', { name: 'Toggle columns' }))
  expect(
    screen.queryByRole('menuitemcheckbox', { name: 'Time' })
  ).not.toBeInTheDocument()
})

it.each(['{"value":true}', 'invalid JSON', '[false,null,1]'])(
  'ignores invalid stored masks: %s',
  (stored) => {
    localStorage.setItem('test-masking:user-1', stored)
    render(<DesktopFixture />)
    expect(screen.getByRole('link')).toBeVisible()
  }
)

let maskMobileChannel: () => void
function MobileFixture() {
  const originalColumns = useCommonLogsColumns(true, false)
  const masking = useLogColumnMasking(originalColumns, 'test-mobile-masking')
  maskMobileChannel = () => masking.onMaskChange('channel', true)
  const { table } = useDataTable({
    columns: masking.columns,
    data: [
      usageLogSchema.parse({
        id: 1,
        user_id: 2,
        type: 2,
        content: '',
        created_at: 1788840000,
        model_name: 'private-model',
        username: 'private-user',
        channel: 312,
        channel_name: 'private-channel',
        token_name: 'private-token',
        group: 'private-group',
        quota: 1000,
        prompt_tokens: 1271,
        completion_tokens: 843,
        use_time: 1.3,
        other: JSON.stringify({ anonymization_count: 3 }),
      }),
    ],
    enableRowSelection: false,
  })
  return (
    <>
      <button
        type='button'
        onClick={() => masking.onMaskChange('model_name', true)}
      >
        Mask model
      </button>
      <UsageLogsMobileList
        table={table}
        logCategory='common'
        maskedColumns={masking.maskedColumns}
      />
    </>
  )
}

function renderMobile() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <UsageLogsProvider>
        <MobileFixture />
      </UsageLogsProvider>
    </QueryClientProvider>
  )
}

it('masks mobile summary values and identity metadata without hiding the anonymization count', () => {
  localStorage.setItem(
    'test-mobile-masking',
    JSON.stringify([
      'model_name',
      'user',
      'channel',
      'token_name',
      'prompt_tokens',
    ])
  )
  const { container } = renderMobile()
  expect(screen.getByText('Anonymized: 3')).toBeVisible()
  for (const secret of [
    'private-model',
    'private-user',
    'private-channel',
    'private-token',
    'private-group',
    '1271',
    '843',
  ]) {
    expect(container.innerHTML).not.toContain(secret)
  }
  expect(
    screen.queryByRole('button', { name: /^Model:/ })
  ).not.toBeInTheDocument()
})

it('closes a mobile inspection when that column becomes masked', async () => {
  const user = userEvent.setup()
  const { container } = renderMobile()
  await user.click(
    screen.getByRole('button', { name: 'Channel: private-channel #312' })
  )
  const dialog = await screen.findByRole('dialog', { name: 'Channel' })
  expect(within(dialog).getByText('private-channel #312')).toBeVisible()
  act(() => maskMobileChannel())
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(container.innerHTML).not.toContain('private-channel')
})

it.each([undefined, 0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, '3'])(
  'does not mark historical, unprocessed or invalid anonymization counts: %s',
  (count) =>
    expect(getAnonymizationCount({ anonymization_count: count } as never)).toBe(
      0
    )
)
