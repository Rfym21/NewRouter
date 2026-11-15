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
const CODEX_SIMULATION_DEFAULT_SESSION_ID =
  '01a1dec3-6e18-7193-a48f-e1c4667583c5'
const CODEX_SIMULATION_DEFAULT_TURN_ID = '01a1dec3-6e31-77a2-8afe-c49d875185a6'

export const CODEX_SIMULATION_DEFAULT_HEADERS = {
  accept: '{accept}',
  'content-type': 'application/json',
  originator: 'codex-tui',
  'user-agent':
    'codex-tui/0.167.0 (Windows 10.0.26340; x86_64) WindowsTerminal (codex-tui; 0.167.0)',
  'session-id': '{session_id}',
  'thread-id': '{thread_id}',
  'x-client-request-id': '{thread_id}',
  'x-codex-window-id': '{window_id}',
  'x-codex-turn-metadata': '{turn_metadata}',
  'x-codex-beta-features': 'remote_compaction_v2',
} as const

export const CODEX_SIMULATION_DEFAULT_METADATA = {
  installation_id: '39faaa95-1437-46d2-8f53-100e5d157bfb',
  session_id: CODEX_SIMULATION_DEFAULT_SESSION_ID,
  thread_id: CODEX_SIMULATION_DEFAULT_SESSION_ID,
  turn_id: CODEX_SIMULATION_DEFAULT_TURN_ID,
  root_turn_id: CODEX_SIMULATION_DEFAULT_TURN_ID,
  window_id: `${CODEX_SIMULATION_DEFAULT_SESSION_ID}:0`,
  window_number: 0,
  context_window_id: '01a1dec3-6e1c-76c1-be3e-1befdfc5c5a7',
  agent_name: '/root',
  request_kind: 'turn',
  history_initialization: 'new',
  thread_source: 'user',
  turn_trigger: 'user',
  sandbox: 'none',
  sandbox_mode: 'read-only',
  auto_review_enabled: false,
  node_repl_auto_review_required: false,
  node_repl_disabled: false,
  turn_started_at_unix_ms: 1794740237086,
  analytics_enabled: true,
} as const

export const CODEX_SIMULATION_DEFAULT_SYSTEM = [
  {
    type: 'message',
    role: 'developer',
    content: [
      {
        type: 'input_text',
        text: 'You are Codex, a coding agent based on GPT-5. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.',
      },
    ],
  },
] as const

export const CODEX_SIMULATION_DEFAULT_HEADERS_JSON = JSON.stringify(
  CODEX_SIMULATION_DEFAULT_HEADERS,
  null,
  2
)
export const CODEX_SIMULATION_DEFAULT_METADATA_JSON = JSON.stringify(
  CODEX_SIMULATION_DEFAULT_METADATA,
  null,
  2
)
export const CODEX_SIMULATION_DEFAULT_SYSTEM_JSON = JSON.stringify(
  CODEX_SIMULATION_DEFAULT_SYSTEM,
  null,
  2
)
