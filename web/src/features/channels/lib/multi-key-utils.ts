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
import {
  MULTI_KEY_STATUS_CONFIG,
  MULTI_KEY_CONFIRM_MESSAGES,
} from '../constants'
import type { MultiKeyConfirmAction } from '../types'

/** Mirrors the `binding:"max=255"` constraint on `MultiKeyManageRequest.Reason`. */
export const MULTI_KEY_REASON_MAX_LENGTH = 255

/**
 * Confirmation body per action. A Record keyed by the action union means a newly
 * added action type fails to compile instead of rendering an empty dialog.
 */
const MULTI_KEY_CONFIRM_MESSAGE_BY_TYPE: Record<
  MultiKeyConfirmAction['type'],
  string
> = {
  enable: MULTI_KEY_CONFIRM_MESSAGES.ENABLE,
  disable: MULTI_KEY_CONFIRM_MESSAGES.DISABLE,
  delete: MULTI_KEY_CONFIRM_MESSAGES.DELETE,
  'enable-all': MULTI_KEY_CONFIRM_MESSAGES.ENABLE_ALL,
  'disable-all': MULTI_KEY_CONFIRM_MESSAGES.DISABLE_ALL,
  'delete-disabled': MULTI_KEY_CONFIRM_MESSAGES.DELETE_DISABLED,
}

/**
 * Whether the action needs the destructive confirmation styling. Same Record
 * treatment: a new action type has to declare its own answer.
 */
const MULTI_KEY_DESTRUCTIVE_BY_TYPE: Record<
  MultiKeyConfirmAction['type'],
  boolean
> = {
  enable: false,
  disable: false,
  delete: true,
  // Enable All replaces all three ChannelInfo maps with empty maps, so every
  // upstream diagnostic (reason and disabled time) is destroyed for good.
  'enable-all': true,
  'disable-all': true,
  'delete-disabled': true,
}

/**
 * Get status badge configuration for multi-key status
 */
export function getMultiKeyStatusConfig(status: number) {
  return (
    MULTI_KEY_STATUS_CONFIG[status as keyof typeof MULTI_KEY_STATUS_CONFIG] || {
      variant: 'neutral' as const,
      label: 'Unknown',
    }
  )
}

/**
 * Get confirmation message for multi-key action
 */
export function getMultiKeyConfirmMessage(
  action: MultiKeyConfirmAction | null
): string {
  if (!action) return ''
  return MULTI_KEY_CONFIRM_MESSAGE_BY_TYPE[action.type]
}

/**
 * Check if action is destructive
 */
export function isDestructiveAction(
  action: MultiKeyConfirmAction | null
): boolean {
  if (!action) return false
  return MULTI_KEY_DESTRUCTIVE_BY_TYPE[action.type]
}

// ============================================================================
// Per-key test result buckets
// ============================================================================

/**
 * Classification of one key's test result. `testing` comes from the in-flight
 * index set only and is never stored in the result map, so it can never be
 * mistaken for a finished verdict by the bulk actions.
 */
export type TestBucket =
  | { kind: 'untested' }
  | { kind: 'testing' }
  | { kind: 'success'; code?: number }
  | { kind: 'http-error'; code: number } // code can legitimately be 200
  | { kind: 'no-response' }

/** Buckets that can back a dropdown option; in-flight keys are excluded by design. */
export type TestBucketOption = Exclude<TestBucket, { kind: 'testing' }>

/**
 * The fields the bucket decision reads from a test result.
 * Structurally satisfied by `ChannelTestOutcome` from `./channel-actions`.
 */
export type TestBucketSource = {
  success: boolean
  /** Status code the upstream really returned; absent/0 = no HTTP response at all */
  upstreamStatusCode?: number
  /** Whether our gateway produced a JSON body; false for timeouts and network errors */
  serverResponded?: boolean
  /** Server's verdict on whether this key's credential was really used; absent = it was not */
  keyAttempted?: boolean
}

/**
 * Whether a settled outcome says anything at all about the key that was tested.
 *
 * The only question is "was this failure a verdict on THIS key", and the server
 * answers it rather than the UI guessing: `key_attempted` is raised at the
 * `adaptor.DoRequest` boundary in `controller/channel-test.go`, past which this
 * key's own credential has taken part in a real upstream call. Everything
 * before that boundary — an unsupported `test_model`, a price-map or
 * param-override failure, an invalid API type — is request construction and
 * fails identically for every key of the channel.
 *
 * BOTH directions of a wrong answer do damage, which is why the decision is
 * delegated instead of inferred:
 * - Reading a channel-wide failure as per-key drops every key of a healthy
 *   channel into a selectable bucket with a disable reason pre-filled, and one
 *   "disable selected" click retires the whole channel.
 * - Reading a real dead-key verdict as channel-wide renders that key as `Not
 *   tested`, hiding it from the bulk actions that exist to retire it, and
 *   aborts the rest of the run on top.
 *
 * The previous version guessed with an allowlist holding the one `error_code`
 * raised at that boundary, and AWS Bedrock killed it: its adaptor returns
 * `(nil, nil)` from `DoRequest` and performs the real InvokeModel in
 * `DoResponse` (`relay/channel/aws/relay-aws.go`), so a revoked credential
 * comes back as `aws_invoke_error` with no `upstream_status_code` and counted as
 * "not the key's fault". Do NOT reintroduce an error-code list in either
 * direction: no code on its own says whether the key reached the wire.
 *
 * @param outcome - The settled test outcome.
 * @returns True when the outcome may be attributed to the tested key.
 */
