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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

import type { LogColumnMasking } from '../lib/column-masking'

export function LogColumnOptions<TData>({
  table,
  masking,
}: {
  table: Table<TData>
  masking: LogColumnMasking
}) {
  const { t } = useTranslation()
  const columns = table
    .getAllLeafColumns()
    .filter((column) => !['select', 'actions'].includes(column.id))
  const removableColumns = columns.filter((column) => column.getCanHide())
  const getLabel = (column: (typeof columns)[number]) =>
    typeof column.columnDef.header === 'string'
      ? column.columnDef.header
      : (column.columnDef.meta?.label ?? column.id)

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        render={
          <Button
            variant='outline'
            className='shrink-0'
            aria-label={t('Columns')}
          />
        }
      >
        {t('Columns')}
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end' className='w-56'>
        <DropdownMenuGroup>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              {t('Toggle columns')}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className='w-56'>
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t('Toggle columns')}</DropdownMenuLabel>
                {removableColumns.map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={column.getIsVisible()}
                    onCheckedChange={(visible) =>
                      column.toggleVisibility(visible)
                    }
                    closeOnClick={false}
                  >
                    {getLabel(column)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              {t('Hide column contents')}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className='w-56'>
              <DropdownMenuGroup>
                <DropdownMenuLabel>
                  {t('Hide column contents')}
                </DropdownMenuLabel>
                {columns.map((column) => (
                  <DropdownMenuCheckboxItem
                    key={column.id}
                    checked={masking.maskedColumns.has(column.id)}
                    onCheckedChange={(masked) =>
                      masking.onMaskChange(column.id, masked)
                    }
                    closeOnClick={false}
                  >
                    {getLabel(column)}
                  </DropdownMenuCheckboxItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem
            disabled={masking.maskedColumns.size === 0}
            onClick={masking.resetMasks}
          >
            {t('Show all column contents')}
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
