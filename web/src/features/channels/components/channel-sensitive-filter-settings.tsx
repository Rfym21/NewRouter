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
import { useId, useMemo, type ReactElement, type ReactNode } from 'react'
import type { Control } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Textarea } from '@/components/ui/textarea'

import type { ChannelFormValues } from '../lib/channel-form'

type ChannelSensitiveFilterSettingsProps = {
  control: Control<ChannelFormValues>
  globalSensitiveCheckEnabled: boolean
  globalSensitiveWords: string[]
  globalSettingsStatus: 'ready' | 'loading' | 'error' | 'restricted'
  sensitiveCheckMode: ChannelFormValues['sensitive_check_mode']
  sensitiveMode: ChannelFormValues['sensitive_mode']
  sensitiveAddedWords: string
  sensitiveRemovedWords: string
  sensitiveOverrideWords: string
}

function normalizeSensitiveWords(words: string[]): string[] {
  return [
    ...new Set(words.map((word) => word.trim().toLowerCase()).filter(Boolean)),
  ].sort()
}

function parseSensitiveWordText(value: string): string[] {
  return normalizeSensitiveWords(value.split('\n'))
}

/**
 * ChannelSensitiveFilterSettings renders the channel override controls and
 * derives a preview from the global list without duplicating it in form state.
 *
 * @param props - Channel form control, current values, and global settings.
 * @returns The channel-level sensitive-word settings fields.
 */
