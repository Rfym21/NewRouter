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
import { useQuery, useQueryClient, useMutation } from '@tanstack/react-query'
import { Loader2, Trash2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import {
  addRatioSchedule,
  deleteRatioSchedule,
  getRatioSchedules,
  type RatioSchedule,
  type RatioScheduleMode,
} from './ratio-schedule-api'

const ratioSchedulesQueryKey = ['ratio-schedules'] as const

/** Format a 0..1439 minute-of-day as a zero-padded "HH:MM" string. */
function minuteToHHMM(minute: number): string {
  const safe = ((Math.floor(minute) % 1440) + 1440) % 1440
  const hh = Math.floor(safe / 60)
  const mm = safe % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(hh)}:${pad(mm)}`
}

/** Convert a "HH:MM" time to a 0..1439 minute-of-day, or null when malformed. */
function hhmmToMinute(hhmm: string): number | null {
  const match = /^(\d{1,2}):(\d{2})$/.exec(hhmm.trim())
  if (!match) return null
  const hh = Number.parseInt(match[1], 10)
  const mm = Number.parseInt(match[2], 10)
  if (hh > 23 || mm > 59) return null
  return hh * 60 + mm
}

export function RatioScheduleManager({ groupNames }: { groupNames: string[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: ratioSchedulesQueryKey,
    queryFn: async () => requireServerSuccess(await getRatioSchedules()),
  })
  const schedules = data?.data ?? []

  const [group, setGroup] = useState('')
  const [startTime, setStartTime] = useState('00:00')
  const [endTime, setEndTime] = useState('06:00')
  const [mode, setMode] = useState<RatioScheduleMode>('override')
  const [ratio, setRatio] = useState('0.8')
  const [stack, setStack] = useState(false)
  const [enabled, setEnabled] = useState(true)

  const addMutation = useMutation({
    mutationFn: async (payload: Parameters<typeof addRatioSchedule>[0]) =>
      requireServerSuccess(await addRatioSchedule(payload)),
    onSuccess: (res) => {
      if (!res.success) {
        handleServerError(res, t('Failed to add schedule'))
        return
      }
      toast.success(t('Schedule added'))
      queryClient.invalidateQueries({ queryKey: ratioSchedulesQueryKey })
    },
    onError: (error: Error) => handleServerError(error),
  })

  const deleteMutation = useMutation({
    mutationFn: async (id: number) =>
      requireServerSuccess(await deleteRatioSchedule(id)),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ratioSchedulesQueryKey })
    },
    onError: (error: Error) => handleServerError(error),
  })

  const handleAdd = useCallback(() => {
    if (!group.trim()) {
      toast.info(t('Please select a group'))
      return
    }
    const startMinute = hhmmToMinute(startTime)
    const endMinute = hhmmToMinute(endTime)
    if (startMinute === null || endMinute === null) {
      toast.info(t('Please pick a time'))
      return
    }
    addMutation.mutate({
      group_name: group.trim(),
      start_minute: startMinute,
      end_minute: endMinute,
      mode,
      target_ratio: Number(ratio) || 0,
      // override 时叠加语义无意义,后端会忽略,前端发 false 保持契约干净。
      stack_with_user_ratio: mode === 'multiply' ? stack : false,
      enabled,
    })
  }, [group, startTime, endTime, mode, ratio, stack, enabled, addMutation, t])

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Scheduled ratio changes')}</CardTitle>
        <CardDescription>
          {t(
            'Apply a group ratio during a daily time window. Windows spanning midnight are supported. Runs on the master node.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className='space-y-4'>
        <div className='flex flex-wrap items-end gap-2'>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Group name')}</Label>
            <GroupNameField
              value={group}
              groupNames={groupNames}
              placeholder={t('Group name')}
              onChange={setGroup}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Start time')}</Label>
            <Input
              type='time'
              className='w-32'
              value={startTime}
              onChange={(e) => setStartTime(e.target.value)}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('End time')}</Label>
            <Input
              type='time'
              className='w-32'
              value={endTime}
              onChange={(e) => setEndTime(e.target.value)}
            />
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Mode')}</Label>
            <Select
              value={mode}
              onValueChange={(v) => {
                if (v === 'override' || v === 'multiply') setMode(v)
              }}
            >
              <SelectTrigger className='w-28'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value='override'>{t('Fixed')}</SelectItem>
                  <SelectItem value='multiply'>{t('Discount')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1'>
            <Label className='text-xs'>
              {mode === 'multiply' ? t('Discount factor') : t('Target ratio')}
            </Label>
            <Input
              type='number'
              min={0}
              step={0.05}
              className='w-24'
              value={ratio}
              onChange={(e) => setRatio(e.target.value)}
            />
          </div>
          {mode === 'multiply' && (
            <div className='space-y-1'>
              <Label className='text-xs'>{t('Stack with user ratio')}</Label>
              <div className='flex h-9 items-center'>
                <Switch checked={stack} onCheckedChange={setStack} />
              </div>
            </div>
          )}
          <div className='space-y-1'>
            <Label className='text-xs'>{t('Enabled')}</Label>
            <div className='flex h-9 items-center'>
              <Switch checked={enabled} onCheckedChange={setEnabled} />
            </div>
          </div>
          <Button
            size='sm'
            onClick={handleAdd}
            disabled={addMutation.isPending}
          >
            {addMutation.isPending && (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            )}
            {t('Add schedule')}
          </Button>
        </div>

        {isLoading && (
          <p className='text-muted-foreground text-sm'>{t('Loading...')}</p>
        )}
        {!isLoading && schedules.length === 0 && (
          <p className='text-muted-foreground text-sm'>
            {t('No scheduled changes.')}
          </p>
        )}
        {!isLoading && schedules.length > 0 && (
          <div className='space-y-1 rounded-md border p-1'>
            {schedules.map((schedule: RatioSchedule) => (
              <div
                key={schedule.id}
                className='flex items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-sm'
              >
                <div className='flex min-w-0 flex-wrap items-center gap-2'>
                  <span className='font-medium'>{schedule.group_name}</span>
                  <span className='text-muted-foreground'>
                    {minuteToHHMM(schedule.start_minute)}–
                    {minuteToHHMM(schedule.end_minute)}
                  </span>
                  <span className='text-muted-foreground'>
                    {schedule.mode === 'multiply'
                      ? `× ${schedule.target_ratio}`
                      : `→ ${schedule.target_ratio}`}
                  </span>
                  {schedule.mode === 'multiply' &&
                    schedule.stack_with_user_ratio && (
                      <span className='text-primary text-xs'>
                        {t('Stack with user ratio')}
                      </span>
                    )}
                  <StatusBadge
                    variant={schedule.enabled ? 'info' : 'neutral'}
                    copyable={false}
                  >
                    {schedule.enabled ? t('Enabled') : t('Disabled')}
                  </StatusBadge>
                </div>
                <div className='text-muted-foreground flex shrink-0 items-center gap-2 text-xs'>
                  <Button
                    variant='ghost'
                    size='sm'
                    onClick={() => deleteMutation.mutate(schedule.id)}
                    aria-label={t('Delete')}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function GroupNameField({
  value,
  groupNames,
  placeholder,
  onChange,
}: {
  value: string
  groupNames: string[]
  placeholder: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const options = useMemo(() => {
    if (value && !groupNames.includes(value)) return [value, ...groupNames]
    return groupNames
  }, [value, groupNames])

  if (options.length === 0) {
    return (
      <Input
        className='w-40'
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }

  return (
    <Select value={value || null} onValueChange={(v) => onChange(v ?? '')}>
      <SelectTrigger className='w-40'>
        <SelectValue placeholder={t('Select a group')} />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {options.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}
