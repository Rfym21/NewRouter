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
import { AlertTriangle, ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

type Rule = {
  _id: string
  userId: string
  visible: boolean
  targetGroup: string
  description: string
}

let _idCounter = 0
function uid() {
  return `usu_${++_idCounter}`
}

// Raw keys use +: (add), -: (remove), or no prefix (also add).
// The UI collapses this to visible/hidden and serializes visible rules
// with the +: prefix, which the backend treats identically to no prefix.
function parseRawKey(rawKey: string): { visible: boolean; groupName: string } {
  if (rawKey.startsWith('-:')) {
    return { visible: false, groupName: rawKey.slice(2) }
  }
  if (rawKey.startsWith('+:')) {
    return { visible: true, groupName: rawKey.slice(2) }
  }
  return { visible: true, groupName: rawKey }
}

function toRawKey(visible: boolean, groupName: string): string {
  return visible ? `+:${groupName}` : `-:${groupName}`
}

function safeParseJson(str: string): Record<string, Record<string, string>> {
  if (!str || !str.trim()) return {}
  try {
    return JSON.parse(str) as Record<string, Record<string, string>>
  } catch {
    return {}
  }
}

function flattenRules(nested: Record<string, Record<string, string>>): Rule[] {
  const rules: Rule[] = []
  for (const [userId, inner] of Object.entries(nested)) {
    if (typeof inner !== 'object' || inner === null) continue
    for (const [rawKey, desc] of Object.entries(inner)) {
      const { visible, groupName } = parseRawKey(rawKey)
      let description = ''
      if (!visible) {
        description = 'remove'
      } else if (typeof desc === 'string') {
        description = desc
      }
      rules.push({
        _id: uid(),
        userId,
        visible,
        targetGroup: groupName,
        description,
      })
    }
  }
  return rules
}

function serializeRules(rules: Rule[]): string {
  const result: Record<string, Record<string, string>> = {}
  for (const { userId, visible, targetGroup, description } of rules) {
    if (!userId || !targetGroup) continue
    if (!result[userId]) result[userId] = {}
    result[userId][toRawKey(visible, targetGroup)] = description
  }
  return Object.keys(result).length === 0
    ? '{}'
    : JSON.stringify(result, null, 2)
}

type GroupSelectProps = {
  options: string[]
  value: string
  placeholder: string
  onValueChange: (value: string) => void
  className?: string
}

function GroupSelect(props: GroupSelectProps) {
  const knownOptions = useMemo(() => {
    if (props.value && !props.options.includes(props.value)) {
      return [props.value, ...props.options]
    }
    return props.options
  }, [props.options, props.value])

  return (
    <Select
      value={props.value === '' ? null : props.value}
      onValueChange={(v) => {
        if (typeof v === 'string' && v !== '') props.onValueChange(v)
      }}
    >
      <SelectTrigger className={props.className}>
        <SelectValue placeholder={props.placeholder} />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {knownOptions.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

type UserSpecialUsableRulesEditorProps = {
  value: string
  groupOptions: string[]
  onChange: (value: string) => void
}

type UserSectionProps = {
  userId: string
  items: Rule[]
  groupOptions: string[]
  onUpdate: (id: string, field: keyof Rule, val: string | boolean) => void
  onRemove: (id: string) => void
  onAdd: (userId: string) => void
  onRemoveUser: (userId: string) => void
}

function UserSection(props: UserSectionProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className='rounded-lg border'>
        <div className='flex items-center justify-between p-3'>
          <div className='flex items-center gap-2'>
            <CollapsibleTrigger
              render={
                <Button variant='ghost' size='sm' className='h-6 w-6 p-0' />
              }
            >
              {open ? (
                <ChevronUp className='h-4 w-4' />
              ) : (
                <ChevronDown className='h-4 w-4' />
              )}
            </CollapsibleTrigger>
            <span className='font-semibold'>
              {t('User ID')}: {props.userId}
            </span>
            <StatusBadge variant='neutral' copyable={false}>
              {props.items.length} {t('rules')}
            </StatusBadge>
          </div>
          <div className='flex items-center gap-1'>
            <Button
              variant='ghost'
              size='sm'
              className='h-7 w-7 p-0'
              onClick={() => props.onAdd(props.userId)}
            >
              <Plus className='h-4 w-4' />
            </Button>
            <Button
              variant='ghost'
              size='sm'
              className='text-destructive h-7 w-7 p-0'
              onClick={() => props.onRemoveUser(props.userId)}
            >
              <Trash2 className='h-4 w-4' />
            </Button>
          </div>
        </div>
        <CollapsibleContent>
          <div className='space-y-2 border-t p-3'>
            {props.items.map((rule) => (
              <div key={rule._id} className='flex items-center gap-2'>
                <Select
                  value={rule.visible ? 'visible' : 'hidden'}
                  onValueChange={(v) =>
                    v !== null &&
                    props.onUpdate(rule._id, 'visible', v === 'visible')
                  }
                >
                  <SelectTrigger className='w-[130px]'>
                    <SelectValue>
                      <StatusBadge
                        label={rule.visible ? t('Extra visible') : t('Hidden')}
                        variant={rule.visible ? 'info' : 'danger'}
                        copyable={false}
                      />
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='visible'>
                        <StatusBadge
                          label={t('Extra visible')}
                          variant='info'
                          copyable={false}
                        />
                      </SelectItem>
                      <SelectItem value='hidden'>
                        <StatusBadge
                          label={t('Hidden')}
                          variant='danger'
                          copyable={false}
                        />
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <div className='flex flex-1 items-center gap-1.5'>
                  <GroupSelect
                    className='flex-1'
                    options={props.groupOptions}
                    value={rule.targetGroup}
                    placeholder={t('Group name')}
                    onValueChange={(v) =>
                      props.onUpdate(rule._id, 'targetGroup', v)
                    }
                  />
                  {rule.targetGroup &&
                    !props.groupOptions.includes(rule.targetGroup) && (
                      <AlertTriangle
                        className='text-destructive h-4 w-4 shrink-0'
                        aria-label={t('Not in pricing table')}
                      />
                    )}
                </div>
                {rule.visible ? (
                  <Input
                    className='flex-1'
                    value={rule.description}
                    placeholder={t('Description')}
                    onChange={(e) =>
                      props.onUpdate(rule._id, 'description', e.target.value)
                    }
                  />
                ) : (
                  <div className='text-muted-foreground flex-1 px-3 text-sm'>
                    -
                  </div>
                )}
                <Button
                  variant='ghost'
                  size='sm'
                  className='text-destructive h-8 w-8 p-0'
                  onClick={() => props.onRemove(rule._id)}
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            ))}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  )
}

export function UserSpecialUsableRulesEditor(
  props: UserSpecialUsableRulesEditorProps
) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<Rule[]>(() =>
    flattenRules(safeParseJson(props.value))
  )
  const [newUserId, setNewUserId] = useState('')

  const { onChange } = props
  const emitChange = useCallback(
    (newRules: Rule[]) => {
      setRules(newRules)
      onChange(serializeRules(newRules))
    },
    [onChange]
  )

  const updateRule = useCallback(
    (id: string, field: keyof Rule, val: string | boolean) => {
      emitChange(
        rules.map((r) => {
          if (r._id !== id) return r
          const updated = { ...r, [field]: val }
          if (field === 'visible' && val === false) {
            updated.description = 'remove'
          } else if (field === 'visible' && val === true && !r.visible) {
            if (updated.description === 'remove') updated.description = ''
          }
          return updated
        })
      )
    },
    [rules, emitChange]
  )

  const removeRule = useCallback(
    (id: string) => emitChange(rules.filter((r) => r._id !== id)),
    [rules, emitChange]
  )

  const removeUser = useCallback(
    (userId: string) => emitChange(rules.filter((r) => r.userId !== userId)),
    [rules, emitChange]
  )

  const addRuleToUser = useCallback(
    (userId: string) => {
      emitChange([
        ...rules,
        {
          _id: uid(),
          userId,
          visible: true,
          targetGroup: '',
          description: '',
        },
      ])
    },
    [rules, emitChange]
  )

  const grouped = useMemo(() => {
    const map: Record<string, Rule[]> = {}
    const order: string[] = []
    for (const r of rules) {
      if (!r.userId) continue
      if (!map[r.userId]) {
        map[r.userId] = []
        order.push(r.userId)
      }
      map[r.userId].push(r)
    }
    return order.map((id) => ({ id, items: map[id] }))
  }, [rules])

  const handleAddUser = useCallback(() => {
    const id = newUserId.trim()
    if (!id) return
    if (!grouped.some((g) => g.id === id)) {
      addRuleToUser(id)
    }
    setNewUserId('')
  }, [newUserId, grouped, addRuleToUser])

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Special usable group rules by user ID')}</CardTitle>
        <CardDescription>
          {t(
            'Make extra groups visible to, or hide default groups from, a specific user ID.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='space-y-3'>
          {grouped.length === 0 ? (
            <p className='text-muted-foreground py-4 text-center text-sm'>
              {t('No rules yet. Add a user ID below to get started.')}
            </p>
          ) : (
            grouped.map((group) => (
              <UserSection
                key={group.id}
                userId={group.id}
                items={group.items}
                groupOptions={props.groupOptions}
                onUpdate={updateRule}
                onRemove={removeRule}
                onAdd={addRuleToUser}
                onRemoveUser={removeUser}
              />
            ))
          )}

          <div className='flex items-center justify-center gap-2 pt-2'>
            <Input
              className='w-[200px]'
              placeholder={t('User ID')}
              value={newUserId}
              onChange={(e) => setNewUserId(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  handleAddUser()
                }
              }}
            />
            <Button
              variant='outline'
              size='sm'
              onClick={handleAddUser}
              disabled={!newUserId.trim()}
            >
              <Plus className='mr-2 h-4 w-4' />
              {t('Add rules for a user ID')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
