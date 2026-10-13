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
  keepPreviousData,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import type { ColumnDef, Row, RowSelectionState } from '@tanstack/react-table'
import type { TFunction } from 'i18next'
import {
  Check,
  Copy,
  Gauge,
  Info,
  Power,
  PowerOff,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  DataTablePagination,
  DataTableView,
  useDataTable,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Spinner } from '@/components/ui/spinner'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useIsMobile } from '@/hooks/use-mobile'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError, getServerMessage } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

import {
  deleteDisabledMultiKeys,
  deleteKeys,
  deleteMultiKey,
  disableAllMultiKeys,
  disableKeys,
  disableMultiKey,
  enableAllMultiKeys,
  enableKeys,
  enableMultiKey,
  getMultiKeyStatus,
} from '../../api'
import { MULTI_KEY_FILTER_OPTIONS, MULTI_KEY_STATUS } from '../../constants'
import {
  useMultiKeyBatchTest,
  type MultiKeyTestResultMap,
} from '../../hooks/use-multi-key-batch-test'
import {
  channelsQueryKeys,
  compareTestBuckets,
  formatTimestamp,
  getFailureStatusDisplay,
  getFirstErrorLine,
  getMultiKeyConfirmMessage,
  getMultiKeyStatusConfig,
  getTestBucketLabel,
  getTestBucketValue,
  isDestructiveAction,
  MULTI_KEY_REASON_MAX_LENGTH,
  normalizeInlineError,
  resolveTestBucket,
  type ChannelTestOutcome,
  type TestBucket,
  type TestBucketOption,
} from '../../lib'
import type {
  Channel,
  KeyStatus,
  MultiKeyBatchCounts,
  MultiKeyBatchErrorCode,
  MultiKeyConfirmAction,
} from '../../types'
import { useChannels } from '../channels-provider'
import { MultiKeyBulkActions } from './multi-key-bulk-actions'
import { StatisticsCard } from './multi-key-statistics-card'
import { MultiKeyTableRowActions } from './multi-key-table-row-actions'

/** Hard ceiling on the pagination loop; the reason field carries whole upstream error bodies. */
const MULTI_KEY_MAX_LOADED_KEYS = 5000
/** The server clamps `page_size` to 500, so asking for more only wastes a round trip. */
const MULTI_KEY_STATUS_PAGE_SIZE = 500
/**
 * How many times `fetchMultiKeyStatus` may start its page walk over. One attempt
 * is one full pass from page 1 to wherever the walk ends; a pass is abandoned and
 * a new one started the moment the server's channel-wide `total` disagrees with
 * the one pinned on page 1, because from that point the accumulated array splices
 * two different numberings together.
 *
 * A bounded retry rather than a flag on the returned snapshot: a flag only works
 * if every present and future consumer honours it, and this dialog has already
 * shipped nine rounds of consumers that read the key indexes directly. Retrying
 * produces a snapshot that is simply correct, and one concurrent write during a
 * multi-second walk — overwhelmingly the common case — is fixed outright by the
 * first restart. Three is enough that a genuinely quiet channel never sees the
 * error path.
 *
 * WHAT THREE ACTUALLY COSTS, and the cross-file coupling that makes it true.
 * Three attempts times the page budget (`MULTI_KEY_MAX_LOADED_KEYS` /
 * `MULTI_KEY_STATUS_PAGE_SIZE`, ten pages today) is up to THIRTY sequential POSTs
 * per queryFn invocation, each one rebuilding the channel's whole KeyStatus list
 * server side and serialising up to a page of rows whose `reason` can carry an
 * entire upstream error body — the very cost the server's page-size clamp exists
 * to bound.
 *
 * That is the whole bound only because the query overrides react-query's `retry`
 * (see `useQuery` below) to refuse a `MULTI_KEY_WALK_UNSTABLE` verdict. Under the
 * global policy in `src/main.tsx` this constant is silently multiplied: PROD
 * returns false only once `failureCount > 3`, so failure counts 0, 1, 2 and 3 all
 * retry and the queryFn runs FIVE times — 150 POSTs plus roughly fifteen seconds
 * of exponential backoff, all behind one undifferentiated spinner, to re-learn
 * what three attempts already established. The number here and that override are
 * one decision in two files: change either and re-derive the other.
 */
const MULTI_KEY_STATUS_MAX_WALK_ATTEMPTS = 3
/**
 * Thrown by `fetchMultiKeyStatus` when it cannot produce ONE coherent snapshot.
 * A sentinel rather than copy because it travels as `Error.message` through
 * react-query, and the load-error toast maps it to real text — see the effect
 * next to `expectedKeyCount`. Nothing else may render it.
 */
const MULTI_KEY_WALK_UNSTABLE = 'multi-key-status-walk-unstable'
/** Above this many selected keys, "Test selected" asks for confirmation first. */
const TEST_SELECTED_CONFIRM_THRESHOLD = 20
/**
 * Below this many identically-failing keys the coincidence is not evidence of
 * anything: two keys bought from the same reseller really can be revoked
 * together, and warning about it would train the operator to click past it.
 */
const SHARED_NO_RESPONSE_MIN_KEYS = 3
const ALL_BUCKETS = 'all'
const ALL_STATUSES = 'all'
const DEFAULT_PAGINATION = { pageIndex: 0, pageSize: 10 }
/** Stable identity so the memos below do not recompute while the query has no data. */
const EMPTY_KEYS: KeyStatus[] = []

type MultiKeyStatusSnapshot = {
  keys: KeyStatus[]
  /**
   * Every key the channel has. The server computes its `total` *after* the
   * status filter, but `fetchMultiKeyStatus` never sends one, so here it is
   * always the channel-wide count. That makes it the only correct denominator
   * for the three counters below, which the server increments over the whole
   * key list ("Overall statistics" in `controller/channel.go`) rather than over
   * the page or the prefix this dialog managed to load.
   */
  channelKeyCount: number
  enabledCount: number
  manualDisabledCount: number
  autoDisabledCount: number
  /**
   * True when our own load ceiling stopped the walk short of the server total.
   * The loaded keys are a prefix of the real list and no refresh can change
   * that, so bulk `delete_keys` is held back client side. Not because the
   * server would refuse it: `expected_key_count` carries the channel-wide COUNT
   * the server itself reported, so the precondition matches and the delete
   * would land — which is exactly why this flag is the only thing standing
   * between an irreversible batch and a snapshot that provably omits keys.
   */
  truncatedByCap: boolean
  /**
   * True when the server ran out of pages while its own total kept climbing:
   * keys were appended between our page requests. Distinct from
   * `truncatedByCap` because the snapshot is merely stale, not clipped — a
   * refresh loads the rest, so nothing needs to be disabled for it.
   *
   * NEAR-UNREACHABLE, AND DELIBERATELY KEPT. Since the walk pins `total` on page
   * 1 (see `fetchMultiKeyStatus`) and treats a RISE exactly like a fall, the case
   * this flag was written for — a plain append landing mid-walk — no longer gets
   * here: it moves `total` between two pages, the pin catches that and answers
   * with a restart, and the attempt that finally succeeds saw one list. What is
   * left is the bad-value case only: a `total_pages` that runs out while an
   * unmoving `total` still says there is more. That is the same shape of problem
   * and takes the same advice, so the flag and its Refresh banner stay exactly as
   * they are. Do not delete them as dead code — a user-facing string costs a new
   * key across seven locales to reinstate, and the flag is genuinely reachable,
   * just rarely.
   */
  grewDuringLoad: boolean
  fetchedAt: number
}

/**
 * Loads every key of one channel by walking the paginated endpoint.
 *
 * The walk is several sequential requests on any channel above
 * `MULTI_KEY_STATUS_PAGE_SIZE`, and the server renumbers every surviving key
 * when one is deleted, so the pages have to be proven to describe ONE list
 * before they can be handed on as a snapshot. Two things do that: `total` is
 * pinned on the first page and every later page must repeat it, and the
 * collected indexes must come back contiguous from 0. Neither can be satisfied
 * with a warning flag — the caller turns this snapshot into `expected_key_count`
 * and into row indexes for irreversible deletes — so a walk that cannot be
 * proven coherent throws instead of returning.
 *
 * @param channelId - Channel whose keys are loaded.
 * @param signal - react-query's per-fetch AbortSignal. What threading it buys,
 *   stated precisely because the obvious claim is the wrong one: `refetch()`
 *   cancels the running fetch by default, but that only reaches the in-flight
 *   request when the queryFn actually READ `context.signal`
 *   (`#abortSignalConsumed`, see `@tanstack/query-core/build/modern/query.js`).
 *   So this is what stops a superseded walk instead of leaving two of them
 *   racing — which matters because `resyncAfterDelete` refetches while a walk may
 *   still be in flight. It does NOT make closing the dialog stop the walk:
 *   closing sets `open` false but never unmounts `MultiKeyManageDialogContent`
 *   (the wrapper returns null only when no channel is selected), so
 *   `removeObserver` never runs and the walk keeps issuing page requests at a
 *   dialog nobody is looking at. Cancelling on close would need an explicit
 *   `queryClient.cancelQueries` from `resetState()`; that is not done here.
 * @returns One snapshot covering all loaded keys plus the channel-wide counters.
 * @throws `MULTI_KEY_WALK_UNSTABLE` when no attempt produced a coherent snapshot.
 */
async function fetchMultiKeyStatus(
  channelId: number,
  signal?: AbortSignal
): Promise<MultiKeyStatusSnapshot> {
  const maxPages = Math.ceil(
    MULTI_KEY_MAX_LOADED_KEYS / MULTI_KEY_STATUS_PAGE_SIZE
  )

  for (
    let attempt = 1;
    attempt <= MULTI_KEY_STATUS_MAX_WALK_ATTEMPTS;
    attempt += 1
  ) {
    // Every accumulator is per-attempt: a restart must not inherit one key, one
    // counter or one page number from the numbering it is abandoning.
    const keys: KeyStatus[] = []
    /** The channel-wide `total` of page 1; every later page must repeat it. */
    let pinnedTotal: number | null = null
    let enabledCount = 0
    let manualDisabledCount = 0
    let autoDisabledCount = 0
    let fetchedPages = 0
    let listMoved = false

    for (let page = 1; page <= maxPages; page += 1) {
      // Checked before every page, and before every restart below, because the
      // walk is a loop the transport cannot see out of: axios aborts the request
      // that is open, but nothing stops this loop from opening the next nine.
      // What it throws never reaches the UI — react-query only aborts as part of
      // cancelling, and a cancelled retryer has already rejected its promise with
      // a CancelledError, so these settle a promise nobody is left holding.
      signal?.throwIfAborted()
      const response = await getMultiKeyStatus(
        channelId,
        page,
        MULTI_KEY_STATUS_PAGE_SIZE,
        // No status filter: `total` has to stay the channel-wide count, because
        // it is what the pin, `expected_key_count` and the statistics all mean.
        undefined,
        { signal }
      )
      // getMultiKeyStatus goes through channelActionConfig() (skipBusinessError +
      // skipErrorHandler), so a rejected request resolves as data instead of
      // throwing. Rethrowing here is what keeps the query in the error state;
      // without it a permission denial would be cached as an empty key list.
      if (!response.success || !response.data) {
        throw createServerError(response)
      }

      const data = response.data
      const pageKeys = data.keys ?? []
      // Same fallback the walk has always used — with no `total` from the server
      // there is nothing to claim beyond the keys in hand — but read per page so
      // it can be compared, never accumulated into the pinned value.
      const pageTotal = data.total ?? keys.length + pageKeys.length

      if (pinnedTotal === null) {
        pinnedTotal = pageTotal
      } else if (pageTotal !== pinnedTotal) {
        // The channel-wide count moved between two pages of one walk. The pages
        // already collected were numbered under the old list and everything
        // still to come under the new one, so the array in hand is a splice of
        // two numberings and cannot be repaired, only rebuilt. This is the case
        // the old code could not see when the count went DOWN: it re-read
        // `channelKeyCount` from every page, so a shrink lowered the very number
        // the finished walk was then checked against and both warning flags came
        // out false. Note that a rise is treated exactly the same, even though an
        // append on its own renumbers nothing: a count is not able to tell one
        // append from one delete plus two appends.
        listMoved = true
        break
      }

      fetchedPages = page
      keys.push(...pageKeys)
      enabledCount = data.enabled_count ?? 0
      manualDisabledCount = data.manual_disabled_count ?? 0
      autoDisabledCount = data.auto_disabled_count ?? 0

      if (keys.length >= pinnedTotal) break
      if (keys.length >= MULTI_KEY_MAX_LOADED_KEYS) break
      // total_pages is the bad-value guard: a wrong total must not loop forever.
      if (page >= (data.total_pages ?? page)) break
    }

    // Throw the splice away and walk the whole list again from page 1. Another
    // whole pass is the most expensive thing this function can decide to do, so
    // the abort check comes first.
    if (listMoved) {
      signal?.throwIfAborted()
      continue
    }

    if (pinnedTotal === null) {
      // Unreachable while `maxPages >= 1`: the page loop either pinned a total or
      // threw. A throw rather than a `?? keys.length` fallback so that a future
      // change to the page budget cannot quietly ship an unpinned snapshot, which
      // is the exact class of bug this function now exists to refuse.
      throw new Error(MULTI_KEY_WALK_UNSTABLE)
    }

    // Integrity assertion, kept as defence in depth rather than against a threat
    // anyone can name. The server numbers `Index` by position over its own
    // contiguous key slice and pages it in order (`controller/channel.go`
    // :1800), and this walk sends no status filter, so a walk that saw a single
    // numbering must come back with key i at position i — in the complete case
    // and in the cap-truncated one alike, since 5000 of 6000 keys is still the
    // prefix 0..4999.
    //
    // An earlier round justified this by the server's `page > total_pages`
    // clamp, which re-serves the LAST page and would put the same indexes in the
    // array twice. Be honest: the walk cannot reach that clamp. `total_pages`
    // comes out of the same response as `total`, and the loop stops on whichever
    // of the two runs out first, so any change big enough to make the client
    // over-request has already moved `total` — and the pin fires a restart one
    // page earlier. What survives is the residual class this dialog can never
    // close on its own: a server that renumbers by something other than
    // position, a future status filter added to this call, a paging bug. None of
    // those is hypothetical enough to hand un-asserted indexes to an irreversible
    // delete, and positional equality costs one pass and no second structure —
    // strictly stronger than collecting the indexes into a Set and comparing it
    // to {0..n-1}, for the same money.
    for (let position = 0; position < keys.length; position += 1) {
      if (keys[position]?.index !== position) {
        // Deliberately not a restart: every page of this attempt already agreed
        // on `total`, so on this channel the count has just proven blind and
        // walking again would be retrying against a signal that does not work.
        throw new Error(MULTI_KEY_WALK_UNSTABLE)
      }
    }

    // Everything below only matters when the walk ended short of the server's own
    // total; a channel of any size under the cap and without a concurrent write
    // leaves both flags false. `pinnedTotal`, never a per-page total: it is the
    // count the rows below were NUMBERED under, and it is what the caller sends
    // as `expected_key_count`, so the two must describe the same list.
    const isShortOfTotal = keys.length < pinnedTotal
    // Our own ceiling ended the walk — either the key cap or the page budget
    // derived from it. Nothing the operator does can complete this list.
    const hitLoadCeiling =
      keys.length >= MULTI_KEY_MAX_LOADED_KEYS || fetchedPages >= maxPages

    return {
      keys,
      channelKeyCount: pinnedTotal,
      enabledCount,
      manualDisabledCount,
      autoDisabledCount,
      truncatedByCap: isShortOfTotal && hitLoadCeiling,
      // The server said "no more pages" while reporting a total we had not
      // reached: the list grew under us mid-pagination. Loading again picks the
      // new keys up, so this must not be reported as a hard truncation.
      grewDuringLoad: isShortOfTotal && !hitLoadCeiling,
      fetchedAt: Date.now(),
    }
  }

  // Every attempt was abandoned mid-walk. Returning a flagged snapshot here would
  // put a list that is provably a splice of two numberings in front of the
  // operator and trust every consumer to notice the flag; throwing leaves the
  // query in its error state, which is the only state in which no wrong write is
  // reachable at all.
  throw new Error(MULTI_KEY_WALK_UNSTABLE)
}

