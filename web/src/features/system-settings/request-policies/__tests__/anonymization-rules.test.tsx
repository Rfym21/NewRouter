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
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { AnonymizationRule } from '../../api'
import { SettingsPageProvider } from '../../components/settings-page-context'
import { RuntimeSettingsSection } from '../../operations/runtime-settings-section'
import { RequestChecksSection } from '../request-checks-section'

const rulesKey = 'anonymization_setting.custom_rules'
const rule = (name: string, pattern: string): AnonymizationRule => ({
  name,
  mode: 'literal',
  pattern,
  enabled: true,
  entity_type: 'CUSTOM',
  capture_group: 0,
})

function Fixture({ rules = '[]' }: { rules?: string }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <RequestChecksSection
          defaultValues={{
            CheckSensitiveEnabled: false,
            CheckSensitiveOnPromptEnabled: false,
            SensitiveErrorReturnWordsEnabled: false,
            AnonymizationEnabled: false,
            AnonymizationStrictContextEnabled: false,
            AnonymizationStableSurrogates: false,
            SensitiveWords: '',
            [rulesKey]: rules,
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

function renderRules(rules: AnonymizationRule[] | string = []) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Fixture
        rules={typeof rules === 'string' ? rules : JSON.stringify(rules)}
      />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, data: { options: {} } },
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        anonymized: '[CUSTOM_EXAMPLE]',
        matches: [
          {
            text: 'Project Alpha',
            entity_type: 'CUSTOM',
            rule_name: 'Project',
            start: 0,
            end: 13,
          },
        ],
      },
    },
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})

function AnalyzerFixture({
  directory = '',
  url = '',
}: {
  directory?: string
  url?: string
}) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setContainer} />
      <SettingsPageProvider actionsContainer={container}>
        <RuntimeSettingsSection
          defaultValues={{
            'runtime_setting.relay_timeout': 0,
            'runtime_setting.streaming_timeout': 300,
            'runtime_setting.max_request_body_mb': 128,
            'runtime_setting.stream_scanner_max_buffer_mb': 128,
            'runtime_setting.relay_max_idle_conns': 500,
            'runtime_setting.relay_max_idle_conns_per_host': 100,
            'runtime_setting.global_api_rate_limit_enabled': false,
            'runtime_setting.global_api_rate_limit': 360,
            'runtime_setting.global_api_rate_limit_duration': 180,
            'runtime_setting.global_web_rate_limit_enabled': false,
            'runtime_setting.global_web_rate_limit': 60,
            'runtime_setting.global_web_rate_limit_duration': 180,
            'runtime_setting.critical_rate_limit_enabled': false,
            'runtime_setting.critical_rate_limit': 20,
            'runtime_setting.critical_rate_limit_duration': 1200,
            'runtime_setting.search_rate_limit_enabled': false,
            'runtime_setting.search_rate_limit': 10,
            'runtime_setting.search_rate_limit_duration': 60,
            'runtime_setting.azure_default_api_version': '',
            'runtime_setting.max_file_download_mb': 64,
            'runtime_setting.anonymization_local_ner_model_dir': directory,
            'runtime_setting.anonymization_presidio_url': url,
            'runtime_setting.anonymization_context_ttl_seconds': 3600,
            'runtime_setting.anonymization_max_contexts': 1024,
            'runtime_setting.anonymization_max_context_kb': 4096,
            'runtime_setting.force_stream_option_enabled': true,
            'runtime_setting.count_token_enabled': true,
            'runtime_setting.get_media_token_enabled': true,
            'runtime_setting.get_media_token_not_stream_enabled': false,
            'runtime_setting.sync_upstream_base': '',
            'runtime_setting.sync_http_timeout_seconds': 15,
            'runtime_setting.sync_http_retry': 3,
            'runtime_setting.sync_http_max_mb': 32,
            'runtime_setting.notify_limit_count': 10,
            'runtime_setting.notification_limit_duration': 60,
            'runtime_setting.channel_test_request_interval': 0,
            'runtime_setting.linux_do_token_endpoint': '',
            'runtime_setting.linux_do_user_endpoint': '',
            'runtime_setting.debug_enabled': false,
            'runtime_setting.access_log_enabled': false,
            'runtime_setting.error_log_enabled': true,
            'perf_metrics_setting.enabled': false,
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

function renderAnalyzer(directory = '', url = '') {
  render(
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { mutations: { retry: false } } })
      }
    >
      <AnalyzerFixture directory={directory} url={url} />
    </QueryClientProvider>
  )
}