export function isKeyAttributableFailure(outcome: {
  success: boolean
  upstreamStatusCode?: number
  serverResponded?: boolean
  keyAttempted?: boolean
}): boolean {
  if (outcome.success) return true
  // The upstream really answered, so whatever it said is a verdict on this key.
  if (outcome.upstreamStatusCode) return true
  // Our own gateway never answered: timeout or network error. Timeouts are
  // ruled to belong in `no-response`, so they stay attributable.
  if (!outcome.serverResponded) return true
  // The gateway answered but named no upstream status, so only it knows whether
  // the credential was ever dispatched. Absent means it was not.
  return outcome.keyAttempted === true
}

/**
 * Resolves the bucket of one key. This is the single decision point shared by
 * the status-code dropdown and the table cell: success wins over any code,
 * because a 200 with an unusable body fails while an AWS AKSK key can succeed
 * without ever exposing an HTTP status.
 *
 * @param options - The stored result, if any, plus whether the key is in flight.
 * @returns The bucket the key belongs to right now.
 */
export function resolveTestBucket(options: {
  result?: TestBucketSource
  isTesting?: boolean
}): TestBucket {
  if (options.isTesting) return { kind: 'testing' }

  const result = options.result
  if (!result) return { kind: 'untested' }

  if (result.success) {
    return { kind: 'success', code: result.upstreamStatusCode }
  }

  // Defence in depth: the batch hook already refuses to store a
  // channel-wide failure, so in practice this branch is unreachable. It stays
  // because a bucket that can be bulk-disabled must never depend on every
  // caller remembering to filter first.
  if (!isKeyAttributableFailure(result)) return { kind: 'untested' }

  if (result.upstreamStatusCode) {
    return { kind: 'http-error', code: result.upstreamStatusCode }
  }

  // Timeouts and local failures — DNS, TLS, a refused or dropped connection —
  // land here with no code. Cancellations do not: the batch hook drops the
  // outcome of a request it aborted itself
  // (`hooks/use-multi-key-batch-test.ts`), so a key the operator stopped stays
  // `untested` on purpose, out of reach of the bulk actions that would otherwise
  // disable or delete keys that were never really tested.
  return { kind: 'no-response' }
}

/**
 * Stable token identifying a bucket, used both as the dropdown value and to
 * match rows while filtering.
 */
export function getTestBucketValue(bucket: TestBucketOption): string {
  switch (bucket.kind) {
    case 'untested':
      return 'untested'
    case 'success':
      return 'success'
    case 'http-error':
      return `http-${bucket.code}`
    case 'no-response':
      return 'no-response'
    default: {
      const _exhaustive: never = bucket
      return _exhaustive
    }
  }
}

/**
 * i18n template and interpolation values for a bucket's dropdown label.
 *
 * @param bucket - The bucket to label.
 * @param count - Number of keys currently in that bucket.
 * @returns The source string to pass to `t()` together with its params.
 */
export function getTestBucketLabel(
  bucket: TestBucketOption,
  count: number
): { key: string; params: { count: number; code?: number } } {
  switch (bucket.kind) {
    case 'untested':
      return { key: 'Not tested ({{count}})', params: { count } }
    case 'success':
      return { key: 'Success ({{count}})', params: { count } }
    case 'http-error':
      if (bucket.code === 200) {
        return {
          key: 'HTTP 200 / invalid response ({{count}})',
          params: { count },
        }
      }
      return {
        key: 'HTTP {{code}} ({{count}})',
        params: { count, code: bucket.code },
      }
    case 'no-response':
      return { key: 'No response (local error) ({{count}})', params: { count } }
    default: {
      const _exhaustive: never = bucket
      return _exhaustive
    }
  }
}

function getTestBucketRank(bucket: TestBucketOption): number {
  switch (bucket.kind) {
    case 'success':
      return 0
    case 'http-error':
      return 1
    case 'no-response':
      return 2
    case 'untested':
      return 3
    default: {
      const _exhaustive: never = bucket
      return _exhaustive
    }
  }
}

/**
 * Dropdown order: success, then HTTP codes ascending (200 included), then
 * no-response, then untested. The synthetic `all` entry is prepended by the
 * caller and never goes through here.
 */
export function compareTestBuckets(
  a: TestBucketOption,
  b: TestBucketOption
): number {
  const rankDiff = getTestBucketRank(a) - getTestBucketRank(b)
  if (rankDiff !== 0) return rankDiff
  if (a.kind === 'http-error' && b.kind === 'http-error') {
    return a.code - b.code
  }
  return 0
}
