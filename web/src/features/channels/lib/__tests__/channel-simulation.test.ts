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
import { describe, expect, test } from 'vitest'

import {
  claudeSimulationShowsHeaders,
  codexSimulationShowsHeaders,
  supportsClaudeSimulation,
  supportsCodexSimulation,
} from '../../constants'
import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

function parseForm(type: number, overrides: Record<string, unknown> = {}) {
  return channelFormSchema.parse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'simulation',
    type,
    base_url: 'https://upstream.example.com',
    key: 'test-key',
    models: 'test-model',
    ...overrides,
  })
}

function settingsFor(type: number, overrides: Record<string, unknown> = {}) {
  const form = parseForm(type, overrides)
  const payload = transformFormDataToCreatePayload(form).channel
  return JSON.parse(payload.settings ?? '{}') as Record<string, unknown>
}

function roundTrip(type: number, overrides: Record<string, unknown> = {}) {
  const form = parseForm(type, overrides)
  const payload = transformFormDataToCreatePayload(form).channel
  const channel = channelSchema.parse({
    ...payload,
    other: '',
    remark: '',
    id: 7,
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  return transformChannelToFormDefaults(channel)
}

describe('channel client simulation', () => {
  test('capability sets cover every native Messages and Responses channel', () => {
    for (const type of [3, 14, 20, 33, 41, 58, 59, 60]) {
      expect(supportsClaudeSimulation(type)).toBe(true)
    }
    for (const type of [33, 41]) {
      expect(claudeSimulationShowsHeaders(type)).toBe(false)
    }
    for (const type of [3, 14, 20, 58, 59, 60]) {
      expect(claudeSimulationShowsHeaders(type)).toBe(true)
    }
    for (const type of [1, 3, 8, 20, 48, 57, 58, 59, 60]) {
      expect(supportsCodexSimulation(type)).toBe(true)
    }
    expect(codexSimulationShowsHeaders(57)).toBe(true)
    expect(codexSimulationShowsHeaders(1)).toBe(true)
    expect(supportsClaudeSimulation(1)).toBe(false)
    expect(supportsCodexSimulation(14)).toBe(false)
  })

  test('Claude defaults carry the current CLI fingerprint', () => {
    const headers = JSON.parse(
      CHANNEL_FORM_DEFAULT_VALUES.claude_code_simulation_headers
    )
    expect(headers['user-agent']).toBe('claude-cli/2.1.325 (external, cli)')
    expect(headers['x-stainless-package-version']).toBe('0.128.0')
    expect(headers['x-stainless-runtime-version']).toBe('v26.12.1')
    expect(headers['anthropic-beta']).toContain(
      'thinking-token-count-2026-05-13'
    )
    expect(headers['anthropic-beta']).toContain('advanced-tool-use-2025-11-20')
    expect(headers['anthropic-beta']).not.toContain('{{')
    expect(headers.authorization).toBeUndefined()
  })

  test('New API keeps both full simulations', () => {
    const settings = settingsFor(60, {
      claude_code_simulation_enabled: true,
      codex_simulation_enabled: true,
    })
    expect(settings.claude_code_simulation_enabled).toBe(true)
    expect(settings.codex_simulation_enabled).toBe(true)
    expect(settings.claude_code_simulation).toMatchObject({
      metadata: { device_id: expect.any(String) },
      system: expect.any(Array),
      headers: expect.any(Object),
    })
    expect(settings.codex_simulation).toMatchObject({
      metadata: { session_id: expect.any(String) },
      system: expect.any(Array),
      headers: expect.any(Object),
    })
  })

  test('AWS and Vertex keep Claude metadata but omit headers', () => {
    const overridesByType: Record<number, Record<string, unknown>> = {
      33: { key: 'key|us-east-1', aws_key_type: 'api_key' },
      41: { other: '{}', key: 'vertex-key', vertex_key_type: 'api_key' },
    }
    for (const type of [33, 41]) {
      const settings = settingsFor(type, {
        ...overridesByType[type],
        claude_code_simulation_enabled: true,
      })
      expect(settings.claude_code_simulation).toMatchObject({
        metadata: { device_id: expect.any(String) },
        system: expect.any(Array),
      })
      expect(settings.claude_code_simulation).not.toHaveProperty('headers')
    }
  })

  test('the Codex channel persists auth-filtered simulation headers', () => {
    const settings = settingsFor(57, {
      key: JSON.stringify({ access_token: 'token', account_id: 'account' }),
      codex_simulation_enabled: true,
    })
    expect(settings.codex_simulation).toMatchObject({
      metadata: { session_id: expect.any(String) },
      system: expect.any(Array),
      headers: expect.any(Object),
    })
  })

  test.each([
    [1, {}],
    [3, { other: '{}' }],
    [20, {}],
    [48, {}],
    [
      58,
      {
        advanced_custom: JSON.stringify({
          advanced_routes: [
            {
              incoming_path: '/v1/responses',
              upstream_path: '/v1/responses',
              converter: 'none',
            },
          ],
        }),
      },
    ],
  ])(
    'Responses-capable channel %i persists Codex headers',
    (type, overrides) => {
      const settings = settingsFor(type, {
        ...overrides,
        codex_simulation_enabled: true,
      })
      expect(settings.codex_simulation).toMatchObject({
        headers: expect.any(Object),
        metadata: expect.any(Object),
        system: expect.any(Array),
      })
    }
  )

  test('unsupported channel types drop both simulation blocks', () => {
    const settings = settingsFor(24, {
      claude_code_simulation_enabled: true,
      codex_simulation_enabled: true,
      settings: JSON.stringify({
        claude_code_simulation_enabled: true,
        codex_simulation_enabled: true,
      }),
    })
    expect(settings).not.toHaveProperty('claude_code_simulation_enabled')
    expect(settings).not.toHaveProperty('claude_code_simulation')
    expect(settings).not.toHaveProperty('codex_simulation_enabled')
    expect(settings).not.toHaveProperty('codex_simulation')
  })

  test('saved simulations round-trip into form defaults', () => {
    const form = roundTrip(60, {
      claude_code_simulation_enabled: true,
      codex_simulation_enabled: true,
    })
    expect(form.claude_code_simulation_enabled).toBe(true)
    expect(form.codex_simulation_enabled).toBe(true)
    expect(JSON.parse(form.claude_code_simulation_metadata)).toMatchObject({
      device_id: expect.any(String),
    })
    expect(JSON.parse(form.codex_simulation_metadata)).toMatchObject({
      session_id: expect.any(String),
    })
    expect(JSON.parse(form.codex_simulation_headers)).toHaveProperty(
      'originator',
      'codex-tui'
    )
  })

  test('invalid Codex header names fail validation', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'bad codex',
      type: 1,
      key: 'test-key',
      models: 'gpt-5',
      codex_simulation_enabled: true,
      codex_simulation_headers: JSON.stringify({ 'bad header': 'x' }),
    })
    expect(result.success).toBe(false)
  })

  test('blank Codex session_id fails validation', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'bad codex metadata',
      type: 1,
      key: 'test-key',
      models: 'gpt-5',
      codex_simulation_enabled: true,
      codex_simulation_metadata: JSON.stringify({ session_id: '  ' }),
    })
    expect(result.success).toBe(false)
  })

  test('empty Codex developer message objects fail validation', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'bad codex system',
      type: 1,
      key: 'test-key',
      models: 'gpt-5',
      codex_simulation_enabled: true,
      codex_simulation_system: JSON.stringify([{}]),
    })
    expect(result.success).toBe(false)
  })
})