export function ChannelSensitiveFilterSettings(
  props: ChannelSensitiveFilterSettingsProps
): ReactElement {
  const { t } = useTranslation()
  const id = useId()

  const preview = useMemo(() => {
    if (props.globalSettingsStatus !== 'ready') {
      return {
        checkDisabled: false,
        effectiveWords: [],
        sourceLabel: '',
      }
    }

    const globalWords = normalizeSensitiveWords(props.globalSensitiveWords)
    let effectiveWords = globalWords
    let sourceLabel = t('Inherited global list')

    if (props.sensitiveMode === 'modify') {
      const words = new Set(globalWords)
      for (const word of parseSensitiveWordText(props.sensitiveRemovedWords)) {
        words.delete(word)
      }
      for (const word of parseSensitiveWordText(props.sensitiveAddedWords)) {
        words.add(word)
      }
      effectiveWords = [...words].sort()
      sourceLabel = t('Global list with channel changes')
    } else if (props.sensitiveMode === 'override') {
      effectiveWords = parseSensitiveWordText(props.sensitiveOverrideWords)
      sourceLabel = t('Channel override list')
    }

    const checkDisabled =
      props.sensitiveCheckMode === 'disabled' ||
      (props.sensitiveCheckMode === 'inherit' &&
        !props.globalSensitiveCheckEnabled)

    return {
      checkDisabled,
      effectiveWords: checkDisabled ? [] : effectiveWords,
      sourceLabel,
    }
  }, [
    props.globalSensitiveCheckEnabled,
    props.globalSensitiveWords,
    props.globalSettingsStatus,
    props.sensitiveAddedWords,
    props.sensitiveCheckMode,
    props.sensitiveMode,
    props.sensitiveOverrideWords,
    props.sensitiveRemovedWords,
    t,
  ])

  let previewTitle: ReactNode = t('Effective sensitive word preview')
  let previewContent: ReactNode
  if (props.globalSettingsStatus === 'restricted') {
    previewContent = t(
      'Global sensitive-word settings are only visible to Root. Effective preview is unavailable.'
    )
  } else if (props.globalSettingsStatus === 'loading') {
    previewContent = t('Loading global sensitive-word settings...')
  } else if (props.globalSettingsStatus === 'error') {
    previewContent = t(
      'Unable to load global sensitive-word settings. Effective preview is unavailable.'
    )
  } else if (preview.checkDisabled) {
    previewContent = t('Sensitive word checking is disabled for this channel.')
  } else if (preview.effectiveWords.length === 0) {
    previewContent = t('No sensitive words are configured.')
  } else {
    previewContent = (
      <span className='block font-mono text-xs break-words'>
        {preview.effectiveWords.join(', ')}
      </span>
    )
  }

  if (props.globalSettingsStatus === 'ready') {
    previewTitle = (
      <>
        {t('Effective sensitive words ({{count}})', {
          count: preview.effectiveWords.length,
        })}
        <span className='text-muted-foreground ml-1 font-normal'>
          · {preview.sourceLabel}
        </span>
      </>
    )
  }

  return (
    <div className='space-y-5'>
      <FormField
        control={props.control}
        name='sensitive_check_mode'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <FormLabel>{t('Sensitive word check for this channel')}</FormLabel>
            <FormDescription>
              {t(
                'Follow the global prompt filter or override sensitive-word checking for this channel.'
              )}
            </FormDescription>
            <FormControl>
              <RadioGroup
                value={field.value}
                onValueChange={field.onChange}
                aria-label={t('Sensitive word check for this channel')}
                className='grid gap-3 lg:grid-cols-3'
              >
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='inherit' id={`${id}-check-inherit`} />
                  <Label
                    htmlFor={`${id}-check-inherit`}
                    className='cursor-pointer font-normal'
                  >
                    {t('Follow global setting')}
                  </Label>
                </div>
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='enabled' id={`${id}-check-enabled`} />
                  <Label
                    htmlFor={`${id}-check-enabled`}
                    className='cursor-pointer font-normal'
                  >
                    {t('Force enable')}
                  </Label>
                </div>
                <div className='flex items-center gap-2'>
                  <RadioGroupItem
                    value='disabled'
                    id={`${id}-check-disabled`}
                  />
                  <Label
                    htmlFor={`${id}-check-disabled`}
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

      <FormField
        control={props.control}
        name='sensitive_mode'
        render={({ field }) => (
          <FormItem className='space-y-3'>
            <FormLabel>{t('Sensitive word list mode')}</FormLabel>
            <FormDescription>
              {t(
                'Inherit the global list, adjust it for this channel, or replace it completely.'
              )}
            </FormDescription>
            <FormControl>
              <RadioGroup
                value={field.value}
                onValueChange={field.onChange}
                aria-label={t('Sensitive word list mode')}
                className='grid gap-3 lg:grid-cols-3'
              >
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='inherit' id={`${id}-list-inherit`} />
                  <Label
                    htmlFor={`${id}-list-inherit`}
                    className='cursor-pointer font-normal'
                  >
                    {t('Inherit global word list')}
                  </Label>
                </div>
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='modify' id={`${id}-list-modify`} />
                  <Label
                    htmlFor={`${id}-list-modify`}
                    className='cursor-pointer font-normal'
                  >
                    {t('Modify global word list')}
                  </Label>
                </div>
                <div className='flex items-center gap-2'>
                  <RadioGroupItem value='override' id={`${id}-list-override`} />
                  <Label
                    htmlFor={`${id}-list-override`}
                    className='cursor-pointer font-normal'
                  >
                    {t('Override global word list')}
                  </Label>
                </div>
              </RadioGroup>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      {props.sensitiveMode === 'modify' && (
        <div className='grid gap-4 lg:grid-cols-2'>
          <FormField
            control={props.control}
            name='sensitive_added_words'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Additional sensitive words')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={5}
                    placeholder={t('Enter one keyword per line')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'These words are added to the global list for this channel.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={props.control}
            name='sensitive_removed_words'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Excluded sensitive words')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={5}
                    placeholder={t('Enter one keyword per line')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'These words are removed from the global list for this channel, ignoring case.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      )}

      {props.sensitiveMode === 'override' && (
        <FormField
          control={props.control}
          name='sensitive_override_words'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Channel override sensitive words')}</FormLabel>
              <FormControl>
                <Textarea
                  rows={6}
                  placeholder={t('Enter one keyword per line')}
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'The global list is ignored. Leave this empty to use no sensitive words for this channel.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      <Alert role='status'>
        <AlertTitle>{previewTitle}</AlertTitle>
        <AlertDescription>{previewContent}</AlertDescription>
      </Alert>
    </div>
  )
}