/**
 * Bucket of a stored verdict. `resolveTestBucket` can only answer `testing` when
 * it is told the key is in flight, which the dropdown counter, the row filter and
 * the copy action deliberately never do: a key being retested has to keep its
 * previous bucket instead of vanishing from the list under the cursor.
 *
 * @param result - The stored outcome, if the key was ever tested.
 * @returns The bucket, narrowed to the ones that can back a dropdown option.
 */
function resolveStoredBucket(result?: ChannelTestOutcome): TestBucketOption {
  const bucket = resolveTestBucket({ result })
  return bucket.kind === 'testing' ? { kind: 'untested' } : bucket
}

function getMultiKeyColumnClass(columnId: string) {
  switch (columnId) {
    case 'select':
      return 'w-11 text-center align-middle'
    case 'index':
      return 'w-24 min-w-24 whitespace-nowrap'
    case 'status':
      return 'w-36 min-w-36 whitespace-nowrap'
    case 'test-result':
      return 'w-80 min-w-80 max-w-80 whitespace-normal'
    case 'actions':
      return 'w-64 min-w-64 text-right whitespace-nowrap'
    case 'reason':
      return 'min-w-[200px] max-w-xs whitespace-normal'
    case 'disabled-time':
      return 'w-44 min-w-44 whitespace-nowrap'
    default:
      return undefined
  }
}

/**
 * Disabled reason recorded for a key whose test landed in `bucket`. The strings
 * are persisted verbatim by the server and read straight back into the Disabled
 * Reason column, so they are translated in the admin's own locale.
 *
 * @param bucket - Bucket the key's verdict falls into.
 * @param t - Translate function of the caller.
 * @returns The reason to store.
 */
function getDisableReasonForBucket(
  bucket: TestBucketOption,
  t: TFunction
): string {
  if (bucket.kind === 'http-error') {
    return t('Disabled by admin after key test: HTTP {{code}}', {
      code: bucket.code,
    })
  }
  if (bucket.kind === 'no-response') {
    return t('Disabled by admin after key test: no upstream response')
  }
  return t('Disabled by admin')
}

/**
 * Default disabled reason offered for a bulk disable. A selection that sits in a
 * single test bucket gets the diagnosis spelled out; a mixed selection falls back
 * to the generic reason, because one string is stored for every selected key.
 *
 * @param rows - Currently selected rows.
 * @param results - Stored per-key verdicts.
 * @param t - Translate function of the caller.
 * @returns The reason to pre-fill in the confirmation input.
 */
function getDefaultDisableReason(
  rows: Row<KeyStatus>[],
  results: MultiKeyTestResultMap,
  t: TFunction
): string {
  const values = new Set<string>()
  let bucket: TestBucketOption | null = null
  for (const row of rows) {
    const resolved = resolveStoredBucket(results[row.original.index])
    values.add(getTestBucketValue(resolved))
    bucket = resolved
  }

  if (values.size !== 1 || !bucket) return t('Disabled by admin')
  return getDisableReasonForBucket(bucket, t)
}

/**
 * The one error message every selected key came back with, when the selection
 * looks like a channel-wide fault misreported as N per-key verdicts.
 *
 * `key_attempted` is raised at the *entry* to `adaptor.DoRequest`, so a
 * deterministic channel-level failure thrown inside it but before any network
 * I/O is still reported as a verdict on the key: a `base_url` pasted with a
 * stray newline dies in `http.NewRequest`, a Vertex API-key channel tested
 * against a `-maas` model dies in `getRequestUrl`, `advancedcustom`'s converter
 * can die the same way. Every key of the channel then lands in `no-response`
 * carrying byte-identical copy, and one bulk Disable retires N keys that were
 * never dispatched. Distinguishing the two for real needs the relay layer to
 * report whether I/O began; until it does, this only hands the confirmation the
 * evidence so the operator decides knowing what they are looking at.
 *
 * @param rows - Currently selected rows.
 * @param results - Stored per-key verdicts.
 * @returns The shared message, or undefined when the selection does not fit the pattern.
 */
function getSharedNoResponseError(
  rows: Row<KeyStatus>[],
  results: MultiKeyTestResultMap
): string | undefined {
  if (rows.length < SHARED_NO_RESPONSE_MIN_KEYS) return undefined

  let shared: string | undefined
  for (const row of rows) {
    const result = results[row.original.index]
    // An untested key in the selection is enough to lose the pattern: the run it
    // would belong to is the one that could have proven the fault channel-wide.
    if (!result) return undefined
    if (resolveStoredBucket(result).kind !== 'no-response') return undefined

    const message = result.error?.trim()
    if (!message) return undefined
    if (shared === undefined) {
      shared = message
    } else if (shared !== message) {
      return undefined
    }
  }
  return shared
}

/**
 * One key's failure as rendered by the on-demand details sheet. Captured at
 * click time rather than read back from the result map, so scrolling or a
 * refetch cannot swap the body out from under the reader.
 */
type KeyErrorDetail = {
  keyIndex: number
  summary: string
  /** The whole upstream body, verbatim, newlines and all */
  details: string
}

/**
 * Translated copy for a rejected key action.
 *
 * The multi-key actions answer HTTP 200 with `success: false` plus a stable
 * `error_code` (`controller/channel.go:1863-1871`) and a Chinese `message` that
 * is only meant as a fallback for non-UI clients, so every code the dialog can
 * hit is spelled out here. `message` is reached only by an unknown code, or by
 * the four actions whose own failures still answer without one at all:
 * `enable_key`, `delete_key`, `enable_all_keys` and `disable_all_keys` — the
 * first two do answer `key_list_changed` now that they carry the precondition.
 *
 * @param response - The rejected response.
 * @param t - Translate function of the caller.
 * @param options - `isDeleteAction` picks the `key_list_changed` wording: only a
 *   delete can be reassured with "nothing was deleted", and telling an operator
 *   who pressed Enable that a delete was refused — on a screen where Delete sits
 *   next to Enable — reads as if the wrong button fired.
 * @returns The message to put in the error toast.
 */
function getMultiKeyFailureMessage(
  response: {
    error_code?: MultiKeyBatchErrorCode
    message?: string
    data?: MultiKeyBatchCounts
  },
  t: TFunction,
  options: { isDeleteAction: boolean }
): string {
  switch (response.error_code) {
    case 'key_indexes_empty':
      return t('No keys were selected. Select at least one key and try again.')
    case 'key_index_out_of_range':
      // The whole batch is rejected on the first bad index, so nothing was
      // written — same guarantee as key_list_changed, different trigger. Only
      // the delete path reloads the list by itself, hence the plain ask here.
      return t(
        'Some of the selected keys no longer exist, so nothing was changed. Refresh the key list and try again.'
      )
    case 'no_keys_to_disable':
      return t('All {{skipped}} selected keys are already disabled.', {
        skipped: response.data?.skipped_count ?? 0,
      })
    case 'cannot_delete_all_keys':
      return t(
        'A multi-key channel must keep at least one key. Deselect at least one key, or delete the whole channel instead.'
      )
    case 'all_keys_auto_disabled':
      return t(
        'Every key of this channel is auto-disabled, so they cannot all be deleted. Delete the whole channel instead.'
      )
    case 'key_list_changed':
      // Both branches promise the reload *and* the cleared verdicts because
      // both handlers run `resyncAfterDelete`, and the lost verdicts are the
      // consequence the operator actually notices — they were paid for.
      return options.isDeleteAction
        ? t(
            'The key list changed on the server, so nothing was deleted. The key list has been reloaded and the stored test results were cleared, because key numbering may have shifted. Please try again.'
          )
        : t(
            'The key list changed on the server, so nothing was changed. The key list has been reloaded and the stored test results were cleared, because key numbering may have shifted. Please try again.'
          )
    case 'key_already_disabled':
      // The row was showing a stale enabled state, most likely because live
      // traffic auto-disabled the key. `performAction` reloads the list on this
      // code, so the copy can promise it.
      return t(
        'This key is already disabled, so nothing was changed. The key list has been reloaded to show its current status.'
      )
    default:
      return response.message || t('Operation failed')
  }
}

type MultiKeyManageDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type MultiKeyManageDialogContentProps = MultiKeyManageDialogProps & {
  currentRow: Channel
}

/**
 * Multi-key management dialog.
 *
 * The content lives in its own component keyed by channel id, so switching
 * channels always remounts it and no per-key test result can leak across
 * channels. Reopening the *same* channel reuses the instance, which is why every
 * close also funnels through `resetState`.
 *
 * @param props - Open state of the dialog.
 * @returns The dialog, or nothing when no channel is selected.
 */
export function MultiKeyManageDialog({
  open,
  onOpenChange,
}: MultiKeyManageDialogProps) {
  const { currentRow } = useChannels()

  if (!currentRow) {
    return null
  }

  return (
    <MultiKeyManageDialogContent
      key={currentRow.id}
      open={open}
      onOpenChange={onOpenChange}
      currentRow={currentRow}
    />
  )
}

