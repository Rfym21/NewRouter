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
import { Loader2, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

import { previewClientUserAgent, type ClientUserAgentPreview } from '../api'
import { newClientRuleKey, type ClientRuleDraft } from './client-rules'

export function ClientRulesEditor(props: {
  value: ClientRuleDraft[]
  onChange: (value: ClientRuleDraft[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [testUserAgent, setTestUserAgent] = useState('')
  const [testing, setTesting] = useState(false)
  const [preview, setPreview] = useState<ClientUserAgentPreview | null>(null)
  const [previewError, setPreviewError] = useState('')

  const update = (index: number, patch: Partial<ClientRuleDraft>) => {
    props.onChange(
      props.value.map((rule, current) =>
        current === index ? { ...rule, ...patch } : rule
      )
    )
  }

  const test = async () => {
    if (!testUserAgent.trim()) return
    setTesting(true)
    setPreviewError('')
    try {
      setPreview(await previewClientUserAgent(testUserAgent.trim()))
    } catch (error) {
      setPreview(null)
      setPreviewError(
        error instanceof Error ? error.message : t('Failed to test User-Agent')
      )
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='space-y-1'>
          <p className='text-sm font-medium'>{t('Custom client rules')}</p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Rules are checked before the built-in library. Separate several keywords with commas; matching is case-insensitive.'
            )}
          </p>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={props.disabled}
          onClick={() =>
            props.onChange([
              ...props.value,
              { key: newClientRuleKey(), name: '', keywords: '' },
            ])
          }
        >
          <Plus className='mr-1 size-3.5' />
          {t('Add rule')}
        </Button>
      </div>

      {props.value.length === 0 ? (
        <p className='text-muted-foreground border-border/60 rounded-lg border border-dashed p-3 text-xs'>
          {t(
            'No custom rules yet. The built-in client library still classifies requests.'
          )}
        </p>
      ) : (
        <div className='space-y-2'>
          {props.value.map((rule, index) => (
            <div
              key={rule.key}
              className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]'
            >
              <Input
                value={rule.name}
                maxLength={64}
                disabled={props.disabled}
                placeholder={t('Client name')}
                aria-label={t('Client name')}
                onChange={(event) =>
                  update(index, { name: event.target.value })
                }
              />
              <Input
                value={rule.keywords}
                disabled={props.disabled}
                placeholder={t('User-Agent keywords, comma separated')}
                aria-label={t('User-Agent keywords')}
                onChange={(event) =>
                  update(index, { keywords: event.target.value })
                }
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                disabled={props.disabled}
                aria-label={t('Remove rule')}
                onClick={() =>
                  props.onChange(
                    props.value.filter((_, current) => current !== index)
                  )
                }
              >
                <Trash2 className='size-4' />
              </Button>
            </div>
          ))}
        </div>
      )}

      <div className='border-border/60 space-y-2 border-t pt-3'>
        <p className='text-sm font-medium'>{t('Test a User-Agent')}</p>
        <div className='flex flex-col gap-2 sm:flex-row'>
          <Input
            value={testUserAgent}
            disabled={props.disabled || testing}
            placeholder={t('Paste a User-Agent to see the detected client')}
            aria-label={t('Test a User-Agent')}
            onChange={(event) => setTestUserAgent(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                void test()
              }
            }}
          />
          <Button
            type='button'
            variant='outline'
            disabled={props.disabled || testing || !testUserAgent.trim()}
            onClick={() => void test()}
          >
            {testing && <Loader2 className='mr-1 size-3.5 animate-spin' />}
            {t('Test')}
          </Button>
        </div>
        {preview && (
          <p className='text-muted-foreground text-xs'>
            {t('Detected client:')}{' '}
            <span className='text-foreground font-medium'>
              {preview.name || '—'}
            </span>
            {preview.custom && (
              <span className='ml-1'>({t('custom rule')})</span>
            )}
            {preview.category && (
              <span className='ml-1 font-mono'>{preview.category}</span>
            )}
          </p>
        )}
        {previewError && (
          <p className='text-destructive text-xs' role='alert'>
            {previewError}
          </p>
        )}
      </div>
    </div>
  )
}
