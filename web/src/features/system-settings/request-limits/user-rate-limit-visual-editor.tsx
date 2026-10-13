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
import { AlertCircle, Plus, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table/static/static-data-table'
import { StaticRowActions } from '@/components/data-table/static/static-row-actions'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

import {
  parseUserRateLimitEntries,
  serializeUserRateLimitEntries,
  USER_DEFAULT_RATE_LIMIT_GROUP,
  type UserRateLimitEntry,
} from './user-rate-limit-config'
import { UserRateLimitDialog } from './user-rate-limit-dialog'

type UserRateLimitVisualEditorProps = {
  value: string
  onChange: (value: string) => void
  id?: string
  className?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
  'aria-label'?: string
  'data-form-root'?: string
  'data-slot'?: string
}

const EMPTY_USER_RATE_LIMIT_ENTRIES: UserRateLimitEntry[] = []

/**
 * Renders a searchable editor for user and user-group rate-limit overrides.
 * @param props Persisted JSON value and its change callback.
 * @returns The user rate-limit visual editor.
 */
export function UserRateLimitVisualEditor(
  props: UserRateLimitVisualEditorProps
) {
  const { t } = useTranslation()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editData, setEditData] = useState<UserRateLimitEntry | null>(null)

  const parsedEntries = useMemo(
    () => parseUserRateLimitEntries(props.value),
    [props.value]
  )
  const entries = parsedEntries ?? EMPTY_USER_RATE_LIMIT_ENTRIES
  const hasSearch = searchText.trim() !== ''
  const filteredEntries = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    if (!query) return entries
    return entries.filter(
      (entry) =>
        entry.userId.includes(query) ||
        entry.groupName.toLowerCase().includes(query)
    )
  }, [entries, searchText])

  const handleSave = (data: UserRateLimitEntry) => {
    const duplicate = entries.some(
      (entry) =>
        entry.userId === data.userId &&
        entry.groupName === data.groupName &&
        (editData === null ||
          entry.userId !== editData.userId ||
          entry.groupName !== editData.groupName)
    )
    if (duplicate) {
      toast.error(t('A rule for this user and group already exists.'))
      return false
    }

    let nextEntries: UserRateLimitEntry[]
    if (editData) {
      nextEntries = entries.map((entry) =>
        entry.userId === editData.userId &&
        entry.groupName === editData.groupName
          ? data
          : entry
      )
    } else {
      nextEntries = [...entries, data]
    }
    props.onChange(serializeUserRateLimitEntries(nextEntries))
    return true
  }

  const handleDelete = (entryToDelete: UserRateLimitEntry) => {
    const nextEntries = entries.filter(
      (entry) =>
        entry.userId !== entryToDelete.userId ||
        entry.groupName !== entryToDelete.groupName
    )
    props.onChange(serializeUserRateLimitEntries(nextEntries))
  }

  const handleEdit = (entry: UserRateLimitEntry) => {
    setEditData(entry)
    setDialogOpen(true)
  }

  const handleAdd = () => {
    setEditData(null)
    setDialogOpen(true)
  }

  let emptyContent = t(
    'No user-specific rate limits configured. Click "Add user rule" to get started.'
  )
  if (parsedEntries === null) {
    emptyContent = t('Correct the invalid JSON to edit user rules.')
  } else if (hasSearch) {
    emptyContent = t('No user rules match your search')
  }

  return (
    <div
      id={props.id}
      role='group'
      aria-describedby={props['aria-describedby']}
      aria-invalid={props['aria-invalid']}
      aria-label={props['aria-label']}
      data-form-root={props['data-form-root']}
      data-slot={props['data-slot']}
      className={cn('space-y-4', props.className)}
    >
      {parsedEntries === null ? (
        <Alert variant='destructive'>
          <AlertCircle aria-hidden='true' />
          <AlertTitle>{t('Invalid user rate-limit JSON')}</AlertTitle>
          <AlertDescription>
            {t('Switch to JSON mode and correct the configuration first.')}
          </AlertDescription>
        </Alert>
      ) : null}

      <div className='flex items-center gap-4'>
        <div className='relative flex-1'>
          <Search
            aria-hidden='true'
            className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4'
          />
          <Input
            placeholder={t('Search user IDs or group names...')}
            aria-label={t('Search user IDs or group names...')}
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
            className='pl-9'
          />
        </div>
        <Button
          type='button'
          onClick={handleAdd}
          disabled={parsedEntries === null}
        >
          <Plus aria-hidden='true' className='mr-2 h-4 w-4' />
          {t('Add user rule')}
        </Button>
      </div>

      <StaticDataTable
        data={filteredEntries}
        getRowKey={(entry) => `${entry.userId}:${entry.groupName}`}
        emptyContent={emptyContent}
        columns={[
          {
            id: 'user-id',
            header: t('User ID'),
            cell: (entry) => <Badge variant='outline'>{entry.userId}</Badge>,
          },
          {
            id: 'group',
            header: t('Group Name'),
            cell: (entry) => (
              <Badge
                variant={
                  entry.groupName === USER_DEFAULT_RATE_LIMIT_GROUP
                    ? 'secondary'
                    : 'outline'
                }
              >
                {entry.groupName === USER_DEFAULT_RATE_LIMIT_GROUP
                  ? t('User default')
                  : entry.groupName}
              </Badge>
            ),
          },
          {
            id: 'max-requests',
            header: t('Max Requests (incl. failures)'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (entry) => (
              <span className='font-mono'>
                {entry.maxRequests === 0
                  ? t('Unlimited')
                  : entry.maxRequests.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'max-success',
            header: t('Max Success'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (entry) => (
              <span className='font-mono'>
                {entry.maxSuccess.toLocaleString()}
              </span>
            ),
          },
          {
            id: 'actions',
            header: t('Actions'),
            className: 'text-right',
            cellClassName: 'text-right',
            cell: (entry) => (
              <StaticRowActions
                editLabel={t('Edit')}
                deleteLabel={t('Delete')}
                menuLabel={t('Open menu')}
                onEdit={() => handleEdit(entry)}
                onDelete={() => handleDelete(entry)}
              />
            ),
          },
        ]}
      />

      <UserRateLimitDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        editData={editData}
      />
    </div>
  )
}
