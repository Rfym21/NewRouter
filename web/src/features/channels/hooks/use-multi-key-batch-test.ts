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
import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  channelsQueryKeys,
  handleTestChannel,
  isKeyAttributableFailure,
  type ChannelTestOutcome,
} from '../lib'

const MULTI_KEY_TEST_CONCURRENCY = 5
const MULTI_KEY_TEST_DELAY_MS = 100
const MULTI_KEY_TEST_TIMEOUT_MS = 120_000

/** Shared id so repeated gateway rejections collapse into a single toast. */
const RATE_LIMIT_TOAST_ID = 'multi-key-test-rate-limited'

/** Shared id so repeated channel-wide failures collapse into a single toast. */
const MULTI_KEY_CHANNEL_ERROR_TOAST_ID = 'multi-key-test-channel-error'

/** Finished verdicts by key index. In-flight keys are never stored here. */
export type MultiKeyTestResultMap = Record<number, ChannelTestOutcome>

export type MultiKeyTestProgress = {
  total: number
  /** Keys that produced a real verdict; aborted and rate-limited ones are excluded */
  completed: number
  success: number
  failed: number
  startedAt: number
}

export type UseMultiKeyBatchTestOptions = {
  channelId: number
  channelName?: string
  /**
   * Invoked once after a run settles and at least one key produced a verdict,
   * so the dialog can refetch its own key status query. The hook only knows the
   * channel list query key, not the dialog's.
   */
  onRunSettled?: () => void
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, ms))
}

/**
 * Detects a rejection from this gateway's own rate limiter, which answers with a
 * bare 429 and no body (`middleware/rate-limit.go` writes the status only). It
 * is not an upstream verdict: a real upstream 429 arrives as an HTTP 200
 * response carrying `upstream_status_code`, which is exactly why the transport
 * status and the body's status are kept apart and both are inspected here.
 *
 * @param outcome - The settled test outcome.
 * @returns True when our own limiter rejected the request.
 */
function isGatewayRateLimited(outcome: ChannelTestOutcome): boolean {
  return (
    outcome.transportStatus === 429 && outcome.upstreamStatusCode === undefined
  )
}

/**
 * One in-flight per-key test: the promise so a second caller can join the
 * request instead of starting a rival one, the controller so `stop()` can
 * abort it.
 */
type MultiKeyTestRequest = {
  controller: AbortController
  promise: Promise<ChannelTestOutcome | undefined>
}

/**
 * Owns the per-key test results, progress and in-flight set of the multi-key
 * dialog, and nothing else.
 *
 * Intentional divergence from the pattern this copies
 * (`channel-test-dialog.tsx:617-755`): it accepts neither `setRowSelection` nor
 * the table instance. That dialog clears the row selection in its `finally`;
 * doing the same here would wipe the selection the moment a run ends and so
 * destroy the whole point of the feature — test, filter to HTTP 401, select,
 * bulk disable.
 *
 * @param options - Channel identity plus an optional post-run refetch hook.
 * @returns Test state and the run controls.
 */
