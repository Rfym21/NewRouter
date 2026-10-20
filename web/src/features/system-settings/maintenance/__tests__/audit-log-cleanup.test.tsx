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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { AuthOperationError } from '@/lib/secure-verification'
import { Route as OperationsRoute } from '@/routes/_authenticated/system-settings/operations/$section'
import { useAuthStore } from '@/stores/auth-store'

import { OPERATIONS_SECTION_IDS } from '../../operations/section-registry'
import { AuditLogCleanup } from '../audit-log-cleanup'

function renderCleanup(role: number = ROLE.SUPER_ADMIN) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const rendered = render(
    <QueryClientProvider client={client}>
      <AuditLogCleanup />
    </QueryClientProvider>
  )
  return { ...rendered, client }
}

async function beginVerification() {
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Clear audit logs' }))
  const confirmation = await screen.findByRole('alertdialog')
  expect(confirmation).toHaveTextContent('This action cannot be undone.')
  await user.click(
    within(confirmation).getByRole('button', { name: 'Continue' })
  )
  await screen.findByRole('dialog', { name: 'Verify to clear audit logs' })
  await screen.findByLabelText('Password', { selector: 'input' })
  return user
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        scope: 'audit_log.clear',
        methods: [{ method: 'password', available: true }],
        oauth_providers: [],
        password_encryption_enabled: false,
      },
    },
  })
  vi.spyOn(api, 'post').mockImplementation(async (url) => ({
    data: {
      success: true,
      data:
        url === '/api/verify'
          ? {
              proof_token: 'audit-proof',
              scope: 'audit_log.clear',
              method: 'password',
              expires_at: Math.floor(Date.now() / 1000) + 300,
            }
          : { deleted_count: 4 },
    },
  }))
})

afterEach(() => {
  useAuthStore.setState(useAuthStore.getInitialState(), true)
})

test('only root sees audit cleanup controls', () => {
  renderCleanup(ROLE.ADMIN)
  expect(
    screen.queryByRole('group', { name: 'Clean audit logs' })
  ).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
  expect(api.post).not.toHaveBeenCalled()
})

test('canceling confirmation or verification never clears audit logs', async () => {
  renderCleanup()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Clear audit logs' }))
  await user.click(
    within(await screen.findByRole('alertdialog')).getByRole('button', {
      name: 'Cancel',
    })
  )
  expect(api.get).not.toHaveBeenCalled()
  await beginVerification()
  await user.click(
    within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' })
  )
  expect(api.post).not.toHaveBeenCalled()
  await waitFor(() =>
    expect(
      screen.getByRole('button', { name: 'Clear audit logs' })
    ).toBeEnabled()
  )
})

test('verification binds the selected date and sends its proof with the cleanup', async () => {
  const { client } = renderCleanup()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const user = await beginVerification()
  expect(screen.getByLabelText('Time')).toBeDisabled()
  expect(api.post).not.toHaveBeenCalled()
  await user.type(
    screen.getByLabelText('Password', { selector: 'input' }),
    'secret'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))

  await waitFor(() => expect(api.post).toHaveBeenCalledTimes(2))
  const verificationBody = vi.mocked(api.post).mock.calls[0]?.[1] as {
    scope: string
    context: { target_timestamp: number }
  }
  expect(verificationBody.scope).toBe('audit_log.clear')
  expect(verificationBody.context.target_timestamp).toBeLessThan(
    Date.now() / 1000
  )
  expect(api.post).toHaveBeenLastCalledWith(
    '/api/log/audit/cleanup',
    { target_timestamp: verificationBody.context.target_timestamp },
    expect.objectContaining({
      headers: { 'X-Security-Proof': 'audit-proof' },
      signal: expect.any(AbortSignal),
    })
  )
  await waitFor(() =>
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['audit'] })
  )
})

test('clearing the selected date blocks cleanup before requesting verification', async () => {
  renderCleanup()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Clear' }))
  await user.click(screen.getByRole('button', { name: 'Clear audit logs' }))
  expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  expect(api.get).not.toHaveBeenCalled()
  expect(api.post).not.toHaveBeenCalled()
})

test('losing root privileges while verifying cancels the cleanup', async () => {
  renderCleanup()
  await beginVerification()
  act(() => {
    useAuthStore
      .getState()
      .auth.setUser({ id: 1, username: 'operator', role: ROLE.ADMIN })
  })
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  expect(api.post).not.toHaveBeenCalled()
})

test('a failed verification leaves audit records intact', async () => {
  vi.mocked(api.post).mockRejectedValue(
    new AuthOperationError('Verification failed. Please try again.')
  )
  renderCleanup()
  const user = await beginVerification()
  await user.type(
    screen.getByLabelText('Password', { selector: 'input' }),
    'incorrect'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Verification failed. Please try again.'
  )
  expect(api.post).toHaveBeenCalledTimes(1)
  expect(vi.mocked(api.post).mock.calls[0]?.[0]).toBe('/api/verify')
})

test('leaving the page aborts an active cleanup and ignores its late result', async () => {
  let finishCleanup!: (result: {
    data: { success: boolean; data: { deleted_count: number } }
  }) => void
  const originalPost = vi.mocked(api.post).getMockImplementation()
  vi.mocked(api.post).mockImplementation((url, ...args) => {
    if (url === '/api/log/audit/cleanup') {
      return new Promise((resolve) => {
        finishCleanup = resolve
      })
    }
    if (!originalPost) throw new Error('Missing API fixture')
    return originalPost(url, ...args)
  })
  const { unmount, client } = renderCleanup()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const user = await beginVerification()
  await user.type(
    screen.getByLabelText('Password', { selector: 'input' }),
    'secret'
  )
  await user.click(screen.getByRole('button', { name: 'Verify' }))
  await waitFor(() => expect(api.post).toHaveBeenCalledTimes(2))
  const config = vi.mocked(api.post).mock.calls[1]?.[2] as {
    signal: AbortSignal
  }
  expect(config.signal.aborted).toBe(false)
  unmount()
  expect(config.signal.aborted).toBe(true)
  await act(async () =>
    finishCleanup({ data: { success: true, data: { deleted_count: 4 } } })
  )
  expect(invalidate).not.toHaveBeenCalled()
})

test('the former export settings URL redirects to the combined logs page', async () => {
  expect(OPERATIONS_SECTION_IDS).not.toContain('log-export')
  const root = createRootRoute()
  const route = createRoute({
    getParentRoute: () => root,
    path: '/system-settings/operations/$section',
    beforeLoad: OperationsRoute.options.beforeLoad as (context: {
      params: { section: string }
    }) => void,
  })
  const router = createRouter({
    routeTree: root.addChildren([route]),
    history: createMemoryHistory({
      initialEntries: ['/system-settings/operations/log-export'],
    }),
  })
  await router.load()
  expect(router.state.location.pathname).toBe(
    '/system-settings/operations/logs'
  )
})