test.each([
  ['/models/guard', '', '', 'https://presidio.example.test'],
  ['', 'https://presidio.example.test', '/models/guard', ''],
])(
  'switching analyzer from %s%s clears the saved source first',
  async (directory, url, nextDirectory, nextUrl) => {
    renderAnalyzer(directory, url)
    fireEvent.change(
      screen.getByRole('textbox', {
        name: 'Local anonymization model directory',
      }),
      { target: { value: nextDirectory } }
    )
    fireEvent.change(
      screen.getByRole('textbox', { name: 'Presidio analyzer URL' }),
      {
        target: { value: nextUrl },
      }
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))

    await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
    expect(api.put).toHaveBeenNthCalledWith(1, '/api/option/', {
      key: directory
        ? 'runtime_setting.anonymization_local_ner_model_dir'
        : 'runtime_setting.anonymization_presidio_url',
      value: '',
    })
    expect(api.put).toHaveBeenNthCalledWith(2, '/api/option/', {
      key: nextDirectory
        ? 'runtime_setting.anonymization_local_ner_model_dir'
        : 'runtime_setting.anonymization_presidio_url',
      value: nextDirectory || nextUrl,
    })
  }
)

test('analyzer fields reject simultaneous sources and malformed remote endpoints', async () => {
  renderAnalyzer('/models/guard')
  const remote = screen.getByRole('textbox', { name: 'Presidio analyzer URL' })
  fireEvent.change(remote, {
    target: { value: 'https://presidio.example.test' },
  })
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(
    await screen.findByText(
      'Configure either a local model directory or a Presidio URL'
    )
  ).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()

  fireEvent.change(
    screen.getByRole('textbox', {
      name: 'Local anonymization model directory',
    }),
    { target: { value: '' } }
  )
  fireEvent.change(remote, {
    target: { value: 'https://presidio.example.test?token=secret' },
  })
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(await screen.findByText('Invalid Presidio analyzer URL')).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})

test('both analyzer fields can be cleared and a blank analyzer can save unrelated settings', async () => {
  renderAnalyzer('/models/guard')
  fireEvent.change(
    screen.getByRole('textbox', {
      name: 'Local anonymization model directory',
    }),
    { target: { value: '  ' } }
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(1))
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: 'runtime_setting.anonymization_local_ner_model_dir',
    value: '',
  })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled()
  )
  fireEvent.change(
    screen.getByRole('spinbutton', {
      name: 'Anonymization context TTL (seconds)',
    }),
    { target: { value: '7200' } }
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: 'runtime_setting.anonymization_context_ttl_seconds',
    value: 7200,
  })
  fireEvent.change(
    screen.getByRole('spinbutton', {
      name: 'Anonymization context max size (KB)',
    }),
    { target: { value: '8192' } }
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(3))
  expect(api.put).toHaveBeenLastCalledWith('/api/option/', {
    key: 'runtime_setting.anonymization_max_context_kb',
    value: 8192,
  })
})

test('strict context defaults off and saves through the atomic policy API', async () => {
  const user = userEvent.setup()
  renderRules()
  const toggle = screen.getByRole('switch', {
    name: 'Require anonymization context',
  })
  expect(toggle).not.toBeChecked()
  await user.click(toggle)
  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))
  await waitFor(() =>
    expect(api.patch).toHaveBeenCalledWith('/api/option/request_policy', {
      options: { AnonymizationStrictContextEnabled: 'true' },
    })
  )
})