export function useMultiKeyBatchTest(options: UseMultiKeyBatchTestOptions) {
  const { channelId, channelName, onRunSettled } = options
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [results, setResults] = useState<MultiKeyTestResultMap>({})
  const [testingIndexes, setTestingIndexes] = useState<Set<number>>(
    () => new Set()
  )
  const [isBatchTesting, setIsBatchTesting] = useState(false)
  const [isStopRequested, setIsStopRequested] = useState(false)
  const [progress, setProgress] = useState<MultiKeyTestProgress | null>(null)

  const runningRef = useRef(false) // re-entrancy latch
  const runIdRef = useRef(0) // stale-write latch
  const stopRef = useRef(false) // cooperative cancellation
  const rateLimitedRef = useRef(false) // our own limiter cut the run short
  // Whether the rate-limit SENTENCE is on screen, the counterpart of
  // `channelErrorNoticeVerdictsRef` below for the other pinned notice.
  // `rateLimitedRef` records that the limiter FIRED and survives a hand
  // dismissal; this one records that the notice is still being SHOWN, and only
  // that licenses restating it with the run's totals. Cleared by both ways a
  // notice can go — the helper below, and sonner's own close button through the
  // `onDismiss` wired at the raise.
  const rateLimitNoticeShownRef = useRef(false)
  const channelErrorRef = useRef(false) // the channel itself cut the run short
  // The channel's own explanation, kept so the end-of-run restatement of that
  // notice can repeat it: the detection sees the message but not the totals, the
  // loop sees the totals but not the message.
  const channelErrorMessageRef = useRef<string | undefined>(undefined)
  // What the channel-error notice currently on screen claims about the result
  // map, or null when no such notice is up. Distinct from `channelErrorRef`,
  // which records that the failure HAPPENED and survives a hand dismissal or a
  // single-row retest: this one records that the SENTENCE is still being shown,
  // and only that licenses re-raising it. Nulled by both ways a notice can go —
  // the helper below, and sonner's own close button through the `onDismiss` wired
  // at the raise. Kept so a verdict landing later in the same chunk can notice
  // that the pinned copy says "No key was marked" while a verdict is now on
  // screen — the map only ever grows, and a `clearResults` that empties a
  // non-empty map retires the notice, so `false` is the only value that can ever
  // go stale.
  const channelErrorNoticeVerdictsRef = useRef<boolean | null>(null)
  // Mirrors the key set of `results`, which is the thing the channel-level
  // notice's wording is a claim about. A ref rather than the state itself for two
  // reasons: reading the state would rebuild `runKeyTest` — 200 times a run —
  // through a dependency on what it writes, and this is updated in the same
  // synchronous turn as `setResults`, so a key that fails channel-level in the
  // same chunk as an earlier verdict still sees that verdict. Reading the size
  // inside a `setResults` updater could not promise that: React runs updaters
  // when it processes the queue, not necessarily at the call. Emptied only by
  // `clearResults`/`reset` and never per run, because `results` is not cleared
  // per run either.
  const resultIndexesRef = useRef(new Set<number>())
  const inFlightRef = useRef(new Map<number, MultiKeyTestRequest>())
  const keyListEpochRef = useRef(0) // reindex latch
  const progressToastIdRef = useRef<ReturnType<typeof toast.loading> | null>(
    null
  )

  const dismissProgressToast = useCallback(() => {
    if (progressToastIdRef.current === null) return

    toast.dismiss(progressToastIdRef.current)
    progressToastIdRef.current = null
  }, [])

  /**
   * Retires the rate-limit toast. It is pinned open (`duration: Infinity`), so
   * nothing expires it on its own: without this the operator carries it across a
   * dialog close and a route change, and it ends up sitting beside the progress
   * spinner of the next run. Called on unmount, at the start of any new test,
   * from a confirmed discard and from `clearResults` — deliberately not from
   * `reset()`, see the note there — so it only ever stays up while it is still
   * the current verdict.
   * Idempotent — dismissing an id sonner no longer holds is a no-op.
   */
  const dismissRateLimitToast = useCallback(() => {
    toast.dismiss(RATE_LIMIT_TOAST_ID)
    // Nothing on screen to restate any more, so nothing to re-raise either.
    rateLimitNoticeShownRef.current = false
  }, [])

  /**
   * Retires the channel-level failure toast. Pinned open for the same reason as
   * the rate-limit one and dismissed at exactly the same five points: the start
   * of a new batch run, the start of a single-row test, a confirmed discard, a
   * results clear, and unmount.
   */
  const dismissChannelErrorToast = useCallback(() => {
    toast.dismiss(MULTI_KEY_CHANNEL_ERROR_TOAST_ID)
    // Nothing on screen to correct any more, so nothing to re-raise either.
    channelErrorNoticeVerdictsRef.current = null
  }, [])

  /**
   * Dismisses the pinned rate-limit / channel-error notices. Called from the
   * discard branch of the close confirmation, and from `clearResults` only when
   * that clear actually emptied a non-empty map — both are points where the
   * verdicts a notice quotes stop existing, so a notice still quoting those
   * totals now describes nothing. `reset()` deliberately does not do this (see
   * the note there): a zero-verdict run is closed without any confirmation, and
   * there the notice is the only warning the operator got. It ends nothing: the
   * run, the results and `stopRef` are left alone.
   */
  const dismissPinnedNotices = useCallback(() => {
    dismissRateLimitToast()
    dismissChannelErrorToast()
    // Both flags are what let the settled run restate its notice, so they go too:
    // otherwise the next run end would raise the notice again over results the
    // operator has just thrown away.
    rateLimitedRef.current = false
    channelErrorRef.current = false
  }, [dismissChannelErrorToast, dismissRateLimitToast])

  /**
   * `12/200 completed · 9 succeeded, 3 failed`. Shared by the pinned progress
   * toast and the end-of-run summary so both read the same way.
   *
   * @param counts - Totals to render.
   * @returns The formatted line.
   */
  const formatTestCounts = useCallback(
    (counts: {
      completed: number
      total: number
      success: number
      failed: number
    }) =>
      `${t('{{completed}}/{{total}} completed', {
        completed: counts.completed,
        total: counts.total,
      })} · ${t('{{success}} succeeded, {{failed}} failed', {
        success: counts.success,
        failed: counts.failed,
      })}`,
    [t]
  )

  /**
   * Raised as soon as our own limiter answers, then raised again with the run
   * totals once the started chunk drains — unless the operator closed it in
   * between, which the flag below records. Both go out under one id, so the
   * operator is left with a single toast that names the cause and says how far
   * the run got, instead of a bare `stopped` count that never mentions rate
   * limiting.
   *
   * @param description - Optional totals line for the settled run.
   */
  const showRateLimitToast = useCallback(
    (description?: string) => {
      toast.error(
        t(
          'Key testing stopped: too many requests to this gateway. Wait a moment, then test the remaining keys.'
        ),
        {
          id: RATE_LIMIT_TOAST_ID,
          description,
          duration: Number.POSITIVE_INFINITY,
          // Same seam, and for the same reasons, as the channel-error notice
          // below: the app mounts sonner with its global close button
          // (`routes/__root.tsx`), which retires the toast without ever reaching
          // `dismissRateLimitToast`, and `onDismiss` fires on every removal path
          // and ONLY on removal — raising an id that is already up merges in
          // place without marking it deleted, so the end-of-run restatement
          // cannot clear the flag it is in the middle of writing.
          onDismiss: () => {
            rateLimitNoticeShownRef.current = false
          },
        }
      )
      // Recorded after the raise, matching the channel-error notice: this is the
      // sentence the operator can now read, and only a sentence still on screen
      // may be restated with the totals.
      rateLimitNoticeShownRef.current = true
    },
    [t]
  )

  /**
   * Raised when the test failed for a reason the channel owns rather than the
   * key: an unsupported `test_model`, a `ModelPriceHelper` or param-override
   * failure, a tag whitelist revoked mid-dialog. Nothing is written to the result
   * map for it, so the table shows nothing for this key and the toast has to
   * carry the whole story — that this is a channel-level problem, not a verdict
   * on any key.
   *
   * The wording turns on `hasVerdicts`, and that flag asks the RESULT MAP — the
   * thing the sentence is a claim about — never a per-run counter. An empty map
   * really does mean no key is marked. A map holding 36 verdicts from a run that
   * died on key 37, or 200 from an earlier run that this one banked nothing on,
   * is on screen behind the toast, and telling that operator "no key was marked"
   * is what sends them back to re-run, and re-pay for, every key that already
   * answered.
   *
   * @param detail - The server's explanation, whether any verdict is stored, and
   *   the run totals once they are final.
   */
  const showChannelErrorToast = useCallback(
    (detail: {
      message?: string
      hasVerdicts: boolean
      counts?: {
        completed: number
        total: number
        success: number
        failed: number
      }
    }) => {
      // Totals first and through the same formatter as the rate-limit summary, so
      // both settled runs report themselves the same way, then the server's own
      // words — the only actionable part, so it is not dropped to make room.
      // Joined only when present rather than passed through: sonner renders an
      // empty description as a blank line under the title.
      const description = [
        detail.counts ? formatTestCounts(detail.counts) : undefined,
        detail.message || undefined,
      ]
        .filter(Boolean)
        .join(' · ')

      toast.error(
        detail.hasVerdicts
          ? t(
              'Key testing stopped: the channel itself could not run the test, so this is not a verdict on any key. This key was not marked and the keys already tested keep their results — fix the channel, then test the rest.'
            )
          : t(
              'Key testing stopped: the channel itself could not run the test, so this is not a verdict on any key. No key was marked — fix the channel, then test again.'
            ),
        {
          id: MULTI_KEY_CHANNEL_ERROR_TOAST_ID,
          description: description || undefined,
          duration: Number.POSITIVE_INFINITY,
          // A notice the operator closed by hand has to stay closed. The ref
          // below is a claim about what is ON SCREEN, and the app mounts sonner
          // with its global close button (`routes/__root.tsx`), which retires the
          // toast without ever reaching `dismissChannelErrorToast` — so without
          // this callback a verdict landing later in the same chunk would
          // re-raise, at `duration: Infinity`, the toast they just dismissed.
          // `onDismiss` is the right seam because sonner calls it on every removal
          // path (close button, swipe, and the `delete` flag its programmatic
          // `toast.dismiss` sets) and ONLY on removal: raising an id that is
          // already up merges in place without marking it deleted, so a re-raise
          // cannot clear the flag it is in the middle of writing. Racing the
          // programmatic helper is harmless in either order — both write `null`.
          onDismiss: () => {
            channelErrorNoticeVerdictsRef.current = null
          },
        }
      )
      // Recorded after the raise, not before: this is the claim the operator can
      // now read, and a verdict landing later in the same chunk compares against
      // it to decide whether the sentence still holds.
      channelErrorNoticeVerdictsRef.current = detail.hasVerdicts
    },
    [formatTestCounts, t]
  )

  const abortAll = useCallback(() => {
    for (const request of inFlightRef.current.values()) {
      request.controller.abort()
    }
    inFlightRef.current.clear()
  }, [])

  const markTesting = useCallback((keyIndex: number, isTesting: boolean) => {
    setTestingIndexes((prev) => {
      const next = new Set(prev)
      if (isTesting) {
        next.add(keyIndex)
      } else {
        next.delete(keyIndex)
      }
      return next
    })
  }, [])

  /**
   * Invalidates every pending write of the current generation without touching
   * React state, so a closing dialog can drop an in-flight run before running
   * its own reset.
   */
  const abandonRun = useCallback(() => {
    runIdRef.current += 1
    stopRef.current = true
    abortAll()
  }, [abortAll])

  const reset = useCallback(() => {
    abandonRun()
    setResults({})
    resultIndexesRef.current.clear() // nothing left for a notice to call "kept"
    setTestingIndexes(() => new Set())
    setIsBatchTesting(false)
    setIsStopRequested(false)
    setProgress(null)
    dismissProgressToast()
    // The two pinned notices — rate limited, channel-level failure — are
    // deliberately NOT retired here, and must not be "tidied" back in. Closing
    // the dialog runs `resetState()` → `reset()`, and a run either of them
    // stopped before any key banked a verdict leaves the close handler with
    // `testResultCount === 0`, so it skips its discard confirmation and a single
    // ESC would destroy the only warning the operator ever got — the one thing
    // that was supposed to be un-missable. A run that did bank verdicts does get
    // that confirmation, and its discard branch calls `dismissPinnedNotices`,
    // which is why retiring them is that branch's job and not this one's. They
    // keep their five real dismissal points (start of a batch run, start of a
    // single-row test, confirmed discard, a results clear, unmount) and
    // sonner's global close button is always there for dismissing them by hand.
  }, [abandonRun, dismissProgressToast])

  /**
   * Drops every stored verdict, bumps the key-list epoch and abandons whatever
   * run is in flight. Call it after any successful delete and whenever a refetch
   * changes the loaded key count: deleting renumbers the surviving keys server
   * side, and cancellation is only checked at chunk boundaries, so without the
   * epoch the up-to-5 requests that resolve afterwards would write results
   * against the old numbering.
   *
   * The epoch on its own only orphans those verdicts, it does not end the run,
   * and orphaning without ending is the worst of both: every later `runKeyTest`
   * returns early at the epoch gate while the loop keeps firing, so a clear at
   * key 20 of 200 froze the progress panel and the pinned spinner at `20/200`,
   * spent ~180 real billed upstream calls on results it threw away, then reported
   * `20/200 completed` over a table holding nothing. So the run is abandoned
   * outright and its UI state torn down here: `testKeys` skips both its summary
   * and its `finally` teardown once the run token this bumps no longer matches.
   */
  const clearResults = useCallback(() => {
    // Read before the clear below, because whether the pinned notices are retired
    // turns on whether this call destroys anything at all.
    const hadVerdicts = resultIndexesRef.current.size > 0

    keyListEpochRef.current += 1
    resultIndexesRef.current.clear() // same reason as in `reset()`
    setResults({})
    abandonRun()
    setTestingIndexes(() => new Set())
    setIsBatchTesting(false)
    setIsStopRequested(false)
    setProgress(null)
    dismissProgressToast()
    // Invariant: a pinned notice is retired exactly when the state it describes
    // is destroyed — never before, never on a path that destroyed nothing.
    //
    // A clear over a NON-EMPTY map destroys that state. Both notices quote
    // verdict totals, the channel-error one asserting outright that "the keys
    // already tested keep their results"; pinned at `duration: Infinity` nothing
    // expires them, and the run token this bumps is also what makes `testKeys`
    // skip the end-of-run restatement that would revise them. Left up, one would
    // assert `36/200` kept results forever over a table where every row now reads
    // `Not tested`. Clearing the two re-raise flags is part of retiring them,
    // which is why this reuses the discard branch's helper.
    //
    // A clear over an ALREADY-EMPTY map destroys nothing, so it must leave them
    // alone. Our own limiter can kill a run on its very first chunk: the notice is
    // pinned with the map still empty and a `0/200 completed` description — totals
    // about the RUN, not about the map — and because nothing was banked there is
    // no refetch and no other trace, making that notice the operator's only record
    // that the gateway is throttling. Waiting the limiter out, they delete one dead
    // key; the post-delete resync lands here, and retiring the notice on that path
    // would destroy a warning about something this call never touched, leaving the
    // delete's own `Operation successful` as the last word. The channel-error
    // notice raised with zero banked verdicts is the same case. This is also why
    // `reset()` retires neither: the dialog merely closing destroys no verdicts,
    // and the throttle and the misconfiguration are both still true.
    if (hadVerdicts) {
      dismissPinnedNotices()
    }
  }, [abandonRun, dismissPinnedNotices, dismissProgressToast])

  const stop = useCallback(() => {
    stopRef.current = true
    setIsStopRequested(true)
    // Aborting is what makes Stop immediate. The per-key handler records no
    // verdict for a request we cancelled ourselves, so the keys that were mid
    // flight stay untested instead of being libelled as `no-response`.
    abortAll()
  }, [abortAll])

  /**
   * Issues the HTTP test for one key and owns that key's controller and
   * spinner.
   *
   * Concurrent callers share one request: the map holds the promise, not just
   * the controller, so a batch run that reaches a key whose single-row Test is
   * still in flight joins that request. Bailing out instead used to make the
   * run report itself `stopped` over a key that had in fact been tested, and
   * dropped the single-row verdict along with it. Each caller still applies its
   * own run and epoch latches to the shared outcome.
   *
   * @param keyIndex - Index of the key to test.
   * @returns The settled outcome, or undefined when the request was cancelled.
   */
  const requestKeyTest = useCallback(
    (keyIndex: number): Promise<ChannelTestOutcome | undefined> => {
      const joined = inFlightRef.current.get(keyIndex)
      if (joined) return joined.promise

      const controller = new AbortController()
      markTesting(keyIndex, true)

      const promise = (async () => {
        let outcome: ChannelTestOutcome | undefined
        try {
          await handleTestChannel(
            channelId,
            {
              channelName,
              keyIndex,
              // Silent on both paths: the verdict lands in the Test Result
              // column, so a 200-key run cannot bury the screen in toasts.
              silent: true,
              signal: controller.signal,
              timeout: MULTI_KEY_TEST_TIMEOUT_MS,
              // A hung request would otherwise poison the in-flight GET dedupe
              // map in `web/src/lib/http-client.ts` for every later retry.
              disableDuplicate: true,
            },
            (settled) => {
              outcome = settled
            }
          )
        } finally {
          // Retract our own entry only: `abortAll` clears the map, so a fresh
          // test of the same key may already own it by the time we settle.
          if (inFlightRef.current.get(keyIndex)?.controller === controller) {
            inFlightRef.current.delete(keyIndex)
          }
          // Deliberately not gated on the run token nor on that ownership
          // check: a leftover spinner would also keep the row permanently
          // unselectable.
          markTesting(keyIndex, false)
        }

        // A request we aborted produced no verdict at all, so the key stays
        // `Not tested` and out of reach of the bulk action bar. Writing one
        // would drop up to MULTI_KEY_TEST_CONCURRENCY keys that were never
        // really tested into the `no-response` bucket, where a bulk disable
        // could sweep up healthy keys. A timeout (`ECONNABORTED`, signal not
        // aborted) is a real verdict and does fall through, without any
        // fabricated 408.
        if (controller.signal.aborted) return undefined
        return outcome
      })()

      // Registered in the same synchronous turn as the request: the body above
      // cannot reach its `finally` before the next microtask, so the entry is
      // always in place before anything tries to retract it.
      inFlightRef.current.set(keyIndex, { controller, promise })
      return promise
    },
    [channelId, channelName, markTesting]
  )

  const runKeyTest = useCallback(
    async (
      keyIndex: number,
      runId: number,
      epoch: number
    ): Promise<ChannelTestOutcome | undefined> => {
      const result = await requestKeyTest(keyIndex)
      if (!result) return undefined

      // Both run-level detections below sit ABOVE the stale-run and reindex
      // gates on purpose, and that split is the point. Those gates exist to
      // protect the per-key WRITE at the bottom from stale data: a verdict aimed
      // at an index the server has since renumbered, or at a run nobody is
      // watching any more. A rejection from our own limiter and a channel-wide
      // failure are not verdicts on a key at all — they are facts about the
      // gateway and about the channel, and they stay true whatever happened to
      // this key's index while the request was in flight. Screened by those
      // gates (they used to be), a reindex mid-run swallowed the 429 whole: the
      // operator was never told the gateway was throttling, and the loop kept
      // firing requests into the limiter.
      //
      // Only the ref is recorded unconditionally. The UI half is gated on this
      // call still belonging to work that exists in this hook instance:
      // `runningRef` covers the batch loop for as long as it is executing, and
      // the run-token match covers the single-row Test button, which runs with no
      // batch loop at all. What it does NOT do is screen out a run abandoned
      // while draining — `runningRef` stays true straight through `abandonRun`
      // and is only cleared by `testKeys`'s `finally` — and it does not have to:
      // `abandonRun` aborts every controller, and an aborted request returns
      // above without ever reaching here.
      const isRunLive = runningRef.current || runId === runIdRef.current

      if (isGatewayRateLimited(result)) {
        // No bucket for this key: it stays `Not tested`, out of reach of the
        // bulk actions. Stop the run and wait for the operator, never resume.
        // The ref is flipped before the state update because the progress toast
        // effect reads it: without that gate the keys still draining would
        // recreate the toast this dismisses, as a second pinned spinner.
        rateLimitedRef.current = true
        if (isRunLive) {
          stopRef.current = true
          setIsStopRequested(true)
          dismissProgressToast()
          showRateLimitToast()
        }
        return undefined
      }

      // A failure the channel owns, not the key: an unsupported `test_model`, a
      // price-map or param-override failure, a tag whitelist revoked mid-dialog.
      // All of those answer `{success:false}` with no `upstream_status_code`,
      // which is indistinguishable from a dead key once it is in the result map
      // — every key of a healthy channel would land in the selectable `No
      // response (local error)` bucket, with a disable reason pre-filled, and one
      // bulk action would take the whole channel down. So no verdict is written
      // and the run stops: these codes are deterministic, the next key answers
      // identically, and carrying on would only spend the gateway's rate-limit
      // budget for nothing. `success` is tested first so a passing test can never
      // be read as one of these, whatever the predicate makes of successes.
      if (!result.success && !isKeyAttributableFailure(result)) {
        channelErrorRef.current = true
        channelErrorMessageRef.current = result.error
        if (isRunLive) {
          stopRef.current = true
          setIsStopRequested(true)
          dismissProgressToast()
          // Raised here without totals, exactly as the 429 above is: the started
          // chunk is still draining, so the loop has no final counts yet and
          // restates this same notice with them once it does. The wording is
          // already right either way — the mirror ref carries the live size of the
          // result map, earlier runs' verdicts included, and that map is what the
          // sentence is a claim about.
          showChannelErrorToast({
            message: result.error,
            hasVerdicts: resultIndexesRef.current.size > 0,
          })
        }
        return undefined
      }

      if (runId !== runIdRef.current) return undefined
      if (epoch !== keyListEpochRef.current) return undefined

      // Mirror written in the same synchronous turn as the state, and before it,
      // so a channel-level failure settling later in this very chunk reads a size
      // that already counts this verdict.
      resultIndexesRef.current.add(keyIndex)
      setResults((prev) => ({ ...prev, [keyIndex]: result }))

      // The mid-run channel-error notice read the map size ONCE, and within a
      // single chunk that size goes from 0 to non-zero after the raise: key 2
      // fails non-attributably while the map is empty, key 0 answers 401 two
      // seconds later, and key 4 sits on the 120s per-key ceiling — so the
      // end-of-run restatement that would flip the wording is ~117s away while
      // the pinned copy claims no key was marked over a 401 that is on screen.
      // Re-raised under the same id so it corrects itself in place instead of
      // stacking, and through the same helper so the copy selection stays in one
      // place. Only `false` is a stale claim (the map only grows, and the one
      // thing that empties it retires the notice), so this fires at most once per
      // notice and never at all in a healthy run, where no notice is up.
      if (channelErrorNoticeVerdictsRef.current === false) {
        showChannelErrorToast({
          message: channelErrorMessageRef.current,
          hasVerdicts: resultIndexesRef.current.size > 0,
        })
      }
      return result
    },
    [
      dismissProgressToast,
      requestKeyTest,
      showChannelErrorToast,
      showRateLimitToast,
    ]
  )

  /** Single-row Test button. Shares the executor, and so the spinner, with the batch loop. */
  const testKey = useCallback(
    async (keyIndex: number): Promise<void> => {
      // The operator answered the pinned notices by testing again, so neither is
      // the current state of affairs any more. A still-limited gateway or a still
      // broken channel raises its own afresh from `runKeyTest`.
      dismissRateLimitToast()
      dismissChannelErrorToast()
      await runKeyTest(keyIndex, runIdRef.current, keyListEpochRef.current)
    },
    [dismissChannelErrorToast, dismissRateLimitToast, runKeyTest]
  )

  const testKeys = useCallback(
    async (keyIndexes: number[]): Promise<void> => {
      // Test all and Test selected are two triggers over one shared stopRef,
      // progress and toast. A second concurrent run would let whichever
      // finishes first tear those down under the other, turning Stop into a
      // no-op while the survivor keeps spending money with no UI at all.
      if (runningRef.current) return

      const targets = [...new Set(keyIndexes)]
        .filter((keyIndex) => keyIndex >= 0)
        .sort((a, b) => a - b)
      if (targets.length === 0) return

      runningRef.current = true
      const runId = ++runIdRef.current
      const epoch = keyListEpochRef.current
      // Reset at run start as well as in `finally`: the dialog content instance
      // is keyed by channel id, so reopening the same channel reuses it and a
      // leftover stopRef would break the loop on its very first chunk.
      stopRef.current = false
      rateLimitedRef.current = false
      channelErrorRef.current = false
      // Deliberately not the result-map mirror: `results` is not cleared between
      // runs, so a run that banks nothing still has the previous run's verdicts on
      // screen and must never be told that no key was marked.

      // The previous run's pinned notices describe a run that is over. Dismissed
      // here rather than left to be updated in place, so neither can outlive its
      // run nor sit next to this run's progress spinner.
      dismissRateLimitToast()
      dismissChannelErrorToast()
      setIsStopRequested(false)
      setIsBatchTesting(true)
      setProgress({
        total: targets.length,
        completed: 0,
        success: 0,
        failed: 0,
        startedAt: Date.now(),
      })

      let completedCount = 0
      let successCount = 0
      let failedCount = 0

      try {
        for (
          let startIndex = 0;
          startIndex < targets.length;
          startIndex += MULTI_KEY_TEST_CONCURRENCY
        ) {
          if (stopRef.current) break

          const chunk = targets.slice(
            startIndex,
            startIndex + MULTI_KEY_TEST_CONCURRENCY
          )
          await Promise.allSettled(
            chunk.map(async (keyIndex) => {
              const outcome = await runKeyTest(keyIndex, runId, epoch)
              // No outcome means aborted, stale, or rejected by our own rate
              // limiter; none of those is a completed test.
              if (!outcome) return

              completedCount += 1
              if (outcome.success) {
                successCount += 1
              } else {
                failedCount += 1
              }

              if (runId !== runIdRef.current) return
              setProgress((prev) =>
                prev
                  ? {
                      ...prev,
                      completed: completedCount,
                      success: successCount,
                      failed: failedCount,
                    }
                  : prev
              )
            })
          )

          if (
            stopRef.current ||
            startIndex + MULTI_KEY_TEST_CONCURRENCY >= targets.length
          ) {
            break
          }

          await sleep(MULTI_KEY_TEST_DELAY_MS)
        }

        if (runId !== runIdRef.current) return

        dismissProgressToast()
        if (channelErrorRef.current && !rateLimitedRef.current) {
          // Restated, not summarised separately: the notice raised mid-run is
          // still pinned open, so the totals go into it under its own id and the
          // run reports them exactly once — a second `Batch test stopped` toast
          // beside it would say the same thing twice and cost the notice its
          // point, which is that these counts are not a verdict on the key that
          // stopped the run. Rate limiting still wins when both fired inside the
          // same chunk, because waiting the limiter out is what the operator has
          // to do before any of this can be retried; that branch retires this
          // notice so the surviving pair can never contradict each other.
          //
          // The two numbers read here legitimately disagree, so they come from
          // different places. WHICH sentence is a claim about the result map, and
          // no run clears that map: a re-run of a channel that has since broken
          // banks nothing while 200 verdicts sit in the table behind the toast.
          // The DESCRIPTION's totals are this run's own, and are withheld when
          // this run banked nothing — `0/200 completed` would only dress up a run
          // that marked nothing at all.
          //
          // Restated only while that notice is actually still on screen. The two
          // refs differ exactly here: `channelErrorRef` records that the failure
          // HAPPENED and nothing on a hand dismissal clears it, while the
          // on-screen ref is nulled by the `onDismiss` wired at the raise, so it
          // is the only one that can tell "still pinned" from "the operator
          // closed it". Without this check the up-to-4 keys still draining after
          // a stop on key 3 take up to MULTI_KEY_TEST_TIMEOUT_MS to settle, and
          // this block re-raises at `duration: Infinity` the very notice they
          // closed two minutes earlier, with no action of theirs — the same bug
          // the mid-run correction already fixed, on the other path.
          //
          // Nothing is raised in its place, deliberately: the plain `Batch test
          // stopped` summary further down announces the same stoppage off the
          // same trigger, so falling through to it would be the same ambush in
          // shorter words. The cost is that this run's completed/total are then
          // never reported anywhere — accepted, because the operator closed the
          // one notice this run had to carry them, having already read its cause,
          // and every verdict those totals count is still on the rows behind the
          // toast.
          if (channelErrorNoticeVerdictsRef.current === null) return

          const hasStoredVerdicts = resultIndexesRef.current.size > 0
          const hasRunVerdicts = completedCount > 0
          showChannelErrorToast({
            message: channelErrorMessageRef.current,
            hasVerdicts: hasStoredVerdicts,
            counts: hasRunVerdicts
              ? {
                  completed: completedCount,
                  total: targets.length,
                  success: successCount,
                  failed: failedCount,
                }
              : undefined,
          })
          return
        }
        if (rateLimitedRef.current) {
          // Our own limiter, not the operator, ended this run: update the toast
          // raised mid-run with the totals rather than adding a second one that
          // only says `stopped`.
          //
          // Invariant enforced here: at most one pinned notice outlives a run, and
          // its copy is never contradicted by the run's final state. Both notices
          // can fire inside a single chunk — key 1 rejected by our limiter, key 2
          // answering with a channel-level failure — and this branch is the one
          // that runs when they do, so the channel-error notice has to be retired:
          // pinned at `duration: Infinity` nothing expires it, and it would sit
          // there forever with its uncorrected mid-run copy next to this toast's
          // real totals. It is the one dropped rather than restated because it
          // raises itself again on the retry the operator has to wait for anyway.
          //
          // The same on-screen check as the channel-error branch above, and for
          // the same reason: a notice the operator closed by hand stays closed,
          // and the drain that outlives the stop is what would otherwise reopen
          // it. The retirement below moves under the guard with it — retiring
          // the channel-error notice is only ever how this pair is kept from
          // contradicting each other, and with nothing raised here there is
          // nothing left to contradict, so wiping it would destroy a second,
          // still-accurate notice the operator never dismissed. In that case it
          // is the surviving pinned notice and its mid-run copy is already
          // correct, the wording having self-corrected as verdicts landed.
          if (!rateLimitNoticeShownRef.current) return

          dismissChannelErrorToast()
          showRateLimitToast(
            formatTestCounts({
              completed: completedCount,
              total: targets.length,
              success: successCount,
              failed: failedCount,
            })
          )
        } else if (completedCount < targets.length) {
          toast.info(
            t(
              'Batch test stopped: {{completed}}/{{total}} completed, {{success}} succeeded, {{failed}} failed',
              {
                completed: completedCount,
                total: targets.length,
                success: successCount,
                failed: failedCount,
              }
            )
          )
        } else if (failedCount > 0) {
          toast.error(
            t(
              'Batch test completed: {{success}} succeeded, {{failed}} failed',
              {
                success: successCount,
                failed: failedCount,
              }
            )
          )
        } else {
          toast.success(
            t('Batch test completed: {{count}} succeeded', {
              count: successCount,
            })
          )
        }
      } finally {
        if (runId === runIdRef.current) {
          stopRef.current = false
          setIsBatchTesting(false)
          setIsStopRequested(false)
          setProgress(null)
          // Exactly one invalidation per run, cancelled runs included, never
          // one per key. Nothing to refresh when no test ever landed.
          if (completedCount > 0) {
            queryClient.invalidateQueries({
              queryKey: channelsQueryKeys.lists(),
            })
            onRunSettled?.()
          }
        }
        runningRef.current = false
      }
    },
    [
      dismissChannelErrorToast,
      dismissProgressToast,
      dismissRateLimitToast,
      formatTestCounts,
      onRunSettled,
      queryClient,
      runKeyTest,
      showChannelErrorToast,
      showRateLimitToast,
      t,
    ]
  )

  // The global Toaster sets duration={5000}, while a per-key test is a real
  // generation call that routinely takes longer than that, so the progress
  // toast has to be pinned open explicitly.
  useEffect(() => {
    // Both stop refs are read, not depended on: this effect only ever re-runs
    // when `progress` or `isStopRequested` change, and the 429 and channel-level
    // handlers flip their ref before touching either. Without the gate, the keys
    // still draining after such a stop would recreate the progress toast those
    // handlers just dismissed — as a brand-new pinned spinner, since dismissing
    // also clears the id this call would otherwise update in place.
    if (!progress || rateLimitedRef.current || channelErrorRef.current) {
      dismissProgressToast()
      return
    }

    const title = isStopRequested
      ? t('Stopping batch test...')
      : t('Batch testing keys...')

    progressToastIdRef.current = toast.loading(title, {
      id: progressToastIdRef.current ?? undefined,
      description: formatTestCounts(progress),
      duration: Number.POSITIVE_INFINITY,
    })
  }, [dismissProgressToast, formatTestCounts, isStopRequested, progress, t])

  // Switching channels remounts the dialog content; it must not leave requests
  // in flight or a pinned toast behind. All three toasts go, the two pinned
  // notices included: this is their last dismissal point, and they would
  // otherwise survive the dialog and follow the operator onto the next page with
  // no run left to explain them. A plain close does not come through here — the
  // content instance is keyed by channel id and reused — which is exactly why
  // `reset()` leaves them alone.
  useEffect(() => {
    return () => {
      abandonRun()
      dismissProgressToast()
      dismissRateLimitToast()
      dismissChannelErrorToast()
    }
  }, [
    abandonRun,
    dismissChannelErrorToast,
    dismissProgressToast,
    dismissRateLimitToast,
  ])

  return useMemo(
    () => ({
      results,
      testingIndexes,
      isBatchTesting,
      isStopRequested,
      progress,
      testKey,
      testKeys,
      stop,
      abandonRun,
      clearResults,
      dismissPinnedNotices,
      reset,
    }),
    [
      abandonRun,
      clearResults,
      dismissPinnedNotices,
      isBatchTesting,
      isStopRequested,
      progress,
      reset,
      results,
      stop,
      testKey,
      testKeys,
      testingIndexes,
    ]
  )
}