function MultiKeyManageDialogContent({
  open,
  onOpenChange,
  currentRow,
}: MultiKeyManageDialogContentProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { copyToClipboard } = useCopyToClipboard()
  const currentUser = useAuthStore((s) => s.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const channelId = currentRow.id

  const [statusFilter, setStatusFilter] = useState<number | null>(null)
  const [bucketFilter, setBucketFilter] = useState<TestBucketOption | null>(
    null
  )
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [pagination, setPagination] = useState(DEFAULT_PAGINATION)
  const [confirmAction, setConfirmAction] =
    useState<MultiKeyConfirmAction | null>(null)
  const [isPerformingAction, setIsPerformingAction] = useState(false)
  /** Target indexes of a pending test confirmation; null while none is open */
  const [testConfirm, setTestConfirm] = useState<number[] | null>(null)
  /** Editable reason of the single-key Disable confirmation */
  const [singleDisableReason, setSingleDisableReason] = useState('')
  const [isDiscardConfirmOpen, setIsDiscardConfirmOpen] = useState(false)
  const [driftIndexes, setDriftIndexes] = useState<Set<number>>(() => new Set())
  const [elapsedMs, setElapsedMs] = useState(0)
  /** Failure whose full upstream body the operator asked to see; null while none */
  const [errorDetail, setErrorDetail] = useState<KeyErrorDetail | null>(null)
  /**
   * Bumped every time this dialog concludes the key list may have been
   * renumbered. `MultiKeyBulkActions` captures the selected key indexes once,
   * when its confirmation opens, and `expected_key_count` cannot catch a capture
   * that went stale while that confirmation sat open: once the refetch behind the
   * renumbering completes, the count we send is back in line with the server, so
   * the precondition passes and the pre-delete indexes are applied to the
   * renumbered list. This token is what makes the toolbar throw the capture away
   * instead of asking the server to validate it.
   */
  const [keyListToken, setKeyListToken] = useState(0)

  /**
   * What the post-run drift diff compares against: index -> status captured when
   * a run starts, plus the channel-wide key count those indexes were numbered
   * under. One ref holding both, because the two halves are only meaningful
   * together — a status map whose numbering cannot be checked is not a "before",
   * it is a set of numbers that happen to be in range.
   */
  const runStatusSnapshotRef = useRef<{
    statuses: Map<number, number>
    channelKeyCount: number
  } | null>(null)
  /**
   * BOTH key counts of the last snapshot the invalidation effect below saw. Null
   * until the first one lands, which is what makes the first snapshot clear
   * nothing. Two numbers rather than one because `MULTI_KEY_MAX_LOADED_KEYS` pins
   * the loaded count constant on any channel above it — see the effect.
   */
  const keyCountsRef = useRef<{ loaded: number; channelWide: number } | null>(
    null
  )
  /**
   * Whether `MultiKeyBulkActions` currently holds a confirmation the operator has
   * opened and not yet acted on — the toolbar reports it through
   * `onPendingDecisionChange`, see the contract on that prop.
   *
   * A REF, NOT STATE, and that is not a micro-optimisation: this dialog re-renders
   * a table of up to 5000 rows and recomputes six memos, and the only consumer is
   * one line inside the invalidation effect. Backing it with state would repaint
   * all of that every time a confirmation opens or closes, for a value nothing
   * renders.
   *
   * The toolbar owns the confirmation and the dialog owns the notice about losing
   * one, so the flag has to cross the boundary somehow: `discardKeyIndexedState()`
   * closes that confirmation INDIRECTLY, by bumping `keyListToken`, which means
   * the parent otherwise watches the single most destructive modal in the feature
   * vanish under the operator's cursor and says nothing about it.
   */
  const hasBulkPendingDecisionRef = useRef(false)

  /**
   * Records the toolbar's pending-decision flag. Stable identity on purpose: it
   * is a dependency of the toolbar's reporting effect, so a new function every
   * render would re-run that effect — cleanup first — on every parent render, and
   * the cleanup reports `false`.
   *
   * @param hasPendingDecision - True while the toolbar shows an idle confirmation.
   */
  const handleBulkPendingDecisionChange = useCallback(
    (hasPendingDecision: boolean) => {
      hasBulkPendingDecisionRef.current = hasPendingDecision
    },
    []
  )

  /**
   * Whether react-query should run the key-status walk again after a failure.
   *
   * The override exists for ONE verdict. `MULTI_KEY_WALK_UNSTABLE` is not a
   * failed request, it is `fetchMultiKeyStatus` reporting that it already walked
   * the list `MULTI_KEY_STATUS_MAX_WALK_ATTEMPTS` times and could not prove that
   * any pass described one numbering. Handing that back to the global retry
   * policy in `src/main.tsx` multiplies the constant instead of respecting it:
   * PROD there returns false only once `failureCount > 3`, so counts 0..3 all
   * retry and the queryFn runs FIVE times — up to 150 sequential POSTs and
   * roughly fifteen seconds of exponential backoff behind one undifferentiated
   * spinner, to re-establish what three attempts already established.
   *
   * Everything else keeps exactly the behaviour it had today. A blanket
   * `retry: false` would be the easy version and a worse bug: one dropped packet
   * on page 3 would become a dialog that cannot be opened, which is a harder
   * failure than the one being fixed. So the transport case is DELEGATED back to
   * the client defaults rather than reimplemented here — reimplementing would
   * fork the DEV/PROD split and the 401/403 rule into a second copy that stops
   * matching the first the day either is touched.
   *
   * @param failureCount - Failures so far for this fetch, as react-query counts them.
   * @param queryError - The error the queryFn threw.
   * @returns Whether to run the queryFn again.
   */
  const shouldRetryKeyStatus = useCallback(
    (failureCount: number, queryError: Error): boolean => {
      if (queryError.message === MULTI_KEY_WALK_UNSTABLE) return false

      const fallback = queryClient.getDefaultOptions().queries?.retry
      if (typeof fallback === 'function') {
        return fallback(failureCount, queryError)
      }
      if (typeof fallback === 'number') return failureCount < fallback
      return fallback ?? false
    },
    [queryClient]
  )

  const {
    data: snapshot,
    error,
    isFetching,
    isPending,
    refetch,
  } = useQuery({
    meta: { errorToast: false },
    queryKey: channelsQueryKeys.multiKey(channelId),
    // The context's `signal` is destructured, not ignored: READING it is what
    // sets query-core's `#abortSignalConsumed`, and that flag is the only thing
    // that makes losing the last observer cancel the walk rather than merely
    // stop listening to it. See `fetchMultiKeyStatus`.
    queryFn: ({ signal }) => fetchMultiKeyStatus(channelId, signal),
    enabled: open,
    staleTime: 0,
    retry: shouldRetryKeyStatus,
    // Keeps the previous rows on screen during a refresh, so an action does not
    // unmount the whole table and flash the dialog white.
    placeholderData: keepPreviousData,
  })

  const keys = snapshot?.keys ?? EMPTY_KEYS
  const loadedKeyCount = keys.length
  /**
   * The value every `expected_key_count` precondition is sent with: the
   * channel-wide key count the *server* reported in this snapshot, which is
   * exactly what the field means — "the number of keys I believe this channel
   * has".
   *
   * Deliberately NOT `loadedKeyCount`, and it must not be "simplified" back to
   * it: that is only the prefix this dialog managed to load, so the two numbers
   * disagree whenever the pagination walk stopped short — for good above
   * `MULTI_KEY_MAX_LOADED_KEYS`, until the next refresh on a list that grew
   * mid-load. Every batch is then rejected as `key_list_changed`, the rejection
   * handler throws away verdicts the operator paid thousands of upstream calls
   * for, and the reload reproduces the identical truncated snapshot — over the
   * cap the action is unusable forever, not merely once. On a fully loaded
   * channel the two values are equal, so nothing changes there, and a genuine
   * concurrent change still produces a clean rejection.
   *
   * The precondition compares COUNTS only, and so does the pin `fetchMultiKeyStatus`
   * puts on `total` across a multi-page walk. One residual survives both, and it
   * is worth stating in full because it is the only one left: a COUNT-PRESERVING
   * mutation — delete one plus append one, or a same-size `KeyMode='replace'`,
   * whose handler takes no lock and carries no version.
   *
   * Landing between this snapshot and the batch it authorises, it passes the
   * server precondition. Landing between two pages of the walk that BUILT this
   * snapshot, it is invisible twice over: `total` never moves, so the pin sees
   * nothing, and the spliced pages still carry indexes 0..n-1, so the integrity
   * assertion sees nothing either. Either way the batch is applied to a key list
   * the operator never read. Closing it needs the server-side key-list version
   * the invalidation effect below already names as the real fix.
   */
  const expectedKeyCount = snapshot?.channelKeyCount ?? loadedKeyCount

  useEffect(() => {
    if (!error) return
    // MULTI_KEY_WALK_UNSTABLE is a sentinel, not copy. It rides here as
    // `Error.message` because that is the only channel react-query gives a
    // queryFn, and it must be translated rather than shown: the operator's news
    // is that the list could not be read coherently, not a token. Every other
    // error keeps the message-first fallback unchanged — server rejections carry
    // their own text and it is more specific than anything written here.
    if (error.message === MULTI_KEY_WALK_UNSTABLE) {
      handleServerError(error, undefined, {
        title: t(
          'The key list kept changing while it was being loaded, so it could not be loaded safely. Please try again.'
        ),
      })
      return
    }
    handleServerError(error, t('Failed to load key status'))
  }, [error, t])

  /**
   * The key-status query is sitting in its error state.
   *
   * Read, rather than left to the table, because a failed load does not look
   * like one. react-query never clears `data` on failure: the `error` branch of
   * its reducer spreads the previous state and overwrites only error /
   * errorUpdateCount / errorUpdatedAt / fetchFailureCount / fetchFailureReason /
   * fetchStatus / status (`@tanstack/query-core/build/modern/query.js`). And
   * `isPending` is `status === 'pending'`, so it is FALSE the moment the error
   * lands — the skeleton is skipped and whatever `data` holds, including nothing,
   * is rendered as if it had just been confirmed by the server.
   *
   * The toast above is not a substitute for any of what follows. The Toaster is
   * mounted with `duration={5000}` (`src/routes/__root.tsx`) and this toast is
   * not pinned, so five seconds later the only trace of the failure is gone while
   * every wrong number it explained is still on screen. The toast catches the
   * eye; the body states are what survive it.
   */
  const hasLoadError = error !== null
  /**
   * CASE A — the load failed and no snapshot for this channel was ever obtained.
   * Every derived count is 0 because nothing is KNOWN, not because the channel is
   * empty, so the three statistics cards, the "Test all {{count}} keys" label and
   * the table's own "No keys found" would each assert, permanently and with no
   * spinner to hedge them, that a channel of thousands of keys has none. The body
   * renders one explicit error state in place of all of them.
   */
  const hasUnloadableKeyList = hasLoadError && !snapshot
  /**
   * CASE B — the load failed but an earlier one succeeded. `keepPreviousData` and
   * the untouched `data` keep the PRE-failure rows, counters and "Status updated
   * at ..." on screen, and `isFetching` is false, so nothing distinguishes them
   * from a fresh load: after a delete whose resync walk failed, the row that was
   * just deleted is still listed under a success toast. They stay on screen —
   * stale rows are still the operator's best information, and the repair actions
   * still work against them — but they get a banner that says what they are.
   */
  const hasStaleKeyListBehindError = hasLoadError && Boolean(snapshot)

  const resetPageIndex = useCallback(() => {
    setPagination((prev) =>
      prev.pageIndex === 0 ? prev : { ...prev, pageIndex: 0 }
    )
  }, [])

  const handleRunSettled = useCallback(() => {
    const before = runStatusSnapshotRef.current
    runStatusSnapshotRef.current = null

    void refetch()
      .then((result) => {
        const nextSnapshot = result.data
        if (!before || !nextSnapshot) return
        // The diff below matches BY INDEX, and this very refetch is normally the
        // first place a concurrent renumbering surfaces. On a 10-key channel with
        // key 9 already auto-disabled, one key deleted mid-run makes old key 9
        // the new index 8: `before` says index 8 was enabled (it was — old key 8)
        // and the fresh row at index 8 is auto-disabled (it is — old key 9), so
        // the diff reports a drift live traffic never caused, and it lands BEFORE
        // the invalidation effect toasts the true story. Guarding on the same
        // channel-wide count `expected_key_count` uses costs one comparison and
        // makes the diff answer only for the numbering it was captured under.
        // Skipping it whole loses nothing: the effect is already about to fire on
        // that same count change and say the one thing that is true.
        if (nextSnapshot.channelKeyCount !== before.channelKeyCount) return

        const drifted = nextSnapshot.keys
          .filter(
            (keyStatus) =>
              before.statuses.get(keyStatus.index) ===
                MULTI_KEY_STATUS.ENABLED &&
              keyStatus.status === MULTI_KEY_STATUS.AUTO_DISABLED
          )
          .map((keyStatus) => keyStatus.index)
        if (drifted.length === 0) return

        setDriftIndexes(new Set(drifted))
        toast.info(
          t(
            '{{count}} keys were disabled automatically by live traffic while the test was running',
            { count: drifted.length }
          )
        )
      })
      .catch(() => undefined)
  }, [refetch, t])

  const {
    results: testResults,
    testingIndexes,
    isBatchTesting,
    isStopRequested,
    progress,
    testKey,
    testKeys,
    stop,
    clearResults,
    reset,
    dismissPinnedNotices,
  } = useMultiKeyBatchTest({
    channelId,
    channelName: currentRow.name,
    onRunSettled: handleRunSettled,
  })

  /**
   * Reports that a key-list change killed the key test that was in flight. Both
   * callers abort through `clearResults()`, which drops `isBatchTesting` and
   * `testingIndexes` in one go, so the two flags have to be captured *before* the
   * clear and handed in here.
   *
   * @param wasBatchTesting - Whether a batch run was in flight before the clear.
   * @param wasRowTestInFlight - Whether any single key was in flight before it.
   * @returns True when a notice was shown, so a caller can skip its own fallback.
   */
  const notifyTestStoppedByKeyListChange = useCallback(
    (wasBatchTesting: boolean, wasRowTestInFlight: boolean): boolean => {
      if (wasBatchTesting) {
        toast.info(
          t(
            'The key list of this channel changed, so the batch test was stopped because key numbering may have shifted. The keys need to be tested again.'
          )
        )
        return true
      }
      // A row's Test is one real, billed generation call that can run for tens of
      // seconds, and it is neither a batch run nor a stored verdict — so without
      // this branch the abort just makes the spinner vanish, with no verdict, no
      // toast and nothing saying the request the operator paid for is gone.
      if (wasRowTestInFlight) {
        toast.info(
          t(
            'The key list of this channel changed, so the running key test was stopped because key numbering may have shifted. The keys need to be tested again.'
          )
        )
        return true
      }
      return false
    },
    [t]
  )

  /**
   * Forgets everything this dialog knows by key INDEX, because the numbering
   * those indexes were captured under may no longer be the server's.
   *
   * This is the RENUMBERING path, and it is the only one. Both of its callers —
   * the invalidation effect below and `resyncAfterDelete` — react to the same
   * news, that the server's key list moved while this dialog was looking at it,
   * and every new piece of state that holds a key index, a list of key indexes or
   * anything derived from one MUST be added here rather than to one caller: the
   * two paths were written separately, drifted apart, and each time the state one
   * of them forgot was the state that ended up applied to a renumbered list.
   *
   * It is NOT the only place these fields are ever cleared, and it must not be
   * made so. `resetState` clears seven of the same ones on the CLOSE path, which
   * is a different question with a different answer: closing throws away the
   * whole session, so it also drops things that have nothing to do with
   * numbering — the status filter, the pagination — while deliberately NOT
   * bumping `keyListToken`, because there is no renumbering for the bulk toolbar
   * to be told about and the toolbar is unmounting anyway. Consolidating the two
   * would drag those semantics into whichever direction the merge went. Keep them
   * separate; keep this one exhaustive about indexes.
   *
   * Callers that report what was lost have to read their flags — `isBatchTesting`,
   * `testingIndexes`, the verdict count, an open confirmation — BEFORE calling
   * this, because it drops all of them in one go.
   */
  const discardKeyIndexedState = useCallback(() => {
    // The verdict map (keyed by index), `testingIndexes`, and any run in flight.
    clearResults()
    // Row ids ARE the key indexes, see `getRowId`, so a surviving selection
    // re-points at whatever now occupies those slots.
    setRowSelection({})
    // Reads through the verdict map that just went: kept, it would filter every
    // row out and offer a bucket no key can be in.
    setBucketFilter(null)
    setDriftIndexes(() => new Set())
    setErrorDetail(null)
    // An open ROW confirmation holds the index it was opened for and nothing on
    // screen names that key, so confirming it after a renumbering disables or
    // permanently deletes one the operator never chose — and because
    // `expected_key_count` is recomputed from the fresh snapshot at dispatch
    // time, the precondition agrees with the server and refuses nothing. The
    // channel-wide confirmations carry no index but quote counts that just moved,
    // so they go the same way rather than being filtered out by type.
    setConfirmAction(null)
    // Pre-filled from the verdict stored at that same index; left behind it would
    // carry a stale diagnosis into the next confirmation.
    setSingleDisableReason('')
    // The same capture, a whole list of it: confirming would spend real upstream
    // calls on keys the operator did not pick and bank verdicts under their
    // indexes.
    setTestConfirm(null)
    // The run-start "before" of the drift diff: index -> status, plus the
    // channel-wide count those indexes were numbered under. `handleRunSettled`
    // CAN reach it across a renumbering — its own refetch is usually the first
    // observation of one — which is exactly why it compares that count before it
    // diffs anything. Dropping the whole capture here is the same guard from the
    // other side, for the run `clearResults()` has just abandoned.
    runStatusSnapshotRef.current = null
    // Last: the bulk toolbar keeps its OWN copy of the selected indexes once its
    // confirmation is open, and this token is the only thing that makes it throw
    // that copy away.
    setKeyListToken((token) => token + 1)
    // And the mirror of that copy this dialog keeps for the notice. The toolbar
    // will report `false` by itself once the token bump closes its confirmation,
    // two commits from now; writing it here as well is idempotent and keeps this
    // function's promise literally true — after it returns, nothing this dialog
    // holds still vouches for the old numbering. Order matters only in that every
    // caller reads the flag BEFORE calling, which the doc comment above requires.
    hasBulkPendingDecisionRef.current = false
  }, [clearResults])

  // ANY change to EITHER key count invalidates the index-keyed state above. Two
  // counts because the loaded one alone is blind exactly where the channels are
  // biggest: `MULTI_KEY_MAX_LOADED_KEYS` stops the walk at the cap, so on a
  // 6000-key channel `loadedKeyCount` is 5000 before a concurrent delete and
  // 5000 after it, while the channel-wide count the server reports moves 6000 ->
  // 5999 and every survivor has shifted down. Comparing only the loaded count
  // there clears nothing, leaves 5000 verdicts sitting one row off, and the
  // channel-wide `expected_key_count` this dialog sends has by then caught up to
  // the server, so the precondition passes and a bulk Disable writes the
  // reviewed reason onto keys nobody selected.
  //
  // An append on its own would not need any of this — the server appends at the
  // end, so every existing index keeps pointing at the same key — but a count
  // cannot tell a pure append from a delete plus a bigger append: `delete_key`
  // renumbers every survivor, and 10 -> 11 reads identically as "one appended"
  // and as "one deleted, two appended". No cheap client-side signal breaks the
  // tie either (`key_preview` is a vendor-shared prefix, and status /
  // disabled_time are identical across an all-enabled channel), so it is broken
  // toward clearing, in either direction and on either count: losing paid
  // verdicts is loud and undone by testing again, while acting on a shifted index
  // silently disables a healthy key and leaves the dead one serving traffic.
  // Keeping verdicts across a concurrent key-list change needs a server-side
  // key-list version to compare against.
  useEffect(() => {
    if (!snapshot) return

    const previous = keyCountsRef.current
    const current = {
      loaded: loadedKeyCount,
      channelWide: snapshot.channelKeyCount,
    }
    keyCountsRef.current = current
    // No previous snapshot means nothing was captured under an older numbering,
    // so the first load must not clear anything.
    if (previous === null) return
    if (
      previous.loaded === current.loaded &&
      previous.channelWide === current.channelWide
    ) {
      return
    }

    // Read before clearing. Every one of these is also a dependency purely
    // because it is read here, and none of them can loop: `keyCountsRef.current`
    // is assigned ABOVE both early returns, so a re-run provoked by an arriving
    // verdict, by a confirmation opening, or by `discardKeyIndexedState()`
    // closing one exits at the count comparison — the snapshot has not changed,
    // so `current` still equals what the previous run stored. One notice covers
    // both directions of the count move — the operator cannot act on which way it
    // went, and without any notice the verdicts look like they vanished on their
    // own.
    const hadResults = Object.keys(testResults).length > 0
    // Same, and for the same reason: `clearResults()` abandons any in-flight run,
    // so this flag is false by the time the toast is picked below. A stopped run
    // outranks the discarded verdicts even with none banked yet — the operator
    // paid for requests that will now never produce a verdict.
    const wasBatchTesting = isBatchTesting
    // Same again, read the same way `isRowTestInFlight` derives it: a single row's
    // Test is dropped by the very same `clearResults()`, and it is invisible to
    // both flags above — no batch run, and no verdict banked yet either.
    const wasRowTestInFlight = testingIndexes.size > 0
    // Same again, and the quietest of the four: `discardKeyIndexedState()` also
    // closes an open row / channel-wide confirmation and an open batch-test
    // confirmation, so both are null by the time the notice is picked below. On a
    // dialog with no verdicts and no test in flight — the operator opened it,
    // clicked Delete on one row and is reading the confirmation when a reconnect
    // refetch (`refetchOnReconnect` is on and this query is always stale) brings
    // in a changed count — the two flags above are false and the modal would
    // simply vanish mid-decision with nothing said.
    //
    // THE BULK TOOLBAR'S OWN CONFIRMATION COUNTS TOO, and it is the one that
    // matters most: "Delete N selected keys of this channel? This action cannot
    // be undone." lives in `MultiKeyBulkActions`' local state, and
    // `discardKeyIndexedState()` closes it only INDIRECTLY, by bumping
    // `keyListToken`. Read from the parent's own state alone this branch is blind
    // to exactly the most destructive modal in the dialog, so the toolbar reports
    // it through `onPendingDecisionChange` and the ref is folded in here.
    //
    // The read is safe against effect ordering rather than lucky. React runs
    // child effects before parent effects within one commit, but the toolbar's
    // clearing effect is keyed on `keyListToken`, which only moves INSIDE this
    // effect — so in the commit this effect runs in, the toolbar's reporting
    // effect has no changed dependency and does not re-run, and the ref still
    // holds the `true` it was given when the confirmation opened. The toolbar
    // reports `false` two commits later, after `keyListToken` has dropped its
    // confirmation. The reverse order is also correct: if the operator dismisses
    // the confirmation in the same commit the count moves, the child reports
    // `false` FIRST and no notice is shown, which is right — nothing vanished
    // under the cursor.
    //
    // GATED ON `isPerformingAction`, because a dispatch owns the story about its
    // own outcome. `ConfirmDialog`'s confirm button is a plain `<Button>` that
    // does NOT close the dialog (`src/components/confirm-dialog.tsx`), and
    // `performAction` clears `confirmAction` only in its `finally`, so
    // `confirmAction !== null` stays true for the whole duration of the request
    // it dispatched. Without this gate a renumbering landing mid-flight toasts
    // "... Nothing was changed." over an action that is at that moment changing
    // things, and the request's own "Operation successful" then contradicts it —
    // two toasts, and the new one is the false one. The same gate covers the
    // toolbar from the parent's side; the toolbar additionally reports `false`
    // while its own `isDispatching` is set, so its dispatch cannot reproduce this
    // one level down.
    //
    // `testConfirm` needs no such gate and must not get one: `startTest` clears
    // it synchronously before `testKeys` is called, so it is already null by the
    // time the run it authorised is in flight — and that run is then covered by
    // `wasBatchTesting`, whose branch outranks this one anyway.
    //
    // What is deliberately NOT changed by any of this: `discardKeyIndexedState()`
    // still closes the confirmation mid-flight. Round 9 established that leaving
    // it open is what lets a stale index be dispatched, and that closing it
    // cannot strand `isPerformingAction` nor double-dispatch, because
    // `performAction`'s `finally` runs regardless and reads `confirmAction` from
    // its own closure. Only what is SAID changes here.
    const hadPendingDecision =
      !isPerformingAction &&
      (confirmAction !== null ||
        testConfirm !== null ||
        hasBulkPendingDecisionRef.current)

    discardKeyIndexedState()

    // Called once, before the branches: it toasts as a side effect, so it cannot
    // be re-evaluated per branch.
    const reportedStoppedTest = notifyTestStoppedByKeyListChange(
      wasBatchTesting,
      wasRowTestInFlight
    )
    if (!reportedStoppedTest && hadResults) {
      toast.info(
        t(
          'The key list of this channel changed, so key numbering may have shifted. The test results were cleared and the keys need to be tested again.'
        )
      )
    }

    // Deliberately NOT chained to the branch above. The precedence between a
    // stopped run and discarded verdicts is real and stays: those are two ways of
    // describing the same lost test work, so only the stronger one is said. A
    // dropped confirmation is a different fact about a different thing, and it
    // carries the one sentence neither other message has — "Nothing was changed."
    //
    // Chained as an `else` it was silent in exactly the workflow the bulk toolbar
    // exists for: test all, filter to a status bucket, select it, bulk delete. By
    // the time the operator reaches that confirmation `hadResults` is always true
    // and always wins, so the most destructive modal in the dialog went back to
    // vanishing under the cursor with only "the test results were cleared" to
    // explain it — which says nothing about whether the delete ran. A modal that
    // disappears while the operator is reading "this action cannot be undone"
    // reads as "it went through", and that is the whole reason this notice exists.
    //
    // Two toasts can now appear at once (verdicts cleared plus confirmation
    // dropped). That is intended: they are complementary, not contradictory. The
    // pair this feature must never emit is a true one next to a false one, which
    // is what the `isPerformingAction` gate on `hadPendingDecision` prevents.
    if (hadPendingDecision) {
      toast.info(
        t(
          'The key list of this channel changed, so the pending confirmation was closed. Nothing was changed.'
        )
      )
    }
  }, [
    confirmAction,
    discardKeyIndexedState,
    isBatchTesting,
    // Read by `hadPendingDecision`, and harmless as a dependency for the same
    // reason as the rest: a re-run it provokes exits at the count comparison,
    // because `keyCountsRef.current` was assigned above both early returns.
    isPerformingAction,
    loadedKeyCount,
    notifyTestStoppedByKeyListChange,
    snapshot,
    t,
    testConfirm,
    testResults,
    testingIndexes,
  ])

  const startedAt = progress?.startedAt
  useEffect(() => {
    if (startedAt === undefined) {
      setElapsedMs(0)
      return
    }

    setElapsedMs(Date.now() - startedAt)
    const timer = window.setInterval(
      () => setElapsedMs(Date.now() - startedAt),
      1000
    )
    return () => window.clearInterval(timer)
  }, [startedAt])

  const testResultCount = useMemo(
    () => Object.keys(testResults).length,
    [testResults]
  )

  const statusFilteredKeys = useMemo(
    () =>
      statusFilter === null
        ? keys
        : keys.filter((keyStatus) => keyStatus.status === statusFilter),
    [keys, statusFilter]
  )

  // Counted over the status-filtered rows rather than every loaded key: the
  // number in `HTTP 401 (7)` has to be the number of rows picking that option
  // will actually leave in the table.
  const bucketCounts = useMemo(() => {
    const counts = new Map<
      string,
      { bucket: TestBucketOption; count: number }
    >()
    for (const keyStatus of statusFilteredKeys) {
      const bucket = resolveStoredBucket(testResults[keyStatus.index])
      const value = getTestBucketValue(bucket)
      const entry = counts.get(value)
      if (entry) {
        entry.count += 1
      } else {
        counts.set(value, { bucket, count: 1 })
      }
    }
    return counts
  }, [statusFilteredKeys, testResults])

  const bucketItems = useMemo(() => {
    const entries = [...bucketCounts.values()]
    const selectedValue = bucketFilter ? getTestBucketValue(bucketFilter) : null
    // The selected bucket has to stay in the list even once it empties out, or
    // Base UI Select cannot render its label and the user cannot select it back.
    if (bucketFilter && selectedValue && !bucketCounts.has(selectedValue)) {
      entries.push({ bucket: bucketFilter, count: 0 })
    }
    entries.sort((a, b) => compareTestBuckets(a.bucket, b.bucket))

    return [
      { value: ALL_BUCKETS, label: t('All Test Results'), bucket: null },
      ...entries.map((entry) => {
        const { key, params } = getTestBucketLabel(entry.bucket, entry.count)
        return {
          value: getTestBucketValue(entry.bucket),
          label: t(key, params),
          bucket: entry.bucket,
        }
      }),
    ]
  }, [bucketCounts, bucketFilter, t])

  const filteredKeys = useMemo(() => {
    const bucketValue = bucketFilter ? getTestBucketValue(bucketFilter) : null
    if (!bucketValue) return statusFilteredKeys

    return statusFilteredKeys.filter(
      (keyStatus) =>
        getTestBucketValue(
          resolveStoredBucket(testResults[keyStatus.index])
        ) === bucketValue
    )
  }, [bucketFilter, statusFilteredKeys, testResults])

  /**
   * Opens the single-key confirmation. Disable additionally pre-fills the same
   * bucket-derived reason the bulk toolbar offers, so the two paths cannot record
   * different reasons for the same diagnosis.
   */
  const handleRowAction = useCallback(
    (action: MultiKeyConfirmAction) => {
      if (action.type === 'disable' && action.keyIndex !== undefined) {
        setSingleDisableReason(
          getDisableReasonForBucket(
            resolveStoredBucket(testResults[action.keyIndex]),
            t
          )
        )
      }
      setConfirmAction(action)
    },
    [t, testResults]
  )

  /**
   * A dialog mutation is in flight — or a refetch is, which for the row actions
   * amounts to the same danger: `placeholderData: keepPreviousData` keeps the
   * PRE-refetch rows and their indexes rendered for the whole request, several
   * seconds on a channel that needs four sequential pages, so an action fired now
   * is built on numbering the server may already have moved past. Deliberately
   * *not* the same thing as gating on a test being in flight, which the row-level
   * Enable/Disable must never be: a test leaves the numbering alone.
   */
  const isRowActionBlocked = isPerformingAction || isFetching

  /**
   * A failed key-list load blocks TESTING and nothing else. Read the second half
   * before "tightening" this into a general read-only mode.
   *
   * Testing is blocked because a test spends real, billed upstream generation
   * calls against a list of key INDEXES, and `test_key` is the one action this
   * dialog can fire that carries NO `expected_key_count` precondition — nothing
   * server side will refuse a run aimed at a numbering the client can no longer
   * read. Case A has no numbering at all; case B has one that is provably not the
   * server's current answer.
   *
   * Enable, Disable and Delete deliberately stay live, row-level and bulk alike.
   * Every one of them carries the precondition, so a dispatch built on stale
   * indexes comes back as `key_list_changed` with copy that says so and a resync
   * behind it — the failure mode is one wasted round trip. And this error state is
   * precisely when an operator is trying to REPAIR a channel; taking their repair
   * tools away because the read path is failing would leave them looking at a
   * channel they can see is wrong and cannot touch. That trade is intentional.
   */
  const isTestBlockedByLoadError = hasLoadError
  /**
   * Copy for every refused test trigger. The pinned banner string, reused rather
   * than paraphrased so the tooltip, the toast and the banner cannot drift into
   * three different accounts of the same state.
   */
  const testBlockedByLoadErrorMessage = t(
    'The key list could not be refreshed. What is shown below may be out of date, and testing is disabled until it loads again.'
  )

  /**
   * A row's Test button, refused while the key list is in its error state.
   *
   * The refusal is a callback guard rather than a `disabled` attribute because
   * `MultiKeyTableRowActions` exposes no prop that greys out Test alone: it
   * disables Test through `isTesting`, `isBatchTesting` and `isPerformingAction`,
   * and each of those also disables that row's Delete — which must stay live per
   * `isTestBlockedByLoadError` — while two of them additionally mislabel the
   * reason. Adding one would mean editing that component, which is outside the
   * file this round may touch. The guard is what actually stops the billed
   * request; the toast is what stops the click from reading as a no-op.
   *
   * @param keyIndex - Index of the key whose Test was pressed.
   */
  const handleRowTest = useCallback(
    async (keyIndex: number) => {
      if (isTestBlockedByLoadError) {
        toast.error(testBlockedByLoadErrorMessage)
        return
      }
      await testKey(keyIndex)
    },
    [isTestBlockedByLoadError, testBlockedByLoadErrorMessage, testKey]
  )

  const columns = useMemo<ColumnDef<KeyStatus>[]>(
    () => [
      {
        id: 'select',
        // All-rows variant, not the page variant: this table paginates client
        // side and the operator has to be able to select across pages.
        header: ({ table: tableInstance }) => (
          <Checkbox
            checked={tableInstance.getIsAllRowsSelected()}
            indeterminate={
              tableInstance.getIsSomeRowsSelected() &&
              !tableInstance.getIsAllRowsSelected()
            }
            onCheckedChange={(value) =>
              tableInstance.toggleAllRowsSelected(!!value)
            }
            aria-label={t('Select all keys')}
          />
        ),
        cell: ({ row }) => (
          <Checkbox
            checked={row.getIsSelected()}
            disabled={!row.getCanSelect()}
            onCheckedChange={(value) => row.toggleSelected(!!value)}
            aria-label={t('Select key #{{index}}', {
              index: row.original.index + 1,
            })}
          />
        ),
        enableSorting: false,
        enableHiding: false,
        size: 44,
      },
      {
        id: 'index',
        header: t('Index'),
        cell: ({ row }) => (
          <span
            className='font-mono'
            title={t(
              'The number is the position of the key in the channel key list; it changes when a key is deleted.'
            )}
          >
            {`#${row.original.index + 1} / ${loadedKeyCount}`}
          </span>
        ),
        enableSorting: false,
        size: 96,
      },
      {
        id: 'status',
        header: t('Status'),
        cell: ({ row }) => {
          const config = getMultiKeyStatusConfig(row.original.status)
          return (
            <div className='flex flex-wrap items-center gap-1'>
              <StatusBadge
                label={t(config.label)}
                variant={config.variant}
                showDot
                copyable={false}
              />
              {driftIndexes.has(row.original.index) && (
                <StatusBadge
                  label={t('Changed during test')}
                  variant='warning'
                  size='sm'
                  copyable={false}
                />
              )}
            </div>
          )
        },
        enableSorting: false,
        size: 144,
      },
      {
        id: 'test-result',
        // Named after what it really reports: the provider's own status code.
        // `status_code_mapping` is deliberately not applied to the test path, so
        // the tooltip has to say so — see the header below.
        header: () => (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type='button'
                  className='focus-visible:ring-ring/50 flex cursor-help items-center gap-1 rounded-sm focus-visible:ring-[3px] focus-visible:outline-none'
                />
              }
            >
              <span>{t('Upstream status')}</span>
              <Info
                className='text-muted-foreground size-3.5 shrink-0'
                aria-hidden='true'
              />
            </TooltipTrigger>
            <TooltipContent>
              {t(
                "The raw status code the provider returned. This channel's status code mapping is deliberately not applied here, so it can differ from the status code shown in the relay error log."
              )}
            </TooltipContent>
          </Tooltip>
        ),
        cell: ({ row }) => {
          const keyIndex = row.original.index
          return (
            <KeyTestResultCell
              keyIndex={keyIndex}
              bucket={resolveTestBucket({
                result: testResults[keyIndex],
                isTesting: testingIndexes.has(keyIndex),
              })}
              outcome={testResults[keyIndex]}
              onOpenDetails={setErrorDetail}
            />
          )
        },
        enableSorting: false,
        size: 320,
      },
      {
        id: 'actions',
        header: t('Actions'),
        cell: ({ row }) => (
          <MultiKeyTableRowActions
            keyIndex={row.original.index}
            status={row.original.status}
            canDelete={canEditSensitive}
            isTesting={testingIndexes.has(row.original.index)}
            isBatchTesting={isBatchTesting}
            isPerformingAction={isRowActionBlocked}
            onTest={handleRowTest}
            onAction={handleRowAction}
          />
        ),
        enableSorting: false,
        size: 256,
      },
      {
        id: 'reason',
        header: t('Disabled Reason'),
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {row.original.reason || '-'}
          </span>
        ),
        enableSorting: false,
        size: 208,
      },
      {
        id: 'disabled-time',
        header: t('Disabled Time'),
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {row.original.disabled_time
              ? formatTimestamp(row.original.disabled_time)
              : '-'}
          </span>
        ),
        enableSorting: false,
        size: 176,
      },
    ],
    // Test results are read through this closure instead of being spread into the
    // row objects, so DataTableView's row memoisation would render stale
    // verdicts if any of these were missing.
    [
      canEditSensitive,
      driftIndexes,
      handleRowAction,
      handleRowTest,
      isBatchTesting,
      isRowActionBlocked,
      loadedKeyCount,
      t,
      testResults,
      testingIndexes,
    ]
  )

  const ensurePageInRange = useCallback((pageCount: number) => {
    setPagination((prev) =>
      pageCount > 0 && prev.pageIndex >= pageCount
        ? { ...prev, pageIndex: pageCount - 1 }
        : prev
    )
  }, [])

  const canSelectRow = useCallback(
    (row: Row<KeyStatus>) => !testingIndexes.has(row.original.index),
    [testingIndexes]
  )

  const { table } = useDataTable({
    data: filteredKeys,
    columns,
    // Reads the field, not the row position: a future move back to server-side
    // pagination must not make the row id drift away from the real key index.
    getRowId: (keyStatus) => String(keyStatus.index),
    rowSelection,
    onRowSelectionChange: setRowSelection,
    pagination,
    onPaginationChange: setPagination,
    enableSorting: false,
    enableRowSelection: canSelectRow,
    // Filtering happens upstream of the table: useDataTable does not manage
    // columnFilters, and a filterFn reading testResults through a closure would
    // not recompute as verdicts arrive.
    withFilteredRowModel: false,
    withSortedRowModel: false,
    withFacetedRowModel: false,
    // The data identity changes throughout a run; the default reset would bounce
    // the operator back to page 1 over and over.
    autoResetPageIndex: false,
    ensurePageInRange,
  })

  const selectedRows = table.getFilteredSelectedRowModel().rows
  const selectedCount = selectedRows.length
  const defaultDisableReason = getDefaultDisableReason(
    selectedRows,
    testResults,
    t
  )
  /**
   * Read from the same selected rows as `defaultDisableReason`, so the warning
   * and the reason it pre-fills can never describe different selections.
   */
  const sharedNoResponseError = useMemo(
    () => getSharedNoResponseError(selectedRows, testResults),
    [selectedRows, testResults]
  )
  const hasFilter = statusFilter !== null || bucketFilter !== null
  const isRefreshing = isFetching && !isPending
  /**
   * The load cap stopped the pagination loop before the last page, so the loaded
   * keys are only a prefix of the channel's real list. Everything index-based
   * still works — including `expected_key_count`, which carries the server's own
   * channel-wide COUNT and therefore matches — so this flag, not the server, is
   * what holds bulk delete back: an irreversible batch must not be authorised
   * from a snapshot that provably omits keys, and no refresh can complete it.
   * Deliberately not true for `grewDuringLoad`: that list is completable by a
   * refresh, and permanently disabling bulk delete over it would lock out any
   * channel that happened to gain a key mid-load.
   */
  const isKeyListTruncated = snapshot?.truncatedByCap ?? false
  /**
   * Keys were appended while the dialog was walking the pages, so the snapshot
   * is a stale prefix. Bulk delete stays enabled: appends never renumber the
   * prefix, so the captured indexes still address the rows on screen, and a list
   * that is still moving is caught by the channel-wide COUNT comparison, whose
   * rejection reloads — but saying so up front spares the operator that round
   * trip.
   */
  const didKeyListGrowDuringLoad = snapshot?.grewDuringLoad ?? false
  /**
   * A key is in flight outside a batch run, i.e. someone pressed a row's Test.
   * The hook makes a batch run *join* that request instead of skipping the key,
   * so gating the two run triggers on it is only about not letting a click look
   * like a no-op while the joined request is still open.
   */
  const isRowTestInFlight = testingIndexes.size > 0
  const disabledKeyCount =
    (snapshot?.manualDisabledCount ?? 0) + (snapshot?.autoDisabledCount ?? 0)
  const testModelLabel =
    currentRow.test_model?.trim() || t('Channel default model')

  const clearFilters = useCallback(() => {
    setStatusFilter(null)
    setBucketFilter(null)
    resetPageIndex()
  }, [resetPageIndex])

  const resetState = useCallback(() => {
    // reset() is the hook's own three steps: abandon the run (bump the run token,
    // set stopRef, abort every controller), clear results / testingIndexes /
    // progress / flags, dismiss the progress toast. It deliberately leaves the two
    // pinned notices — rate limited, channel-level failure — standing, and that
    // must not be tidied back in: they describe the gateway's or the channel's
    // state rather than this run's, so they still hold after the run is gone, and
    // a run stopped by either of them recorded no verdicts, which means a plain
    // ESC skips the discard confirmation and would otherwise wipe the only
    // warning the operator ever got.
    reset()
    setRowSelection({})
    setStatusFilter(null)
    setBucketFilter(null)
    setPagination(DEFAULT_PAGINATION)
    setConfirmAction(null)
    setTestConfirm(null)
    setDriftIndexes(() => new Set())
    setErrorDetail(null)
    setIsDiscardConfirmOpen(false)
    runStatusSnapshotRef.current = null
  }, [reset])

  const closeDialog = useCallback(() => {
    resetState()
    onOpenChange(false)
  }, [onOpenChange, resetState])

  /**
   * Confirmed-discard close: the operator answered yes to throwing the verdicts
   * away. This is the one close path that proves the pinned notices were read —
   * the run they explain produced the very results being discarded — so they are
   * retired on this close path and no other. `resetState`, `closeDialog` and
   * the zero-verdict close deliberately leave them up.
   */
  const confirmDiscardAndClose = useCallback(() => {
    dismissPinnedNotices()
    closeDialog()
  }, [closeDialog, dismissPinnedNotices])

  const handleDialogOpenChange = useCallback(
    (nextOpen: boolean) => {
      if (nextOpen) return

      if (isBatchTesting) {
        // A paid run may only be ended through Stop, never by ESC or the close
        // button. Outside presses are already blocked by disablePointerDismissal.
        toast.info(t('Stop the batch test before closing this dialog.'))
        return
      }
      if (testResultCount > 0) {
        setIsDiscardConfirmOpen(true)
        return
      }
      closeDialog()
    },
    [closeDialog, isBatchTesting, t, testResultCount]
  )

  const handleEscapeCapture = useCallback(
    (event: KeyboardEvent<HTMLDivElement>) => {
      if (event.key !== 'Escape') return
      if (table.getFilteredSelectedRowModel().rows.length === 0) return

      // Makes the floating toolbar's advertised "Clear selection (Escape)" true
      // inside a dialog. Base UI listens for Escape on `document` while React's
      // listener sits on the portal container below it, so stopping propagation
      // from a capture handler is what keeps the popup open.
      event.preventDefault()
      event.stopPropagation()
      table.resetRowSelection()
    },
    [table]
  )

  const handleStatusFilterChange = useCallback(
    (value: string | null) => {
      if (value === null) return

      setStatusFilter(
        value === ALL_STATUSES ? null : Number.parseInt(value, 10)
      )
      resetPageIndex()
    },
    [resetPageIndex]
  )

  const handleBucketFilterChange = useCallback(
    (value: string | null) => {
      if (value === null) return

      setBucketFilter(
        bucketItems.find((item) => item.value === value)?.bucket ?? null
      )
      resetPageIndex()
    },
    [bucketItems, resetPageIndex]
  )

  const startTest = useCallback(
    (keyIndexes: number[]) => {
      // The last gate every batch path passes through, and the only one that
      // catches a test confirmation which was already OPEN when the load failed:
      // a load error does not move the key count, so the invalidation effect
      // never fires and never closes that confirmation. Both buttons behind it
      // are disabled by then, but the modal in front of the operator is not, and
      // confirming it would spend the whole run. Close it and say why.
      if (isTestBlockedByLoadError) {
        setTestConfirm(null)
        toast.error(testBlockedByLoadErrorMessage)
        return
      }

      runStatusSnapshotRef.current = {
        statuses: new Map(
          keys.map((keyStatus) => [keyStatus.index, keyStatus.status])
        ),
        // Read from the same snapshot as the statuses above, so the count and the
        // numbering it vouches for can never describe two different loads.
        channelKeyCount: expectedKeyCount,
      }
      setDriftIndexes(() => new Set())
      setTestConfirm(null)
      void testKeys(keyIndexes)
    },
    [
      expectedKeyCount,
      isTestBlockedByLoadError,
      keys,
      testBlockedByLoadErrorMessage,
      testKeys,
    ]
  )

  const handleTestSelected = useCallback(() => {
    const keyIndexes = selectedRows.map((row) => row.original.index)
    if (keyIndexes.length === 0) return

    if (keyIndexes.length > TEST_SELECTED_CONFIRM_THRESHOLD) {
      setTestConfirm(keyIndexes)
      return
    }
    startTest(keyIndexes)
  }, [selectedRows, startTest])

  const handleCopyResults = useCallback(() => {
    const lines = filteredKeys.map((keyStatus) => {
      const outcome = testResults[keyStatus.index]
      const bucket = resolveStoredBucket(outcome)
      const code =
        bucket.kind === 'http-error' || bucket.kind === 'success'
          ? bucket.code
          : undefined
      const errorText = outcome?.error?.trim()
      const firstLine = errorText
        ? normalizeInlineError(getFirstErrorLine(errorText) ?? errorText)
        : ''
      return [
        `#${keyStatus.index + 1}`,
        getTestBucketValue(bucket),
        code === undefined ? '' : String(code),
        firstLine,
      ].join('\t')
    })
    void copyToClipboard(lines.join('\n'))
  }, [copyToClipboard, filteredKeys, testResults])

  /**
   * Drops every local assumption about the key list and reloads it. Used after a
   * delete, successful or not — a failed delete may or may not have landed
   * upstream, and the surviving keys are renumbered when it did — and after a
   * `key_list_changed` rejection of any action, bulk or row, which says the same
   * thing: the server's key count moved, so every index-keyed verdict on screen
   * may now name a different key. The status filter and the page index go too,
   * even for a non-destructive rejection: the verdicts they were narrowed around
   * are gone either way, so leaving the operator on page 40 of a filter over a
   * list that just changed underneath them is the more confusing half-state.
   *
   * The `await` on the refetch is load bearing and must not be "optimised" back
   * to a bare `void`: every caller holds `isPerformingAction` until this resolves,
   * and that is the only thing keeping the row actions and the bulk toolbar off a
   * table that is still rendering the PRE-delete rows. `keepPreviousData` keeps
   * those rows, their indexes and their `loadedKeyCount` on screen for the whole
   * refetch — four sequential requests on a 2000-key channel — so an action
   * dispatched inside that window carries stale indexes against the renumbered
   * list, and `expected_key_count` cannot catch it once the count has caught up.
   */
  const resyncAfterDelete = useCallback(async () => {
    // Read before `discardKeyIndexedState()`'s `clearResults()` drops them, same
    // as in the invalidation effect: a row test unrelated to the deleted keys
    // dies here too, and the action's own success toast makes that look like
    // nothing happened to it.
    const wasBatchTesting = isBatchTesting
    const wasRowTestInFlight = testingIndexes.size > 0

    // Everything index-keyed goes here, and it goes BEFORE the refetch, never
    // after: the point is to invalidate captures the moment they become suspect,
    // not once the fresh list has landed and made the stale indexes look
    // plausible again.
    discardKeyIndexedState()
    // The two below are deliberately NOT in that function: neither names a key
    // index, so neither is unsafe, and the effect above deliberately leaves both
    // standing. They are dropped only on this path, for the reason in the doc
    // comment — the verdicts a filtered, deep-paged view was narrowed around are
    // gone either way.
    setStatusFilter(null)
    resetPageIndex()
    notifyTestStoppedByKeyListChange(wasBatchTesting, wasRowTestInFlight)
    await refetch()
  }, [
    discardKeyIndexedState,
    isBatchTesting,
    notifyTestStoppedByKeyListChange,
    refetch,
    resetPageIndex,
    testingIndexes,
  ])

  const handleBulkEnable = useCallback(
    async (keyIndexes: number[]): Promise<boolean> => {
      setIsPerformingAction(true)
      try {
        const response = await enableKeys(
          channelId,
          keyIndexes,
          expectedKeyCount
        )
        if (!response.success) {
          handleServerError(response, undefined, {
            title: getMultiKeyFailureMessage(response, t, {
              isDeleteAction: false,
            }),
          })
          // The precondition rejected the whole batch because the server's key
          // COUNT no longer matches the snapshot these rows came from, so those
          // rows have to go the same way the delete path sends them: leaving
          // them up only earns the same rejection on the retry.
          if (response.error_code === 'key_list_changed') {
            await resyncAfterDelete()
          }
          return false
        }

        toast.success(
          t(
            'Enabled {{affected}} of {{requested}} selected keys ({{skipped}} were already enabled)',
            {
              affected: response.data?.affected_count ?? keyIndexes.length,
              requested: keyIndexes.length,
              skipped: response.data?.skipped_count ?? 0,
            }
          )
        )
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        await refetch()
        return true
      } catch (_error: unknown) {
        handleServerError(_error, t('Operation failed'))
        return false
      } finally {
        setIsPerformingAction(false)
      }
    },
    [channelId, expectedKeyCount, queryClient, refetch, resyncAfterDelete, t]
  )

  const handleBulkDisable = useCallback(
    async (keyIndexes: number[], reason: string): Promise<boolean> => {
      setIsPerformingAction(true)
      try {
        const response = await disableKeys(
          channelId,
          keyIndexes,
          reason,
          expectedKeyCount
        )
        if (!response.success) {
          handleServerError(response, undefined, {
            title: getMultiKeyFailureMessage(response, t, {
              isDeleteAction: false,
            }),
          })
          // Same as the enable path, and the stakes are higher: `disable_keys`
          // stamps the reason the operator reviewed onto whatever keys now sit at
          // those indexes, so a rejected batch must not leave the numbering it was
          // built on standing.
          if (response.error_code === 'key_list_changed') {
            await resyncAfterDelete()
          }
          return false
        }

        toast.success(
          t(
            'Disabled {{affected}} of {{requested}} selected keys ({{skipped}} were already disabled)',
            {
              affected: response.data?.affected_count ?? keyIndexes.length,
              requested: keyIndexes.length,
              skipped: response.data?.skipped_count ?? 0,
            }
          )
        )
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        await refetch()
        return true
      } catch (_error: unknown) {
        handleServerError(_error, t('Operation failed'))
        return false
      } finally {
        setIsPerformingAction(false)
      }
    },
    [channelId, expectedKeyCount, queryClient, refetch, resyncAfterDelete, t]
  )

  const handleBulkDelete = useCallback(
    async (keyIndexes: number[]): Promise<boolean> => {
      // Defensive: the toolbar already disables the button and its confirm.
      if (!canEditSensitive) return false
      // Same: the snapshot provably omits keys, so an irreversible batch must
      // not be authorised from it. No longer a server-side impossibility —
      // expected_key_count carries the channel-wide count, which does match —
      // which is exactly why the client has to hold this line itself.
      if (isKeyListTruncated) return false
      // Same again: a verdict is still on its way to an index, and delete
      // renumbers every later key, so it would land on a different one.
      // enableRowSelection cannot cover this — it only blocks new toggles, so a
      // row selected before its spinner appeared stays selected.
      if (isBatchTesting || isRowTestInFlight) return false

      setIsPerformingAction(true)
      try {
        // expected_key_count is the concurrency precondition: a batch computed on
        // a stale list is rejected whole rather than deleting the wrong keys. It
        // is a COUNT comparison — see `expectedKeyCount` for what that misses.
        const response = await deleteKeys(
          channelId,
          keyIndexes,
          expectedKeyCount
        )
        if (!response.success) {
          handleServerError(response, undefined, {
            title: getMultiKeyFailureMessage(response, t, {
              isDeleteAction: true,
            }),
          })
          await resyncAfterDelete()
          return false
        }

        toast.success(
          t('Deleted {{affected}} of {{requested}} selected keys', {
            affected: response.data?.affected_count ?? keyIndexes.length,
            requested: keyIndexes.length,
          })
        )
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        await resyncAfterDelete()
        return true
      } catch (error) {
        handleServerError(error, undefined, {
          title: t(
            'The delete may or may not have been applied. The key list has been reloaded, please check it before retrying.'
          ),
        })
        await resyncAfterDelete()
        return false
      } finally {
        setIsPerformingAction(false)
      }
    },
    [
      canEditSensitive,
      channelId,
      expectedKeyCount,
      isBatchTesting,
      isKeyListTruncated,
      isRowTestInFlight,
      queryClient,
      resyncAfterDelete,
      t,
    ]
  )

  const performAction = useCallback(async () => {
    if (!confirmAction) return
    if (
      !canEditSensitive &&
      (confirmAction.type === 'delete' ||
        confirmAction.type === 'delete-disabled')
    ) {
      setConfirmAction(null)
      return
    }

    setIsPerformingAction(true)
    try {
      const { type, keyIndex } = confirmAction
      let response:
        | {
            success: boolean
            message?: string
            error_code?: MultiKeyBatchErrorCode
          }
        | undefined

      // The three row actions carry the same precondition as the batches, and
      // for a sharper reason: this dialog can sit open for hours, and one stale
      // row index applied to a renumbered list disables — or permanently
      // deletes — a key the operator never picked. Channel-wide count, never
      // `loadedKeyCount`; see `expectedKeyCount`.
      if (type === 'enable' && keyIndex !== undefined) {
        response = await enableMultiKey(channelId, keyIndex, expectedKeyCount)
      } else if (type === 'disable' && keyIndex !== undefined) {
        // Stays on disable_key so a single-key disable keeps being audited as
        // one, not as a batch of one. The reason the admin just reviewed rides
        // along; an empty one is omitted so the server's own default applies.
        response = await disableMultiKey(
          channelId,
          keyIndex,
          singleDisableReason.trim() || undefined,
          expectedKeyCount
        )
      } else if (type === 'delete' && keyIndex !== undefined) {
        response = await deleteMultiKey(channelId, keyIndex, expectedKeyCount)
      } else if (type === 'enable-all') {
        response = await enableAllMultiKeys(channelId)
      } else if (type === 'disable-all') {
        response = await disableAllMultiKeys(channelId)
      } else if (type === 'delete-disabled') {
        response = await deleteDisabledMultiKeys(channelId)
      }

      const isDeleteAction = type === 'delete' || type === 'delete-disabled'
      if (response?.success) {
        toast.success(getServerMessage(response) || t('Operation successful'))
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        if (isDeleteAction) {
          await resyncAfterDelete()
        } else {
          await refetch()
        }
      } else {
        handleServerError(response, undefined, {
          title: response
            ? getMultiKeyFailureMessage(response, t, { isDeleteAction })
            : t('Operation failed'),
        })
        // A delete that came back as a business failure leaves the local list
        // untrustworthy in exactly the same way as a transport error, and a
        // key_list_changed rejection — reachable from every row action now that
        // they all carry the precondition — says outright that the count moved.
        // Either way the index-keyed verdicts on screen may name other keys, so
        // the resync the copy above has just promised has to actually run.
        if (isDeleteAction || response?.error_code === 'key_list_changed') {
          await resyncAfterDelete()
        } else if (response?.error_code === 'key_already_disabled') {
          // This rejection *is* the news that the row was showing stale state:
          // the key is already disabled, almost certainly by live traffic, and
          // carries an upstream reason and timestamp the dialog has never seen.
          await refetch()
        }
      }
    } catch (_error: unknown) {
      handleServerError(_error, t('Operation failed'))
      if (
        confirmAction.type === 'delete' ||
        confirmAction.type === 'delete-disabled'
      ) {
        await resyncAfterDelete()
      }
    } finally {
      setIsPerformingAction(false)
      setConfirmAction(null)
    }
  }, [
    canEditSensitive,
    channelId,
    confirmAction,
    expectedKeyCount,
    queryClient,
    refetch,
    resyncAfterDelete,
    singleDisableReason,
    t,
  ])

  const confirmDesc = useMemo(() => {
    if (!confirmAction) return ''

    if (confirmAction.type === 'enable-all') {
      return (
        <>
          {t(
            'Enable all {{count}} keys of this channel? The current filter and selection are ignored.',
            { count: snapshot?.channelKeyCount ?? loadedKeyCount }
          )}
          <br />
          {t(
            'This clears the recorded failure reason of {{count}} disabled keys, {{auto}} of which the gateway disabled automatically. Those upstream diagnostics cannot be recovered.',
            { count: disabledKeyCount, auto: snapshot?.autoDisabledCount ?? 0 }
          )}
        </>
      )
    }
    if (confirmAction.type === 'disable-all') {
      return t(
        'Disable all {{count}} enabled keys of this channel? The current filter and selection are ignored.',
        { count: snapshot?.enabledCount ?? 0 }
      )
    }
    if (confirmAction.type === 'delete-disabled') {
      return (
        <>
          {t(
            'Delete all {{count}} auto-disabled keys of this channel? The current filter and selection are ignored.',
            { count: snapshot?.autoDisabledCount ?? 0 }
          )}{' '}
          {t('This action cannot be undone.')}
        </>
      )
    }
    return t(getMultiKeyConfirmMessage(confirmAction))
  }, [confirmAction, disabledKeyCount, loadedKeyCount, snapshot, t])

  const isChannelWideBlocked =
    selectedCount > 0 || hasFilter || isBatchTesting || isPerformingAction
  let channelWideHint = ''
  // Comes first so the two conditions the admin can act on right now still win
  // the tooltip. The gate itself stays: a channel-wide mutation mid-run would
  // rewrite the very statuses the run's own drift diff is compared against, so
  // the post-run "disabled by live traffic" notice would start lying.
  if (isBatchTesting) {
    channelWideHint = t(
      'Wait for the batch test to finish to act on all keys of this channel at once'
    )
  }
  if (hasFilter) {
    channelWideHint = t(
      'Clear the filters to act on all keys of this channel at once'
    )
  }
  if (selectedCount > 0) {
    channelWideHint = t(
      'Clear the selection to act on all keys of this channel at once'
    )
  }

  // Same "most permanent last" idiom as the row actions' delete hint: a failed
  // load outlasts a running row test, and it is the reason the operator cannot
  // resolve by waiting, so it wins the tooltip on both test triggers.
  let testTriggerHint: string | undefined
  if (isRowTestInFlight) {
    testTriggerHint = t('Wait for the running key test to finish')
  }
  if (isTestBlockedByLoadError) {
    testTriggerHint = testBlockedByLoadErrorMessage
  }

  return (
    <>
      <Dialog
        open={open}
        onOpenChange={handleDialogOpenChange}
        disablePointerDismissal={isBatchTesting}
        title={
          <>
            {t('Multi-Key Management')}
            <StatusBadge
              label={currentRow.name}
              variant='neutral'
              copyable={false}
            />
            {currentRow.channel_info?.multi_key_mode && (
              <StatusBadge
                label={
                  currentRow.channel_info.multi_key_mode === 'random'
                    ? t('Random')
                    : t('Polling')
                }
                variant='neutral'
                copyable={false}
              />
            )}
          </>
        }
        description={t(
          'Manage multi-key status and configuration for this channel'
        )}
        contentClassName='flex max-h-[min(90dvh,var(--dialog-available-height))] max-w-5xl flex-col'
        titleClassName='flex items-center gap-2'
        contentHeight='min(72vh, 720px)'
        bodyClassName='flex h-full min-h-0 flex-col overflow-hidden'
      >
        <div
          className='flex h-full min-h-0 flex-col gap-4 overflow-hidden'
          onKeyDownCapture={handleEscapeCapture}
        >
          {/* Statistics: the three counters are channel-wide, so the
              denominator has to be the channel-wide key count too — pairing
              them with the loaded count renders "Enabled 5800 of 5000" as soon
              as the channel holds more keys than the load cap.

              Hidden entirely in case A. With no snapshot every count and every
              denominator falls back to 0, and "Enabled 0 of 0" three times over
              is not a neutral placeholder — it is a positive claim that the
              channel has no keys, made by a dialog that has just failed to find
              out. The error state below says the true thing instead. */}
          {!hasUnloadableKeyList && (
            <div className='grid shrink-0 grid-cols-3 gap-3'>
              <StatisticsCard
                label={t('Enabled')}
                count={snapshot?.enabledCount ?? 0}
                total={snapshot?.channelKeyCount ?? loadedKeyCount}
              />
              <StatisticsCard
                label={t('Manual Disabled')}
                count={snapshot?.manualDisabledCount ?? 0}
                total={snapshot?.channelKeyCount ?? loadedKeyCount}
              />
              <StatisticsCard
                label={t('Auto Disabled')}
                count={snapshot?.autoDisabledCount ?? 0}
                total={snapshot?.channelKeyCount ?? loadedKeyCount}
              />
            </div>
          )}

          <Separator className='shrink-0' />

          {/* Filters */}
          <div className='flex shrink-0 flex-wrap items-center gap-2'>
            <Select
              items={MULTI_KEY_FILTER_OPTIONS.map((option) => ({
                value: option.value,
                label: t(option.label),
              }))}
              value={
                statusFilter === null ? ALL_STATUSES : String(statusFilter)
              }
              onValueChange={handleStatusFilterChange}
            >
              <SelectTrigger className='w-40'>
                <SelectValue placeholder={t('All Status')} />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {MULTI_KEY_FILTER_OPTIONS.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {t(option.label)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>

            <Select
              items={bucketItems.map((item) => ({
                value: item.value,
                label: item.label,
              }))}
              value={
                bucketFilter ? getTestBucketValue(bucketFilter) : ALL_BUCKETS
              }
              onValueChange={handleBucketFilterChange}
              disabled={testResultCount === 0}
            >
              <SelectTrigger
                className='w-64'
                title={
                  testResultCount === 0
                    ? t('Test some keys first to filter by status code')
                    : undefined
                }
              >
                <SelectValue placeholder={t('All Test Results')} />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  {bucketItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>

            {hasFilter && (
              <Button variant='ghost' size='sm' onClick={clearFilters}>
                {t('Clear filters')}
              </Button>
            )}

            {isRefreshing && (
              <Spinner className='text-muted-foreground' aria-hidden='true' />
            )}

            {snapshot && (
              <span className='text-muted-foreground text-xs'>
                {t('Status updated at {{time}}', {
                  time: new Date(snapshot.fetchedAt).toLocaleTimeString(
                    undefined,
                    { hour: '2-digit', minute: '2-digit' }
                  ),
                })}
              </span>
            )}
          </div>

          {/* Test + channel-wide actions */}
          <div className='flex shrink-0 flex-wrap items-center gap-2'>
            {isBatchTesting ? (
              <Button
                variant='outline'
                size='sm'
                onClick={stop}
                disabled={isStopRequested}
              >
                {isStopRequested ? t('Stopping...') : t('Stop')}
              </Button>
            ) : (
              /* Hidden rather than disabled in case A, and only there: both
                 labels interpolate a count that is 0 because nothing is known,
                 so "Test all 0 keys" would repeat the empty-channel claim the
                 statistics above were just pulled for. In case B the counts were
                 really observed — just possibly out of date — so those buttons
                 stay visible and go disabled with the reason in their tooltip. */
              !hasUnloadableKeyList && (
                <>
                  <Button
                    size='sm'
                    onClick={() =>
                      setTestConfirm(keys.map((keyStatus) => keyStatus.index))
                    }
                    disabled={
                      loadedKeyCount === 0 ||
                      isPerformingAction ||
                      isRowTestInFlight ||
                      isTestBlockedByLoadError
                    }
                    title={testTriggerHint}
                  >
                    <Gauge data-icon='inline-start' />
                    {t('Test all {{count}} keys', { count: loadedKeyCount })}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={handleTestSelected}
                    disabled={
                      selectedCount === 0 ||
                      isPerformingAction ||
                      isRowTestInFlight ||
                      isTestBlockedByLoadError
                    }
                    title={testTriggerHint}
                  >
                    {t('Test {{count}} selected keys', {
                      count: selectedCount,
                    })}
                  </Button>
                </>
              )
            )}

            <Button
              variant='outline'
              size='sm'
              onClick={handleCopyResults}
              disabled={testResultCount === 0}
            >
              <Copy data-icon='inline-start' />
              {t('Copy results')}
            </Button>

            <Button
              variant='outline'
              size='sm'
              onClick={() => void refetch()}
              disabled={isFetching}
              aria-label={t('Refresh')}
              title={t('Refresh')}
            >
              <RefreshCw className='h-4 w-4' />
            </Button>

            <div className='ms-auto flex flex-wrap items-center gap-2'>
              {disabledKeyCount > 0 && (
                <Button
                  variant='default'
                  size='sm'
                  onClick={() => setConfirmAction({ type: 'enable-all' })}
                  disabled={isChannelWideBlocked}
                  title={channelWideHint || undefined}
                >
                  <Power data-icon='inline-start' />
                  {t('Enable all keys of this channel')}
                </Button>
              )}

              {(snapshot?.enabledCount ?? 0) > 0 && (
                <Button
                  variant='destructive'
                  size='sm'
                  onClick={() => setConfirmAction({ type: 'disable-all' })}
                  disabled={isChannelWideBlocked}
                  title={channelWideHint || undefined}
                >
                  <PowerOff data-icon='inline-start' />
                  {t('Disable all enabled keys of this channel')}
                </Button>
              )}

              {(snapshot?.autoDisabledCount ?? 0) > 0 && (
                <Button
                  variant='destructive'
                  size='sm'
                  onClick={() => {
                    if (!canEditSensitive) return
                    setConfirmAction({ type: 'delete-disabled' })
                  }}
                  disabled={isChannelWideBlocked || !canEditSensitive}
                  title={
                    canEditSensitive
                      ? channelWideHint || undefined
                      : t('No permission to perform this action')
                  }
                >
                  <Trash2 data-icon='inline-start' />
                  {t('Delete all auto-disabled keys of this channel')}
                </Button>
              )}
            </div>
          </div>

          {!canEditSensitive && (
            <p className='text-muted-foreground shrink-0 text-xs'>
              {t('No permission to perform this action')}
            </p>
          )}

          {/* Case B's banner, first because it outranks the other two: those
              describe a list that loaded, this one says the list on screen is
              not the server's current answer at all. Structure and classes are
              the grew-during-load banner's, verbatim — a warning paragraph the
              body scrolls with plus its Refresh, no new chrome. A banner rather
              than only a toast because the load-error toast is gone five seconds
              later (`duration={5000}` in `src/routes/__root.tsx`) while the
              stale rows, the stale counters and the stale "Status updated at"
              stay up indefinitely; and Refresh belongs on it because retrying
              the load is the only thing that clears this state. */}
          {hasStaleKeyListBehindError && (
            <div
              className='flex shrink-0 flex-wrap items-center gap-2'
              role='status'
            >
              <p className='text-warning text-xs'>
                {testBlockedByLoadErrorMessage}
              </p>
              <Button
                variant='outline'
                size='sm'
                onClick={() => void refetch()}
                disabled={isFetching}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Refresh')}
              </Button>
            </div>
          )}

          {isKeyListTruncated && snapshot && (
            <p className='text-warning shrink-0 text-xs' role='status'>
              {t(
                'Loaded the first {{loaded}} of {{total}} keys; testing and filtering only apply to the loaded ones, and deleting selected keys is unavailable because it must be checked against the whole list.',
                { loaded: loadedKeyCount, total: snapshot.channelKeyCount }
              )}
            </p>
          )}

          {/* Near-dead code, deliberately: `fetchMultiKeyStatus` pins `total` on
              page 1 and restarts the walk on ANY move, rise included, so a plain
              mid-walk append is answered with a restart and never reaches
              `grewDuringLoad`. Only the `total_pages` bad-value case still gets
              here. Kept because the flag is genuinely reachable and because
              deleting a user-facing string costs a new key across seven locales
              to put back — see the field's own comment on the snapshot type. */}
          {didKeyListGrowDuringLoad && snapshot && (
            <div
              className='flex shrink-0 flex-wrap items-center gap-2'
              role='status'
            >
              <p className='text-warning text-xs'>
                {t(
                  'This channel gained keys while the list was loading, so only {{loaded}} of {{total}} keys are shown. Refresh to load the rest.',
                  {
                    loaded: loadedKeyCount,
                    total: snapshot.channelKeyCount,
                  }
                )}
              </p>
              <Button
                variant='outline'
                size='sm'
                onClick={() => void refetch()}
                disabled={isFetching}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Refresh')}
              </Button>
            </div>
          )}

          {progress && (
            <div className='shrink-0 rounded-md border p-3'>
              <Progress
                value={
                  progress.total > 0
                    ? (progress.completed / progress.total) * 100
                    : 0
                }
              >
                <ProgressLabel>
                  {isStopRequested
                    ? t('Stopping batch test...')
                    : t('Batch testing keys...')}
                </ProgressLabel>
                <ProgressValue>
                  {() =>
                    t('{{completed}}/{{total}} completed', {
                      completed: progress.completed,
                      total: progress.total,
                    })
                  }
                </ProgressValue>
              </Progress>
              <div className='mt-2 flex flex-wrap items-center justify-between gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {t('{{success}} succeeded, {{failed}} failed', {
                    success: progress.success,
                    failed: progress.failed,
                  })}
                  {' · '}
                  {t('{{seconds}}s elapsed', {
                    seconds: Math.floor(elapsedMs / 1000),
                  })}
                </span>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={stop}
                  disabled={isStopRequested}
                >
                  {isStopRequested ? t('Stopping...') : t('Stop')}
                </Button>
              </div>
            </div>
          )}

          {/* The floating bulk toolbar is `fixed bottom-6`; the padding is
              unconditional so rows never jump 5rem under the cursor when the
              first checkbox is ticked */}
          <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-hidden pb-20'>
            {hasUnloadableKeyList ? (
              /* Case A. This REPLACES the table rather than sitting above it:
                 `isPending` is false once the error lands, so the table skips
                 its skeleton and renders "No keys found" — and a banner reading
                 "could not be loaded" directly above the words "No keys found"
                 is not a warning, it is a contradiction the operator has to
                 pick a side of. There is no pagination and no bulk toolbar here
                 either; both would be furniture for rows that do not exist, and
                 the toolbar only shows itself once rows are selected anyway.
                 Refresh is the one control that belongs, and it is the same
                 button the grew-during-load banner uses. */
              <div
                className='flex min-h-0 flex-1 flex-col items-center justify-center gap-3 rounded-md border p-6 text-center'
                role='status'
              >
                <p className='text-warning max-w-prose text-sm'>
                  {t(
                    'The key list of this channel could not be loaded. Nothing is shown below — this does not mean the channel has no keys.'
                  )}
                </p>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => void refetch()}
                  disabled={isFetching}
                >
                  <RefreshCw data-icon='inline-start' />
                  {t('Refresh')}
                </Button>
              </div>
            ) : (
              <>
                <DataTableView
                  table={table}
                  isLoading={isPending}
                  splitHeader
                  containerClassName='min-h-0 flex-1 rounded-md'
                  tableContainerClassName='h-full min-h-0'
                  splitHeaderScrollClassName='h-full'
                  containerProps={{
                    role: 'region',
                    'aria-label': t('Multi-Key Management'),
                    'aria-busy': isFetching,
                  }}
                  getColumnClassName={(columnId) =>
                    getMultiKeyColumnClass(columnId)
                  }
                  emptyContent={
                    hasFilter ? (
                      <div className='flex flex-col items-center gap-2'>
                        <span>{t('No keys match the current filters')}</span>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={clearFilters}
                        >
                          {t('Clear filters')}
                        </Button>
                      </div>
                    ) : (
                      t('No keys found')
                    )
                  }
                  emptyCellClassName='text-muted-foreground h-24 text-center text-sm'
                />

                <div className='shrink-0'>
                  <DataTablePagination table={table} />
                </div>

                <MultiKeyBulkActions
                  table={table}
                  canEditSensitive={canEditSensitive}
                  isTestInFlight={isBatchTesting || isRowTestInFlight}
                  isPerformingAction={isPerformingAction}
                  isRefreshing={isFetching}
                  keyListToken={keyListToken}
                  loadedKeyCount={loadedKeyCount}
                  isKeyListTruncated={isKeyListTruncated}
                  didKeyListGrow={didKeyListGrowDuringLoad}
                  testResultCount={testResultCount}
                  defaultDisableReason={defaultDisableReason}
                  sharedNoResponseError={sharedNoResponseError}
                  onPendingDecisionChange={handleBulkPendingDecisionChange}
                  onEnableKeys={handleBulkEnable}
                  onDisableKeys={handleBulkDisable}
                  onDeleteKeys={handleBulkDelete}
                />
              </>
            )}
          </div>
        </div>
      </Dialog>

      {/* Single-key and channel-wide confirmations */}
      <ConfirmDialog
        open={confirmAction !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen && !isPerformingAction) setConfirmAction(null)
        }}
        title={t('Confirm Action')}
        desc={confirmDesc}
        destructive={isDestructiveAction(confirmAction)}
        isLoading={isPerformingAction}
        handleConfirm={() => void performAction()}
      >
        {confirmAction?.type === 'disable' && (
          <div className='grid gap-2'>
            <Label htmlFor='multi-key-single-disable-reason'>
              {t('Reason')}
            </Label>
            <Input
              id='multi-key-single-disable-reason'
              value={singleDisableReason}
              maxLength={MULTI_KEY_REASON_MAX_LENGTH}
              onChange={(event) => setSingleDisableReason(event.target.value)}
            />
          </div>
        )}
      </ConfirmDialog>

      {/* Test confirmation: it needs interpolation, which the shared
          getMultiKeyConfirmMessage channel cannot carry */}
      <ConfirmDialog
        open={testConfirm !== null}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setTestConfirm(null)
        }}
        title={t('Confirm Action')}
        className='max-w-md'
        desc={
          <>
            {t('Test {{count}} keys of this channel with model {{model}}?', {
              count: testConfirm?.length ?? 0,
              model: testModelLabel,
            })}
            <br />
            {t(
              'Each key sends one real upstream request. Only keys that pass are billed, and you can stop the run at any time.'
            )}
          </>
        }
        confirmText={t('Test')}
        handleConfirm={() => {
          if (testConfirm) startTest(testConfirm)
        }}
      />

      {/* Closing throws away results that cost real upstream calls */}
      <ConfirmDialog
        open={isDiscardConfirmOpen}
        onOpenChange={setIsDiscardConfirmOpen}
        title={t('Confirm Action')}
        desc={t(
          'Discard the test results of {{count}} keys? They cannot be recovered without testing again.',
          { count: testResultCount }
        )}
        destructive
        confirmText={t('Close')}
        handleConfirm={confirmDiscardAndClose}
      />

      {/* On-demand full upstream body of one failure */}
      <KeyErrorDetailsSheet
        detail={errorDetail}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setErrorDetail(null)
        }}
      />
    </>
  )
}

