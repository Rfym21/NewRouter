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
import { useId, type ReactElement } from 'react'
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

import type { ChannelFormValues } from '../lib/channel-form'

type ChannelAnonymizationSettingsProps = {
  control: Control<ChannelFormValues>
}

/**
 * ChannelAnonymizationSettings renders the channel-level override for request /
 * response anonymization as a tri-state control (inherit / force enable / force
 * disable), mirroring the sensitive-word check override.
 *
 * @param props - Channel form control.
 * @returns The anonymization proxy override field.
 */
export function ChannelAnonymizationSettings(
  props: ChannelAnonymizationSettingsProps
): ReactElement {
  const { t } = useTranslation()
  const id = useId()

  return (
    <FormField
      control={props.control}
      name='anonymization_mode'
      render={({ field }) => (
        <FormItem className='space-y-3'>
          <FormLabel>{t('Anonymization proxy for this channel')}</FormLabel>
          <FormDescription>
            {t(
              'Follow global to inherit the global anonymization switch; force enable processes this channel even when the global switch is off; force disable keeps the original passthrough behavior for this channel.'
            )}
          </FormDescription>
          <FormControl>
            <RadioGroup
              value={field.value}
              onValueChange={field.onChange}
              aria-label={t('Anonymization proxy for this channel')}
              className='grid gap-3 lg:grid-cols-3'
            >
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='inherit' id={`${id}-anon-inherit`} />
                <Label
                  htmlFor={`${id}-anon-inherit`}
                  className='cursor-pointer font-normal'
                >
                  {t('Follow global setting')}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='enabled' id={`${id}-anon-enabled`} />
                <Label
                  htmlFor={`${id}-anon-enabled`}
                  className='cursor-pointer font-normal'
                >
                  {t('Force enable')}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='disabled' id={`${id}-anon-disabled`} />
                <Label
                  htmlFor={`${id}-anon-disabled`}
                  className='cursor-pointer font-normal'
                >
                  {t('Force disable')}
                </Label>
              </div>
            </RadioGroup>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
