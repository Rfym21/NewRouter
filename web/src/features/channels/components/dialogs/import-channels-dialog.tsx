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
import { KeyRound, Loader2, Upload } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'

import { createChannel } from '../../api'
import { CHANNEL_TYPES } from '../../constants'
import {
  buildImportChannelRequest,
  channelsQueryKeys,
  parseChannelImportFile,
} from '../../lib'
import type { ChannelExportItem } from '../../types'

type ImportChannelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

function getChannelTypeLabel(type: number): string {
  return CHANNEL_TYPES[type as keyof typeof CHANNEL_TYPES] ?? String(type)
}

function countModels(models: string): number {
  if (!models) return 0
  return models
    .split(',')
    .map((m) => m.trim())
    .filter(Boolean).length
}

export function ImportChannelsDialog({
  open,
  onOpenChange,
}: ImportChannelsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [fileName, setFileName] = useState('')
  const [items, setItems] = useState<ChannelExportItem[]>([])
  const [isImporting, setIsImporting] = useState(false)
  const [progress, setProgress] = useState(0)

  const missingKeyCount = useMemo(
    () => items.filter((item) => !item.key || item.key.trim() === '').length,
    [items]
  )

  const resetState = () => {
    setFileName('')
    setItems([])
    setIsImporting(false)
    setProgress(0)
    if (fileInputRef.current) {
      fileInputRef.current.value = ''
    }
  }

  const handleClose = (next: boolean) => {
    if (isImporting) return
    if (!next) resetState()
    onOpenChange(next)
  }

  const handleFileChange = async (
    event: React.ChangeEvent<HTMLInputElement>
  ) => {
    const file = event.target.files?.[0]
    if (!file) return

    try {
      const text = await file.text()
      const parsed = parseChannelImportFile(text)
      setItems(parsed)
      setFileName(file.name)
    } catch (error) {
      const reason = error instanceof Error ? error.message : 'invalid-schema'
      toast.error(
        reason === 'invalid-json'
          ? t('The selected file is not valid JSON')
          : t('The file format is not a valid channel export')
      )
      setItems([])
      setFileName('')
    }
  }

  const handleImport = async () => {
    if (items.length === 0) return

    setIsImporting(true)
    setProgress(0)

    let success = 0
    const failures: string[] = []

    for (let i = 0; i < items.length; i += 1) {
      const item = items[i]
      try {
        const res = await createChannel(buildImportChannelRequest(item))
        if (res.success) {
          success += 1
        } else {
          failures.push(item.name)
        }
      } catch {
        failures.push(item.name)
      }
      setProgress(i + 1)
    }

    await queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all })
    setIsImporting(false)

    if (failures.length === 0) {
      toast.success(t('Imported {{count}} channel(s)', { count: success }))
      handleClose(false)
    } else {
      toast.warning(
        t('Imported {{success}} channel(s), {{failed}} failed', {
          success,
          failed: failures.length,
        })
      )
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleClose}
      title={t('Import Channels')}
      description={t(
        'Load channels from a JSON file exported by New Router. Each entry is created as a new channel.'
      )}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            variant='outline'
            onClick={() => handleClose(false)}
            disabled={isImporting}
          >
            {t('Cancel')}
          </Button>
          <Button
            onClick={handleImport}
            disabled={items.length === 0 || isImporting}
          >
            {isImporting && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {isImporting
              ? t('Importing {{current}}/{{total}}', {
                  current: progress,
                  total: items.length,
                })
              : t('Import')}
          </Button>
        </>
      }
    >
      <div className='space-y-4 py-2'>
        <input
          ref={fileInputRef}
          type='file'
          accept='application/json,.json'
          className='hidden'
          onChange={handleFileChange}
        />

        <Button
          variant='outline'
          className='w-full'
          onClick={() => fileInputRef.current?.click()}
          disabled={isImporting}
        >
          <Upload className='mr-2 h-4 w-4' />
          {fileName || t('Select JSON file')}
        </Button>

        {items.length > 0 && (
          <div className='space-y-2'>
            <div className='text-muted-foreground flex items-center justify-between text-sm'>
              <span>
                {t('{{count}} channel(s) to import', { count: items.length })}
              </span>
              {missingKeyCount > 0 && (
                <span className='text-amber-600 dark:text-amber-500'>
                  {t('{{count}} without key', { count: missingKeyCount })}
                </span>
              )}
            </div>

            <div className='max-h-72 space-y-1 overflow-y-auto rounded-md border p-1'>
              {items.map((item) => (
                <div
                  key={`${item.name}::${item.type}::${item.key?.slice(0, 8) ?? ''}::${item.models ?? ''}`}
                  className='flex items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm'
                >
                  <div className='flex min-w-0 items-center gap-2'>
                    <span className='truncate font-medium'>{item.name}</span>
                    <Badge variant='secondary' className='shrink-0'>
                      {getChannelTypeLabel(item.type)}
                    </Badge>
                  </div>
                  <div className='text-muted-foreground flex shrink-0 items-center gap-2 text-xs'>
                    <span>
                      {t('{{count}} model(s)', {
                        count: countModels(item.models),
                      })}
                    </span>
                    {item.key && item.key.trim() !== '' && (
                      <KeyRound className='h-3.5 w-3.5 text-green-600 dark:text-green-500' />
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </Dialog>
  )
}