/**
 * One cell of the Upstream status column. The full upstream body is deliberately
 * not rendered inline: it can be kilobytes long, so the cell shows the first line
 * only (whitespace collapsed, truncated, clamped to two lines) and offers a
 * Details button that opens the whole body in the on-demand sheet, where it can
 * also be copied verbatim.
 *
 * @param props - Resolved bucket, the stored outcome behind it, and the details callback.
 * @returns The badge and, on failure, the summary line plus its Details button.
 */
function KeyTestResultCell({
  keyIndex,
  bucket,
  outcome,
  onOpenDetails,
}: {
  keyIndex: number
  bucket: TestBucket
  outcome?: ChannelTestOutcome
  onOpenDetails: (detail: KeyErrorDetail) => void
}) {
  const { t } = useTranslation()

  if (bucket.kind === 'testing') {
    return (
      <StatusBadge variant='info' copyable={false}>
        <Spinner className='size-3.5 shrink-0' />
        <span className='min-w-0 truncate leading-normal'>
          {t('Testing...')}
        </span>
      </StatusBadge>
    )
  }

  if (bucket.kind === 'untested') {
    return (
      <StatusBadge label={t('Not tested')} variant='neutral' copyable={false} />
    )
  }

  if (bucket.kind === 'success') {
    return (
      <StatusBadge label={t('Success')} variant='success' copyable={false} />
    )
  }

  // Shared with the channel test dialog's failure cell. `details` comes back
  // undefined when the summary already is the whole body, which is exactly when
  // a Details button would open a sheet showing nothing new. The model-price
  // branch belongs to that dialog's own error code, so it is switched off here.
  const { summary, details } = getFailureStatusDisplay({
    errorText: outcome?.error,
    fallbackSummary: t('Test failed'),
    isModelPriceError: false,
    modelPriceSummary: '',
  })

  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <StatusBadge
        label={
          bucket.kind === 'http-error'
            ? t('HTTP {{code}}', { code: bucket.code })
            : t('No response')
        }
        variant={bucket.kind === 'http-error' ? 'danger' : 'warning'}
        copyable={false}
      />
      <p className='text-muted-foreground line-clamp-2 min-w-0 text-xs leading-snug wrap-break-word'>
        {summary}
      </p>
      {details && (
        <Button
          variant='ghost'
          size='sm'
          className='w-fit'
          aria-haspopup='dialog'
          onClick={() => onOpenDetails({ keyIndex, summary, details })}
        >
          <Info data-icon='inline-start' />
          {t('Details')}
        </Button>
      )}
    </div>
  )
}

