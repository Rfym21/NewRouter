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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'

import type { MultiKeyConfirmAction } from '../../types'

type MultiKeyTableRowActionsProps = {
  keyIndex: number
  status: number
  canDelete: boolean
  /**
   * True while this key is in flight; owned by the caller so batch runs light it
   * up too. It gates Test (the request is already open) and Delete (deleting
   * renumbers the surviving keys, which would leave this row's still-open
   * upstream call paid for and thrown away) — never Enable/Disable.
   */
  isTesting: boolean
  /**
   * True while a batch test run is active. It gates Test (the run already covers
   * every key) and Delete (only delete renumbers the surviving keys, which would
   * make the verdicts still landing point at the wrong ones) — never
   * Enable/Disable, whose indexes are unaffected and whose whole point is to be
   * reachable the moment a verdict appears.
   */
  isBatchTesting: boolean
  /** True while another dialog mutation is in flight; blocks every action */
  isPerformingAction: boolean
  onTest: (keyIndex: number) => Promise<void>
  onAction: (action: MultiKeyConfirmAction) => void
}

/**
 * Renders the actions available for one multi-key entry.
 *
 * @param props - Key state and action callbacks.
 * @returns The row action controls.
 */
export function MultiKeyTableRowActions(props: MultiKeyTableRowActionsProps) {
  const { t } = useTranslation()
  const isEnabled = props.status === 1
  const canDelete = props.canDelete && !props.isTesting && !props.isBatchTesting

  // One hint per reason, most permanent last: a button that says nothing about
  // why it is dead is worse than no tooltip at all, and `isPerformingAction`
  // alone used to leave it silent.
  let deleteHint: string | undefined
  if (props.isPerformingAction) {
    deleteHint = t('Wait for the current action to finish')
  }
  if (props.isTesting) {
    deleteHint = t('Cannot delete this key while it is being tested')
  }
  if (props.isBatchTesting) {
    deleteHint = t('Cannot delete keys while a batch test is running')
  }
  if (!props.canDelete) {
    deleteHint = t('No permission to perform this action')
  }

  return (
    <div className='flex justify-end gap-2'>
      <Button
        variant='outline'
        size='sm'
        onClick={() => props.onTest(props.keyIndex)}
        disabled={
          props.isTesting || props.isBatchTesting || props.isPerformingAction
        }
      >
        {props.isTesting && <Spinner data-icon='inline-start' />}
        {t('Test')}
      </Button>
      {isEnabled ? (
        <Button
          variant='outline'
          size='sm'
          onClick={() =>
            props.onAction({ type: 'disable', keyIndex: props.keyIndex })
          }
          disabled={props.isPerformingAction}
        >
          {t('Disable')}
        </Button>
      ) : (
        <Button
          variant='outline'
          size='sm'
          onClick={() =>
            props.onAction({ type: 'enable', keyIndex: props.keyIndex })
          }
          disabled={props.isPerformingAction}
        >
          {t('Enable')}
        </Button>
      )}
      <Button
        variant='destructive'
        size='sm'
        onClick={() => {
          if (!canDelete) return
          props.onAction({ type: 'delete', keyIndex: props.keyIndex })
        }}
        disabled={!canDelete || props.isPerformingAction}
        title={deleteHint}
      >
        {t('Delete')}
      </Button>
    </div>
  )
}
