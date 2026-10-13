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
import { Code2, Palette } from 'lucide-react'
import { useState } from 'react'
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'

import { UserRateLimitVisualEditor } from './user-rate-limit-visual-editor'

type UserRateLimitFieldValues = {
  ModelRequestRateLimitUser?: string
}

/**
 * Renders visual and JSON modes for the user-specific rate-limit option.
 * @returns The user-specific rate-limit form field.
 */
export function UserRateLimitField() {
  const { t } = useTranslation()
  const form = useFormContext<UserRateLimitFieldValues>()
  const [useVisualEditor, setUseVisualEditor] = useState(true)

  return (
    <FormField
      control={form.control}
      name='ModelRequestRateLimitUser'
      render={({ field }) => (
        <FormItem>
          <div className='flex items-center justify-between'>
            <FormLabel>{t('User-specific rate limits')}</FormLabel>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setUseVisualEditor((current) => !current)}
            >
              {useVisualEditor ? (
                <>
                  <Code2 aria-hidden='true' className='mr-2 h-4 w-4' />
                  {t('JSON Mode')}
                </>
              ) : (
                <>
                  <Palette aria-hidden='true' className='mr-2 h-4 w-4' />
                  {t('Visual Mode')}
                </>
              )}
            </Button>
          </div>
          <FormControl>
            {useVisualEditor ? (
              <UserRateLimitVisualEditor
                value={field.value ?? ''}
                onChange={field.onChange}
                aria-label={t('User-specific rate limits')}
              />
            ) : (
              <Textarea
                rows={10}
                placeholder={`{\n  "123": {\n    "*": [100, 80],\n    "vip": [300, 200]\n  }\n}`}
                className='font-mono text-sm'
                {...field}
              />
            )}
          </FormControl>
          <FormDescription>
            <span className='block'>
              {t("Use * for this user's default rule.")}
            </span>
            <span className='block'>
              {t(
                'Priority: user + group > user default > group > global default'
              )}
            </span>
            {!useVisualEditor ? (
              <span className='mt-1 block font-mono text-xs'>
                {`{"userId": {"*": [maxRequests, maxSuccess], "groupName": [maxRequests, maxSuccess]}}`}
              </span>
            ) : null}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