/**
 * On-demand panel for one key's full upstream body. The table cell can only
 * afford a two-line summary, and the Copy results action flattens every row to a
 * single tab-separated line, so this is the only place the whole body — newlines
 * included — can be read and copied.
 *
 * @param props - The captured failure, or null while the sheet is closed.
 * @returns The details sheet.
 */
function KeyErrorDetailsSheet({
  detail,
  onOpenChange,
}: {
  detail: KeyErrorDetail | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })

  return (
    <Sheet open={Boolean(detail)} onOpenChange={onOpenChange}>
      <SheetContent
        side={isMobile ? 'bottom' : 'right'}
        className={
          isMobile
            ? sideDrawerContentClassName('h-auto max-h-[85dvh] rounded-t-xl')
            : sideDrawerContentClassName('sm:max-w-lg')
        }
      >
        {detail && (
          <>
            <SheetHeader className={sideDrawerHeaderClassName('sm:px-5')}>
              <SheetTitle className='pr-10'>
                {t('Upstream response of key #{{index}}', {
                  index: detail.keyIndex + 1,
                })}
              </SheetTitle>
              <SheetDescription className='pr-10 wrap-break-word'>
                {detail.summary}
              </SheetDescription>
            </SheetHeader>
            <div className={sideDrawerFormClassName('gap-4 sm:px-5')}>
              <section className='space-y-2'>
                <div className='text-muted-foreground text-xs font-medium'>
                  {t('Details')}
                </div>
                <pre className='bg-muted/30 text-muted-foreground m-0 max-w-full rounded-md border p-3 text-xs leading-relaxed wrap-break-word whitespace-pre-wrap'>
                  {detail.details}
                </pre>
              </section>
            </div>
            <SheetFooter className={sideDrawerFooterClassName('sm:px-5')}>
              <Button
                variant='outline'
                className='w-full sm:w-auto'
                onClick={() => void copyToClipboard(detail.details)}
              >
                {copiedText === detail.details ? (
                  <Check data-icon='inline-start' className='text-success' />
                ) : (
                  <Copy data-icon='inline-start' />
                )}
                {t('Copy')}
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
