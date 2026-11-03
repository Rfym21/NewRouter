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
import type { ColumnDef } from '@tanstack/react-table'
import { useCallback, useMemo, useState } from 'react'

import { MaskedLogContent } from '../components/masked-log-content'

function getColumnId<TData>(column: ColumnDef<TData>): string | undefined {
  if (column.id) return column.id
  if ('accessorKey' in column) {
    return String(column.accessorKey).replaceAll('.', '_')
  }
  return undefined
}

function readMaskedColumns(storageKey: string): Set<string> {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(storageKey) ?? '[]')
    return new Set(
      Array.isArray(value)
        ? value
            .filter((id): id is string => typeof id === 'string')
            .slice(0, 128)
        : []
    )
  } catch {
    return new Set()
  }
}

export interface LogColumnMasking {
  maskedColumns: ReadonlySet<string>
  onMaskChange: (columnId: string, masked: boolean) => void
  resetMasks: () => void
}

/** Store only column identifiers. Replacing the cell renderer also removes its
 * original tooltip, copy control, links, and inspect dialogs from the DOM. */
export function useLogColumnMasking<TData>(
  columns: ColumnDef<TData>[],
  storageKey: string
): LogColumnMasking & { columns: ColumnDef<TData>[] } {
  const initialMasks = useMemo(
    () => readMaskedColumns(storageKey),
    [storageKey]
  )
  const [state, setState] = useState(() => ({
    storageKey,
    masks: initialMasks,
  }))
  const masks = state.storageKey === storageKey ? state.masks : initialMasks
  const columnIds = useMemo(
    () => new Set(columns.map(getColumnId).filter((id): id is string => !!id)),
    [columns]
  )
  const maskedColumns = useMemo(
    () => new Set([...masks].filter((id) => columnIds.has(id))),
    [masks, columnIds]
  )
  const updateMasks = useCallback(
    (nextMasks: Set<string>) => {
      setState({ storageKey, masks: nextMasks })
      try {
        localStorage.setItem(storageKey, JSON.stringify([...nextMasks]))
      } catch {
        // The current view still works when storage is unavailable.
      }
    },
    [storageKey]
  )
  const onMaskChange = useCallback(
    (columnId: string, masked: boolean) => {
      if (!columnIds.has(columnId)) return
      const nextMasks = new Set(maskedColumns)
      if (masked) nextMasks.add(columnId)
      else nextMasks.delete(columnId)
      updateMasks(nextMasks)
    },
    [columnIds, maskedColumns, updateMasks]
  )
  const resetMasks = useCallback(() => updateMasks(new Set()), [updateMasks])
  const maskedDefinitions = useMemo(
    () =>
      columns.map((column) =>
        maskedColumns.has(getColumnId(column) ?? '')
          ? { ...column, cell: MaskedLogContent }
          : column
      ),
    [columns, maskedColumns]
  )
  return { columns: maskedDefinitions, maskedColumns, onMaskChange, resetMasks }
}