test('current rules can be previewed without saving and then saved through the atomic policy API', async () => {
  const user = userEvent.setup()
  renderRules()
  await user.click(
    screen.getByRole('button', { name: 'Add anonymization rule' })
  )
  const group = screen.getByRole('group', { name: 'Anonymization rule 1' })
  await user.type(
    within(group).getByRole('textbox', { name: 'Rule name' }),
    'Project'
  )
  await user.type(
    within(group).getByRole('textbox', { name: 'Keyword' }),
    'Project Alpha'
  )
  await user.type(
    screen.getByRole('textbox', { name: 'Sample text' }),
    'Project Alpha'
  )
  await user.click(
    screen.getByRole('button', { name: 'Preview anonymization' })
  )

  expect(
    await screen.findByRole('textbox', { name: 'Anonymized text' })
  ).toHaveValue('[CUSTOM_EXAMPLE]')
  expect(
    within(
      screen.getByRole('table', { name: 'Anonymization matches' })
    ).getByText('Project Alpha')
  ).toBeVisible()
  expect(api.post).toHaveBeenCalledWith(
    '/api/option/anonymization/preview',
    { rules: [rule('Project', 'Project Alpha')], text: 'Project Alpha' },
    { signal: expect.anything() }
  )
  expect(api.patch).not.toHaveBeenCalled()
  expect(api.put).not.toHaveBeenCalled()
  expect(
    screen.getByRole('switch', { name: 'Enable anonymization proxy' })
  ).not.toBeChecked()

  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))

  await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(1))
  expect(api.patch).toHaveBeenCalledWith('/api/option/request_policy', {
    options: { [rulesKey]: JSON.stringify([rule('Project', 'Project Alpha')]) },
  })
})

test('deleting and disabling rules changes only the serialized rule list', async () => {
  const user = userEvent.setup()
  renderRules([rule('Alpha', 'Project Alpha'), rule('Beta', 'Project Beta')])
  await user.click(
    within(
      screen.getByRole('group', { name: 'Anonymization rule 1' })
    ).getByRole('button', { name: 'Delete' })
  )
  const remaining = screen.getByRole('group', { name: 'Anonymization rule 1' })
  expect(
    within(remaining).getByRole('textbox', { name: 'Rule name' })
  ).toHaveValue('Beta')
  await user.click(
    within(remaining).getByRole('switch', { name: 'Enable this rule' })
  )
  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))

  await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(1))
  expect(api.patch).toHaveBeenCalledWith('/api/option/request_policy', {
    options: {
      [rulesKey]: JSON.stringify([
        { ...rule('Beta', 'Project Beta'), enabled: false },
      ]),
    },
  })
})

test('rule order can be changed to control matching priority', async () => {
  const user = userEvent.setup()
  renderRules([rule('Alpha', 'Project Alpha'), rule('Beta', 'Project Beta')])
  await user.click(
    within(
      screen.getByRole('group', { name: 'Anonymization rule 1' })
    ).getByRole('button', { name: 'Move down' })
  )
  expect(
    within(
      screen.getByRole('group', { name: 'Anonymization rule 1' })
    ).getByRole('textbox', { name: 'Rule name' })
  ).toHaveValue('Beta')
  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))
  await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(1))
  expect(api.patch).toHaveBeenCalledWith('/api/option/request_policy', {
    options: {
      [rulesKey]: JSON.stringify([
        rule('Beta', 'Project Beta'),
        rule('Alpha', 'Project Alpha'),
      ]),
    },
  })
})

