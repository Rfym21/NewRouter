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
import { Add01Icon, Delete02Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyContent,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import {
  formatFieldPassThroughRules,
  parseFieldPassThroughRulesText,
} from '../lib/field-passthrough'
import type { FieldPassThroughRule } from '../types'

type FieldPassthroughEditorProps = {
  label: string
  description: string
  fieldPlaceholder: string
  rules: FieldPassThroughRule[]
  text: string
  onRulesChange: (rules: FieldPassThroughRule[]) => void
  onTextChange: (text: string) => void
  examples?: FieldPassThroughRule[]
  disabled?: boolean
  invalid?: boolean
}

/**
 * Renders visual and JSON editors for channel field passthrough rules.
 *
 * @param props - Controlled rule and JSON editor values.
 * @returns The field passthrough rule editor.
 */
export function FieldPassthroughEditor(props: FieldPassthroughEditorProps) {
  const { t } = useTranslation()
  const editorId = useId()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const ruleKeysRef = useRef(new WeakMap<FieldPassThroughRule, string>())
  const nextRuleKeyRef = useRef(0)

  function getRuleKey(rule: FieldPassThroughRule): string {
    const existingKey = ruleKeysRef.current.get(rule)
    if (existingKey) return existingKey

    nextRuleKeyRef.current += 1
    const key = `${editorId}-rule-${nextRuleKeyRef.current}`
    ruleKeysRef.current.set(rule, key)
    return key
  }

  function syncRules(rules: FieldPassThroughRule[]): void {
    props.onRulesChange(rules)
    props.onTextChange(formatFieldPassThroughRules(rules))
  }

  function handleModeChange(nextMode: string): void {
    if (nextMode === 'visual' || nextMode === 'json') {
      setMode(nextMode)
    }
  }

  function handleJsonChange(value: string): void {
    props.onTextChange(value)
    const parsed = parseFieldPassThroughRulesText(value)
    if (parsed !== null) {
      props.onRulesChange(parsed)
    }
  }

  function handleRuleChange(
    index: number,
    patch: Partial<FieldPassThroughRule>
  ): void {
    syncRules(
      props.rules.map((rule, ruleIndex) => {
        if (ruleIndex !== index) return rule

        const nextRule = { ...rule, ...patch }
        ruleKeysRef.current.set(nextRule, getRuleKey(rule))
        return nextRule
      })
    )
  }

  function handleAddRule(): void {
    syncRules([...props.rules, { field: '', enabled: true, comment: '' }])
  }

  function handleRemoveRule(index: number): void {
    syncRules(props.rules.filter((_, ruleIndex) => ruleIndex !== index))
  }

  function handleFillExamples(): void {
    syncRules((props.examples || []).map((rule) => ({ ...rule })))
  }

  return (
    <section aria-labelledby={`${editorId}-title`} className='space-y-3'>
      <div className='space-y-1'>
        <h4 id={`${editorId}-title`} className='text-sm font-medium'>
          {props.label}
        </h4>
        <p className='text-muted-foreground text-sm'>{props.description}</p>
      </div>

      <Tabs value={mode} onValueChange={handleModeChange}>
        <TabsList variant='line'>
          <TabsTrigger value='visual'>{t('Visual')}</TabsTrigger>
          <TabsTrigger value='json'>{t('JSON')}</TabsTrigger>
        </TabsList>

        <TabsContent value='visual' className='space-y-3 pt-3'>
          {props.rules.length === 0 ? (
            <Empty className='min-h-40 border'>
              <EmptyHeader>
                <EmptyMedia variant='icon'>
                  <HugeiconsIcon icon={Add01Icon} aria-hidden='true' />
                </EmptyMedia>
                <EmptyTitle>{t('No rules yet')}</EmptyTitle>
              </EmptyHeader>
              <EmptyContent className='flex-row justify-center'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={handleAddRule}
                  disabled={props.disabled}
                >
                  <HugeiconsIcon
                    icon={Add01Icon}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  {t('Add')}
                </Button>
                {Boolean(props.examples?.length) && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={handleFillExamples}
                    disabled={props.disabled}
                  >
                    {t('Fill Template')}
                  </Button>
                )}
              </EmptyContent>
            </Empty>
          ) : (
            <>
              <FieldGroup className='gap-3'>
                {props.rules.map((rule, index) => {
                  const fieldId = `${editorId}-field-${index}`
                  const commentId = `${editorId}-comment-${index}`
                  const enabledId = `${editorId}-enabled-${index}`

                  return (
                    <div
                      key={getRuleKey(rule)}
                      className='bg-muted/30 space-y-3 rounded-lg border p-3'
                    >
                      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto_auto] sm:items-center'>
                        <Field>
                          <FieldLabel htmlFor={fieldId} className='sr-only'>
                            {t('Field Path')}
                          </FieldLabel>
                          <Input
                            id={fieldId}
                            value={rule.field}
                            placeholder={props.fieldPlaceholder}
                            onChange={(event) =>
                              handleRuleChange(index, {
                                field: event.target.value,
                              })
                            }
                            disabled={props.disabled}
                          />
                        </Field>
                        <Field orientation='horizontal' className='w-auto'>
                          <Switch
                            id={enabledId}
                            checked={rule.enabled}
                            onCheckedChange={(enabled) =>
                              handleRuleChange(index, { enabled })
                            }
                            disabled={props.disabled}
                          />
                          <Label
                            htmlFor={enabledId}
                            className='cursor-pointer font-normal'
                          >
                            {rule.enabled ? t('Enabled') : t('Disabled')}
                          </Label>
                        </Field>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          onClick={() => handleRemoveRule(index)}
                          disabled={props.disabled}
                          aria-label={t('Delete')}
                        >
                          <HugeiconsIcon
                            icon={Delete02Icon}
                            aria-hidden='true'
                          />
                        </Button>
                      </div>
                      <Field>
                        <FieldLabel htmlFor={commentId} className='sr-only'>
                          {t('Remark')}
                        </FieldLabel>
                        <Input
                          id={commentId}
                          value={rule.comment || ''}
                          placeholder={t('Remark')}
                          onChange={(event) =>
                            handleRuleChange(index, {
                              comment: event.target.value,
                            })
                          }
                          disabled={props.disabled}
                        />
                      </Field>
                    </div>
                  )
                })}
              </FieldGroup>
              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  onClick={handleAddRule}
                  disabled={props.disabled}
                >
                  <HugeiconsIcon
                    icon={Add01Icon}
                    data-icon='inline-start'
                    aria-hidden='true'
                  />
                  {t('Add')}
                </Button>
                {Boolean(props.examples?.length) && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    onClick={handleFillExamples}
                    disabled={props.disabled}
                  >
                    {t('Fill Template')}
                  </Button>
                )}
              </div>
            </>
          )}
        </TabsContent>

        <TabsContent value='json' className='space-y-2 pt-3'>
          <Textarea
            value={props.text}
            onChange={(event) => handleJsonChange(event.target.value)}
            placeholder='[{"field":"service_tier","enabled":true,"comment":"..."}]'
            disabled={props.disabled}
            rows={8}
            className='font-mono text-sm'
            aria-labelledby={`${editorId}-title`}
            aria-invalid={props.invalid}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'JSON array format: [{"field":"...","enabled":true,"comment":"..."}]'
            )}
          </p>
        </TabsContent>
      </Tabs>
    </section>
  )
}
