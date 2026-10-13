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
import type { Table } from '@tanstack/react-table'
import { Power, PowerOff, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { MULTI_KEY_REASON_MAX_LENGTH } from '../../lib/multi-key-utils'
import {
  getFirstErrorLine,
  normalizeInlineError,
  truncateFailureSummary,
} from '../../lib/test-result-display'
import type { KeyStatus } from '../../types'

/** One dispatched batch action, with its payload captured at click time. */
type MultiKeyBulkAction =
  | { kind: 'enable'; keyIndexes: number[] }
  | { kind: 'disable'; keyIndexes: number[]; reason: string }
  | { kind: 'delete'; keyIndexes: number[] }

/** The two actions that go through a confirmation step before dispatching. */
type MultiKeyBulkConfirm = {
  kind: 'disable' | 'delete'
  keyIndexes: number[]
}

type MultiKeyBulkActionsProps = {
  /** Loaded keys table; the payload is read from its selection at click time. */
  table: Table<KeyStatus>
  /** Whether the admin holds channel sensitive-write, i.e. may delete keys. */
  canEditSensitive: boolean
  /**
   * True while a key test is in flight, either the batch run or a single row's.
   * Blocks Delete only, since only delete reindexes the remaining keys: Enable
   * and Disable leave indexes untouched, so gating them would be pointless
   * friction while a test runs.
   */
  isTestInFlight: boolean
  /** True while any other dialog mutation is in flight; blocks every button. */
  isPerformingAction: boolean
  /** Changes whenever the key list may have been renumbered; any open confirmation is dropped. */
  keyListToken: number
  /** A key-status refetch is in flight, so the rows on screen may be stale. */
  isRefreshing: boolean
  /** Keys currently loaded; selecting all of them makes Delete refuse locally. */
  loadedKeyCount: number
  /**
   * True when the load cap stopped short of the channel's real key count. Delete
   * is blocked because the loaded rows provably omit keys, so an irreversible
   * batch must not be built from them — and nothing server-side would catch it:
   * `expected_key_count` carries the channel-wide count, which does match.
   */
  isKeyListTruncated: boolean
  /**
   * True when the channel gained keys while the list was loading, so the loaded
   * rows are only a stale prefix of it. Selecting every loaded row then no
   * longer means selecting every key of the channel, which changes what the
   * "delete every key" confirmation should advise: warning copy only.
   */
  didKeyListGrow: boolean
  /** Stored per-key test results a delete would invalidate; warning copy only. */
  testResultCount: number
  /** Pre-filled, editable disable reason derived from the selected keys' buckets. */
  defaultDisableReason: string
  /**
   * The one error message every selected key failed with, set only when enough of
   * them agree on it and none got an upstream status code. Such a result is far
   * more likely to be one channel-wide fault raised before dispatch — a bad base
   * URL, an unsupported test model, a malformed credential — than N individually
   * bad keys, so both destructive confirmations say so: warning copy only.
   */
  sharedNoResponseError?: string
  /**
   * Reports whether this toolbar is currently holding a confirmation the
   * operator has NOT yet acted on.
   *
   * The confirmation below lives in this component's local state, so nothing
   * above can see it, and the `keyListToken` effect drops it in silence. The
   * parent needs to know because it is the one that speaks: its
   * count-invalidation effect decides whether a key-list change actually took a
   * decision away from the operator, and the most destructive modal in the
   * feature — "Delete N selected keys of this channel? This action cannot be
   * undone." — otherwise vanishes under the cursor with nothing said, which
   * reads as "the deletion went through".
   *
   * `isDispatching` is part of the condition, not an implementation detail: once
   * the operator has confirmed, the batch is in flight and there is no pending
   * decision left to lose. The parent's notice for this case ends in "Nothing was
   * changed.", a claim it must not make about an action that is landing, so a
   * dispatching toolbar reports `false` and lets the dispatch's own result speak.
   *
   * @param hasPendingDecision - True while a confirmation is open and idle.
   */
  onPendingDecisionChange: (hasPendingDecision: boolean) => void
  /** Resolves true when the server accepted the action, so the selection can clear. */
  onEnableKeys: (keyIndexes: number[]) => Promise<boolean>
  onDisableKeys: (keyIndexes: number[], reason: string) => Promise<boolean>
  onDeleteKeys: (keyIndexes: number[]) => Promise<boolean>
}

/**
 * Floating bulk actions bar for the multi-key management dialog: Enable /
 * Disable / Delete over the selected keys.
 *
 * MOUNTING: render this as the LAST CHILD of the dialog body's
 * `min-h-0 flex-1 flex-col` column (the way `channel-test-dialog.tsx` mounts its
 * own bulk actions), never as a sibling of `<Dialog>`. The shared toolbar is
 * `fixed bottom-6` and the row selection is not cleared on close, so a sibling
 * would keep floating over the channel list page after the dialog is gone.
 * Inside the body it unmounts together with the popup, because Base UI's
 * `DialogPortal` is not kept mounted.
 *
 * The payload is read from `getFilteredSelectedRowModel()` at click time, so it
 * matches the list the user is looking at. Because the dialog pre-filters its
 * data upstream, rows that are selected but currently filtered out are not part
 * of it.
 *
 * @param props - Table instance, permission and busy flags, action callbacks.
 * @returns The floating toolbar plus its confirmation dialogs.
 */
export function MultiKeyBulkActions(props: MultiKeyBulkActionsProps) {
  const { t } = useTranslation()
  const [confirm, setConfirm] = useState<MultiKeyBulkConfirm | null>(null)
  const [reason, setReason] = useState('')
  const [isDispatching, setIsDispatching] = useState(false)

  /**
   * A captured index list is only meaningful against the key numbering it was
   * captured under. Once `keyListToken` moves — say a row Delete elsewhere in the
   * dialog landed and its refetch completed — the indexes captured when this
   * confirmation opened point at whatever now occupies those slots, while the
   * channel-wide `expected_key_count` has caught up too, so it matches and the
   * server has nothing left to refuse: confirming would delete keys the operator
   * never selected. Drop the confirmation and its edited reason instead and let
   * them reselect against the new numbering.
   *
   * Both updaters return their current value when there is nothing to drop, so a
   * token bump with no confirmation on screen is a no-op. `isDispatching` is
   * deliberately left alone: it is owned by `dispatch`'s `finally`, which runs
   * regardless of whether the confirmation is still open, so closing one out from
   * under an in-flight dispatch cannot strand the busy flag.
   */
  useEffect(() => {
    setConfirm((open) => (open === null ? open : null))
    setReason((current) => (current === '' ? current : ''))
  }, [props.keyListToken])

  /**
   * Tells the parent whether a confirmation is sitting on screen unanswered, so
   * that when it drops one via `keyListToken` above it can say so. See the prop's
   * own docs for what it does with the answer and why `isDispatching` counts.
   *
   * An effect rather than calls inside `openConfirm` / `closeConfirm` /
   * `dispatch`: `confirm` and `isDispatching` are written from eight places
   * across those three plus the token effect above, and an unmount owes a report
   * as well, so a hand-placed one would have to be repeated at every one of them
   * and kept in step with every future one — the exact drift this dialog has
   * spent ten rounds undoing. Derive the report from the state instead.
   *
   * The ordering this depends on holds, and it is worth spelling out because it
   * is the whole reason a `useEffect` is enough. Within one commit React runs
   * child passive effects before parent ones, but the two never even compete
   * here: the token bump is issued from INSIDE the parent's invalidation effect,
   * so this component cannot observe the new `props.keyListToken` — and therefore
   * cannot clear `confirm`, and therefore cannot report `false` — until a LATER
   * commit. The parent reads its ref while it still says `true`.
   *
   * The cleanup reports `false` so that unmounting with a confirmation open —
   * closing the dialog mid-decision — cannot leave the parent believing one is
   * still up. It also runs before each re-report, but the parent only ever reads
   * the ref from its own effect body, which React runs after every child effect
   * in the flush has both torn down and re-run, so that momentary `false` is
   * never observed.
   */
  // Destructured, unlike every other `props.x` read in this file, because
  // `react/exhaustive-deps` is an error here and oxlint widens a called
  // `props.onPendingDecisionChange` to a required `props` dependency — which
  // would re-run this on every parent render. The dependency set is identical.
  const { onPendingDecisionChange } = props
  useEffect(() => {
    onPendingDecisionChange(confirm !== null && !isDispatching)
    return () => onPendingDecisionChange(false)
  }, [confirm, isDispatching, onPendingDecisionChange])

  // `isRefreshing` is about the key-status REFETCH, not about a test: while the
  // rows on screen may already be stale, no batch may be dispatched against the
  // indexes read off them. Row-level Enable/Disable stay deliberately ungated on
  // a test being in flight and that rule is unchanged here.
  const isBusy = props.isPerformingAction || isDispatching || props.isRefreshing
  const canDelete =
    props.canEditSensitive && !props.isTestInFlight && !props.isKeyListTruncated
  const pendingIndexes = confirm?.keyIndexes ?? []
  // Everything is loaded client-side, so refuse "delete every key" here instead
  // of waiting for the server to answer with its own message.
  const isDeletingEveryKey =
    props.loadedKeyCount > 0 && pendingIndexes.length >= props.loadedKeyCount
  // Same refusal either way, but on a grown list the selection may not actually
  // cover the channel, so "deselect one key" is the wrong thing to ask for.
  const deletingEveryKeyMessage = props.didKeyListGrow
    ? t(
        'The key list changed while it was loading, so this selection may no longer cover every key of this channel. Refresh the key list before deleting.'
      )
    : t(
        'Cannot delete all keys of a multi-key channel. Deselect at least one key, or disable or delete the whole channel instead.'
      )

  // Shaped like the dialog's own error column — first line, whitespace collapsed,
  // capped — because an upstream body runs to kilobytes and this one is quoted
  // inline in a confirmation that cannot scroll.
  const sharedError = props.sharedNoResponseError?.trim()
  const sharedFailureWarning = sharedError ? (
    <p className='text-warning mt-2 text-xs leading-snug wrap-break-word'>
      {t(
        'All selected keys report the same error and none of them got a response from the upstream provider: "{{error}}". This usually means one channel-level problem with the base URL, the test model or the credentials format, not that this many keys are individually bad, so verify the channel configuration first.',
        {
          error: truncateFailureSummary(
            normalizeInlineError(getFirstErrorLine(sharedError) ?? sharedError)
          ),
        }
      )}
    </p>
  ) : null

  let deleteHint = t('Delete selected keys')
  if (props.isTestInFlight) {
    deleteHint = t('Cannot delete keys while a key test is running')
  }
  if (props.isKeyListTruncated) {
    deleteHint = t(
      'Cannot delete selected keys: this channel has more keys than the dialog can load'
    )
  }
  if (!props.canEditSensitive) {
    deleteHint = t('No permission to perform this action')
  }

  const collectSelectedKeyIndexes = () =>
    props.table
      .getFilteredSelectedRowModel()
      .rows.map((row) => row.original.index)

  const dispatch = async (action: MultiKeyBulkAction) => {
    if (action.keyIndexes.length === 0) {
      setConfirm(null)
      return
    }
    // Defensive: both conditions already disable the confirm button.
    if (action.kind === 'delete' && (!canDelete || isDeletingEveryKey)) {
      setConfirm(null)
      return
    }

    setIsDispatching(true)
    try {
      let succeeded = false
      if (action.kind === 'enable') {
        succeeded = await props.onEnableKeys(action.keyIndexes)
      } else if (action.kind === 'disable') {
        succeeded = await props.onDisableKeys(action.keyIndexes, action.reason)
      } else {
        succeeded = await props.onDeleteKeys(action.keyIndexes)
      }

      // A failed delete may or may not have landed upstream, so the selection is
      // dropped either way and the dialog resyncs the key list.
      if (succeeded || action.kind === 'delete') {
        props.table.resetRowSelection()
      }
    } finally {
      setIsDispatching(false)
      setConfirm(null)
    }
  }

  const openConfirm = (kind: MultiKeyBulkConfirm['kind']) => {
    if (kind === 'delete' && !canDelete) return
    if (kind === 'disable') {
      setReason(props.defaultDisableReason)
    }
    setConfirm({ kind, keyIndexes: collectSelectedKeyIndexes() })
  }

  const closeConfirm = (open: boolean) => {
    if (!open && !isDispatching) {
      setConfirm(null)
    }
  }

  return (
    <>
      <BulkActionsToolbar table={props.table} entityName={t('API key')}>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() =>
                  void dispatch({
                    kind: 'enable',
                    keyIndexes: collectSelectedKeyIndexes(),
                  })
                }
                disabled={isBusy}
                className='size-8'
                aria-label={t('Enable selected keys')}
                title={t('Enable selected keys')}
              />
            }
          >
            <Power />
            <span className='sr-only'>{t('Enable selected keys')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Enable selected keys')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => openConfirm('disable')}
                disabled={isBusy}
                className='size-8'
                aria-label={t('Disable selected keys')}
                title={t('Disable selected keys')}
              />
            }
          >
            <PowerOff />
            <span className='sr-only'>{t('Disable selected keys')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Disable selected keys')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                onClick={() => openConfirm('delete')}
                disabled={isBusy || !canDelete}
                className='size-8'
                aria-label={t('Delete selected keys')}
                title={deleteHint}
              />
            }
          >
            <Trash2 />
            <span className='sr-only'>{t('Delete selected keys')}</span>
          </TooltipTrigger>
          <TooltipContent>
            <p>{deleteHint}</p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      {/* Disable confirmation: the reason is stored on every selected key */}
      <ConfirmDialog
        open={confirm?.kind === 'disable'}
        onOpenChange={closeConfirm}
        title={t('Disable selected keys?')}
        desc={
          <>
            {t(
              'Disable {{count}} selected keys of this channel? The reason below is recorded for each of them.',
              { count: pendingIndexes.length }
            )}
            {sharedFailureWarning}
          </>
        }
        confirmText={t('Disable')}
        isLoading={isBusy}
        handleConfirm={() =>
          void dispatch({
            kind: 'disable',
            keyIndexes: pendingIndexes,
            reason: reason.trim(),
          })
        }
      >
        <div className='grid gap-2'>
          <Label htmlFor='multi-key-disable-reason'>{t('Reason')}</Label>
          <Input
            id='multi-key-disable-reason'
            value={reason}
            maxLength={MULTI_KEY_REASON_MAX_LENGTH}
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
      </ConfirmDialog>

      {/* Delete confirmation: irreversible, and it renumbers the survivors */}
      <ConfirmDialog
        open={confirm?.kind === 'delete'}
        onOpenChange={closeConfirm}
        title={t('Delete selected keys?')}
        className='max-w-md'
        desc={
          <>
            {isDeletingEveryKey ? (
              deletingEveryKeyMessage
            ) : (
              <>
                {t('Delete {{count}} selected keys of this channel?', {
                  count: pendingIndexes.length,
                })}{' '}
                {t('This action cannot be undone.')}
                {props.testResultCount > 0 && (
                  <>
                    <br />
                    {t(
                      'Deleting keys renumbers the remaining ones, so the {{count}} stored test results will be cleared.',
                      { count: props.testResultCount }
                    )}
                  </>
                )}
              </>
            )}
            {/* Outside the branch: an all-keys-identical failure is exactly the
                case where the whole channel is selected and delete refuses, and
                the advice still applies once a key is deselected. */}
            {sharedFailureWarning}
          </>
        }
        confirmText={t('Delete')}
        destructive
        disabled={isDeletingEveryKey}
        isLoading={isBusy}
        handleConfirm={() =>
          void dispatch({ kind: 'delete', keyIndexes: pendingIndexes })
        }
      />
    </>
  )
}