test('regex preview errors are displayed without persisting the rules and can be retried', async () => {
  const user = userEvent.setup()
  vi.mocked(api.post).mockResolvedValueOnce({
    data: {
      success: false,
      code: 'ANONYMIZATION_RULES_INVALID',
      message: 'Invalid anonymization rules',
    },
  })
  renderRules([{ ...rule('Project', '['), mode: 'regex' }])
  await user.type(
    screen.getByRole('textbox', { name: 'Sample text' }),
    'Project Alpha'
  )
  await user.click(
    screen.getByRole('button', { name: 'Preview anonymization' })
  )

  expect(await screen.findByText('Invalid anonymization rules')).toBeVisible()
  expect(api.patch).not.toHaveBeenCalled()
  expect(
    screen.getByRole('textbox', { name: 'Regular expression' })
  ).toHaveValue('[')
  fireEvent.change(
    screen.getByRole('textbox', { name: 'Regular expression' }),
    { target: { value: 'Project (Alpha)' } }
  )
  fireEvent.change(screen.getByRole('spinbutton', { name: 'Capture group' }), {
    target: { value: '1' },
  })
  await user.click(
    screen.getByRole('button', { name: 'Preview anonymization' })
  )
  expect(
    await screen.findByRole('textbox', { name: 'Anonymized text' })
  ).toHaveValue('[CUSTOM_EXAMPLE]')
  expect(api.post).toHaveBeenLastCalledWith(
    '/api/option/anonymization/preview',
    {
      rules: [
        {
          ...rule('Project', 'Project (Alpha)'),
          mode: 'regex',
          capture_group: 1,
        },
      ],
      text: 'Project Alpha',
    },
    { signal: expect.anything() }
  )
})

test('empty rules are valid while incomplete new rules cannot be saved', async () => {
  const user = userEvent.setup()
  renderRules()
  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))
  expect(api.patch).not.toHaveBeenCalled()
  await user.click(
    screen.getByRole('button', { name: 'Add anonymization rule' })
  )
  await user.click(screen.getByRole('button', { name: 'Save sensitive words' }))
  expect(await screen.findByText('Invalid anonymization rules')).toBeVisible()
  expect(api.patch).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Clear rules' }))
  expect(screen.getByText('No custom anonymization rules')).toBeVisible()
})

test('a failed save keeps edits available for retry', async () => {
  const user = userEvent.setup()
  vi.mocked(api.patch).mockRejectedValueOnce(new Error('Network unavailable'))
  renderRules([rule('Project', 'Project Alpha')])
  fireEvent.change(screen.getByRole('textbox', { name: 'Keyword' }), {
    target: { value: 'Project Beta' },
  })
  const save = screen.getByRole('button', { name: 'Save sensitive words' })
  await user.click(save)
  await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(1))
  await waitFor(() => expect(save).toBeEnabled())
  expect(screen.getByRole('textbox', { name: 'Keyword' })).toHaveValue(
    'Project Beta'
  )
  await user.click(save)
  await waitFor(() => expect(api.patch).toHaveBeenCalledTimes(2))
  expect(api.patch).toHaveBeenLastCalledWith('/api/option/request_policy', {
    options: { [rulesKey]: JSON.stringify([rule('Project', 'Project Beta')]) },
  })
})

test('editing sample text cancels the old preview and ignores a late response', async () => {
  const user = userEvent.setup()
  let resolvePreview: (value: unknown) => void = () => undefined
  vi.mocked(api.post).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolvePreview = resolve
      })
  )
  renderRules([rule('Project', 'Project Alpha')])
  await user.type(
    screen.getByRole('textbox', { name: 'Sample text' }),
    'Project Alpha'
  )
  await user.click(
    screen.getByRole('button', { name: 'Preview anonymization' })
  )
  expect(screen.getByRole('button', { name: 'Previewing...' })).toBeDisabled()
  fireEvent.change(screen.getByRole('textbox', { name: 'Sample text' }), {
    target: { value: 'Project Beta' },
  })
  expect(vi.mocked(api.post).mock.calls[0][2]?.signal?.aborted).toBe(true)
  await act(async () =>
    resolvePreview({
      data: {
        success: true,
        data: { anonymized: 'stale preview', matches: [] },
      },
    })
  )
  expect(
    screen.queryByRole('textbox', { name: 'Anonymized text' })
  ).not.toBeInTheDocument()
})
