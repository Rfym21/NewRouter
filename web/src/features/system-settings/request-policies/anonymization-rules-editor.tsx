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
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import {
  getServerErrorMessage,
  isServerErrorCancelled,
} from '@/lib/server-error-message'

import {
  previewAnonymization,
  type AnonymizationPreview,
  type AnonymizationRule,
} from '../api'
import {
  entityLabels,
  parseRules,
  validateAnonymizationRules,
} from './anonymization-rules'

type Props = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
}
type PreviewResult = AnonymizationPreview & { rules: string; text: string }

export function AnonymizationRulesEditor({
  value,
  onChange,
  disabled = false,
}: Props) {
  const { t } = useTranslation()
  const id = useId()
  const rules = useMemo(() => parseRules(value), [value])
  const ruleSequence = useRef(0)
  const newRuleKey = () => `${id}-rule-${++ruleSequence.current}`
  const [ruleKeys, setRuleKeys] = useState<string[]>(
    () => rules?.map(() => newRuleKey()) ?? []
  )
  const currentKeys =
    rules?.map(
      (rule, index) => ruleKeys[index] ?? `${id}-loaded-${rule.name}`
    ) ?? []
  const [text, setText] = useState('')
  const [result, setResult] = useState<PreviewResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const request = useRef<{
    sequence: number
    controller: AbortController | null
  }>({ sequence: 0, controller: null })
  useEffect(
    () => () => {
      request.current.sequence++
      request.current.controller?.abort()
    },
    []
  )

  const cancelPreview = () => {
    request.current.sequence++
    request.current.controller?.abort()
    setPreviewing(false)
    setError(null)
  }
  const updateRules = (next: AnonymizationRule[], keys = currentKeys) => {
    cancelPreview()
    setRuleKeys(next.map((_, index) => keys[index] ?? newRuleKey()))
    onChange(JSON.stringify(next))
  }
  const updateRule = (index: number, update: Partial<AnonymizationRule>) => {
    if (rules) {
      updateRules(
        rules.map((rule, position) =>
          position === index ? { ...rule, ...update } : rule
        )
      )
    }
  }
  const moveRule = (index: number, direction: number) => {
    if (!rules) return
    const next = [...rules]
    const target = index + direction
    if (target < 0 || target >= next.length) return
    ;[next[index], next[target]] = [next[target], next[index]]
    const keys = [...currentKeys]
    ;[keys[index], keys[target]] = [keys[target], keys[index]]
    updateRules(next, keys)
  }

  const handlePreview = async () => {
    const validation = validateAnonymizationRules(value)
    if (validation || !rules) {
      setError(t(validation ?? 'Invalid anonymization rules'))
      return
    }
    if (!text.trim() || new TextEncoder().encode(text).length > 16 * 1024) {
      setError(t('Invalid anonymization preview input'))
      return
    }
    request.current.controller?.abort()
    const controller = new AbortController()
    const sequence = ++request.current.sequence
    request.current.controller = controller
    setPreviewing(true)
    setError(null)
    setResult(null)
    try {
      const preview = await previewAnonymization(
        { rules, text },
        controller.signal
      )
      if (sequence === request.current.sequence) {
        setResult({ ...preview, rules: value, text })
      }
    } catch (cause) {
      if (
        sequence === request.current.sequence &&
        !isServerErrorCancelled(cause)
      ) {
        setError(
          t(
            getServerErrorMessage(cause, t('Failed to preview anonymization')),
            { skipInterpolation: true }
          )
        )
      }
    } finally {
      if (sequence === request.current.sequence) setPreviewing(false)
    }
  }
  const preview =
    result?.rules === value && result.text === text ? result : null
  const translatedEntities = Object.entries(entityLabels).map(
    ([key, label]) => ({ value: key, label: t(label) })
  )

  return (
    <FieldSet>
      <FieldLegend>{t('Custom anonymization rules')}</FieldLegend>
      <FieldDescription>
        {t(
          'Rules run in order before built-in detection. Keywords match exactly; regular expressions use RE2. Channels use these rules whenever anonymization is enabled.'
        )}
      </FieldDescription>
      <FieldGroup>
        {!rules && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Invalid anonymization rules')}
            </AlertDescription>
          </Alert>
        )}
        {rules?.length === 0 && (
          <FieldDescription>
            {t('No custom anonymization rules')}
          </FieldDescription>
        )}
        {rules?.map((rule, index) => {
          const prefix = `${id}-${index}`
          return (
            <FieldSet
              key={currentKeys[index]}
              disabled={disabled}
              className='rounded-lg border p-4'
            >
              <FieldLegend variant='label'>
                {t('Anonymization rule {{index}}', { index: index + 1 })}
              </FieldLegend>
              <FieldGroup className='gap-4'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <Field orientation='horizontal' className='w-auto'>
                    <Switch
                      id={`${prefix}-enabled`}
                      checked={rule.enabled}
                      onCheckedChange={(checked) =>
                        updateRule(index, { enabled: checked })
                      }
                      disabled={disabled}
                    />
                    <FieldLabel htmlFor={`${prefix}-enabled`}>
                      {t('Enable this rule')}
                    </FieldLabel>
                  </Field>
                  <div className='flex flex-wrap gap-2'>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={disabled || index === 0}
                      onClick={() => moveRule(index, -1)}
                    >
                      {t('Move up')}
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={disabled || index === rules.length - 1}
                      onClick={() => moveRule(index, 1)}
                    >
                      {t('Move down')}
                    </Button>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      disabled={disabled}
                      onClick={() =>
                        updateRules(
                          rules.filter((_, position) => position !== index),
                          currentKeys.filter(
                            (_, position) => position !== index
                          )
                        )
                      }
                    >
                      {t('Delete')}
                    </Button>
                  </div>
                </div>
                <Field>
                  <FieldLabel htmlFor={`${prefix}-name`}>
                    {t('Rule name')}
                  </FieldLabel>
                  <Input
                    id={`${prefix}-name`}
                    value={rule.name}
                    maxLength={80}
                    disabled={disabled}
                    onChange={(event) =>
                      updateRule(index, { name: event.target.value })
                    }
                  />
                </Field>
                <Field>
                  <FieldLabel id={`${prefix}-mode`}>
                    {t('Match mode')}
                  </FieldLabel>
                  <RadioGroup
                    value={rule.mode}
                    onValueChange={(mode) =>
                      updateRule(index, {
                        mode: mode as 'literal' | 'regex',
                        capture_group:
                          mode === 'literal' ? 0 : rule.capture_group,
                      })
                    }
                    aria-labelledby={`${prefix}-mode`}
                    disabled={disabled}
                    className='flex flex-wrap gap-4'
                  >
                    <div className='flex items-center gap-2'>
                      <RadioGroupItem
                        id={`${prefix}-literal`}
                        value='literal'
                      />
                      <Label htmlFor={`${prefix}-literal`}>
                        {t('Keyword')}
                      </Label>
                    </div>
                    <div className='flex items-center gap-2'>
                      <RadioGroupItem id={`${prefix}-regex`} value='regex' />
                      <Label htmlFor={`${prefix}-regex`}>
                        {t('Regular expression')}
                      </Label>
                    </div>
                  </RadioGroup>
                </Field>
                <Field>
                  <FieldLabel htmlFor={`${prefix}-pattern`}>
                    {rule.mode === 'literal'
                      ? t('Keyword')
                      : t('Regular expression')}
                  </FieldLabel>
                  <Textarea
                    id={`${prefix}-pattern`}
                    rows={2}
                    value={rule.pattern}
                    maxLength={4096}
                    disabled={disabled}
                    onChange={(event) =>
                      updateRule(index, { pattern: event.target.value })
                    }
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor={`${prefix}-entity`}>
                    {t('Entity type')}
                  </FieldLabel>
                  <Select
                    items={translatedEntities}
                    value={rule.entity_type ?? 'CUSTOM'}
                    onValueChange={(entity) =>
                      entity && updateRule(index, { entity_type: entity })
                    }
                    disabled={disabled}
                  >
                    <SelectTrigger id={`${prefix}-entity`} className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      {translatedEntities.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                {rule.mode === 'regex' && (
                  <Field>
                    <FieldLabel htmlFor={`${prefix}-capture`}>
                      {t('Capture group')}
                    </FieldLabel>
                    <Input
                      id={`${prefix}-capture`}
                      type='number'
                      min={0}
                      step={1}
                      value={rule.capture_group ?? 0}
                      disabled={disabled}
                      onChange={(event) =>
                        updateRule(index, {
                          capture_group: Number(event.target.value) || 0,
                        })
                      }
                    />
                    <FieldDescription>
                      {t(
                        'Group 0 replaces the whole match; higher groups replace only that capture.'
                      )}
                    </FieldDescription>
                  </Field>
                )}
              </FieldGroup>
            </FieldSet>
          )
        })}
        <div className='flex flex-wrap gap-2'>
          <Button
            type='button'
            variant='outline'
            disabled={disabled || !rules || rules.length >= 128}
            onClick={() =>
              rules &&
              updateRules([
                ...rules,
                {
                  name: '',
                  mode: 'literal',
                  pattern: '',
                  enabled: true,
                  entity_type: 'CUSTOM',
                  capture_group: 0,
                },
              ])
            }
          >
            {t('Add anonymization rule')}
          </Button>
          <Button
            type='button'
            variant='outline'
            disabled={disabled || value === '[]'}
            onClick={() => updateRules([])}
          >
            {t('Clear rules')}
          </Button>
        </div>
        <FieldSet>
          <FieldLegend variant='label'>
            {t('Anonymization preview')}
          </FieldLegend>
          <FieldDescription>
            {t(
              'Preview uses the current rules and built-in detectors without saving them. Sample text is not saved.'
            )}
          </FieldDescription>
          <Field>
            <FieldLabel htmlFor={`${id}-sample`}>{t('Sample text')}</FieldLabel>
            <Textarea
              id={`${id}-sample`}
              value={text}
              rows={4}
              maxLength={16384}
              disabled={disabled}
              onChange={(event) => {
                cancelPreview()
                setText(event.target.value)
              }}
            />
          </Field>
          <Button
            type='button'
            variant='outline'
            disabled={disabled || previewing || !text.trim()}
            onClick={() => void handlePreview()}
          >
            {previewing ? t('Previewing...') : t('Preview anonymization')}
          </Button>
          {error && (
            <Alert variant='destructive' role='alert'>
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          {preview && (
            <FieldGroup>
              {preview.truncated && (
                <FieldDescription>
                  {t('Showing {{count}} of {{total}} matches', {
                    count: preview.matches.length,
                    total: preview.match_count ?? preview.matches.length,
                  })}
                </FieldDescription>
              )}
              <Field>
                <FieldLabel htmlFor={`${id}-result`}>
                  {t('Anonymized text')}
                </FieldLabel>
                <Textarea
                  id={`${id}-result`}
                  readOnly
                  rows={4}
                  value={preview.anonymized}
                />
              </Field>
              {preview.matches.length === 0 ? (
                <FieldDescription>
                  {t('No sensitive values matched')}
                </FieldDescription>
              ) : (
                <Table aria-label={t('Anonymization matches')}>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Matched value')}</TableHead>
                      <TableHead>{t('Entity type')}</TableHead>
                      <TableHead>{t('Matched rule')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {preview.matches.map((match) => (
                      <TableRow key={`${match.start}-${match.end}`}>
                        <TableCell className='max-w-sm break-all whitespace-pre-wrap'>
                          {match.text}
                        </TableCell>
                        <TableCell>
                          {t(
                            entityLabels[
                              match.entity_type as keyof typeof entityLabels
                            ] ?? match.entity_type
                          )}
                        </TableCell>
                        <TableCell>
                          {match.rule_name || t('Built-in or analyzer')}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </FieldGroup>
          )}
        </FieldSet>
      </FieldGroup>
    </FieldSet>
  )
}
