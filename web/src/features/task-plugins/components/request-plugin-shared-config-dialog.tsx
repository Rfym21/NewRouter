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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { TaskPluginOption } from '@/features/channels/api'

import { localizedPluginName } from '../lib/plugin-name'
import {
  fromRequestPluginConfigDraft,
  requestPluginConfigDefaults,
  toRequestPluginConfigDraft,
  type RequestPluginConfigDraft,
} from '../lib/request-plugins'
import { RequestPluginConfigForm } from './request-plugin-config-form'

type RequestPluginSharedConfigDialogProps = {
  open: boolean
  /** The request processing plugins that declare a shared config. */
  plugins: TaskPluginOption[]
  sharedConfigs: Record<string, unknown>
  saving: boolean
  /** The gateway's validation message for the last save. */
  error?: string
  onOpenChange: (open: boolean) => void
  onSave: (plugin: string, config: Record<string, unknown>) => void
}

/**
 * Edits the config one plugin shares with every instance of it. The plugin
 * reads it as ctx.shared.
 */
export function RequestPluginSharedConfigDialog(
  props: RequestPluginSharedConfigDialogProps
) {
  const { t, i18n } = useTranslation()
  const options = props.plugins.filter((plugin) => plugin.sharedConfigSchema)
  const [pluginKey, setPluginKey] = useState(options[0]?.key ?? '')
  const plugin = options.find((item) => item.key === pluginKey) ?? options[0]
  const schema = plugin?.sharedConfigSchema ?? undefined
  const [draft, setDraft] = useState<RequestPluginConfigDraft>(() =>
    toRequestPluginConfigDraft(
      schema,
      (props.sharedConfigs[plugin?.key ?? ''] as Record<string, unknown>) ??
        requestPluginConfigDefaults(schema)
    )
  )
  const [showAllErrors, setShowAllErrors] = useState(false)

  // Each opening starts from the saved config of the selected plugin.
  useEffect(() => {
    if (!props.open) return
    const next = options[0]?.key ?? ''
    setPluginKey(next)
    setShowAllErrors(false)
  }, [props.open]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!plugin) return
    const saved = props.sharedConfigs[plugin.key]
    setDraft(
      toRequestPluginConfigDraft(
        schema,
        (saved as Record<string, unknown> | undefined) ??
          requestPluginConfigDefaults(schema)
      )
    )
    setShowAllErrors(false)
  }, [pluginKey, plugin?.key, props.open, props.sharedConfigs]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!plugin) return null
  const parsed = fromRequestPluginConfigDraft(schema, draft)
  const save = () => {
    setShowAllErrors(true)
    if (Object.keys(parsed.errors).length > 0) return
    props.onSave(plugin.key, parsed.config)
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Shared plugin settings')}
      description={t(
        'Settings every instance of the plugin reads as ctx.shared. An instance config can override them.'
      )}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            disabled={props.saving}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' disabled={props.saving} onClick={save}>
            {props.saving ? t('Saving...') : t('Save')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        {options.length > 1 && (
          <Select
            value={plugin.key}
            onValueChange={(value) => {
              if (value) setPluginKey(value)
            }}
          >
            <SelectTrigger className='w-full'>
              <SelectValue placeholder={t('Choose a plugin')} />
            </SelectTrigger>
            <SelectContent>
              {options.map((option) => (
                <SelectItem key={option.key} value={option.key}>
                  {localizedPluginName(option, i18n.language)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {props.error && (
          <p className='text-destructive text-sm' role='alert'>
            {props.error}
          </p>
        )}
        <RequestPluginConfigForm
          pluginKey={plugin.key}
          schema={schema ?? undefined}
          draft={draft}
          errors={parsed.errors}
          showAllErrors={showAllErrors}
          disabled={props.saving}
          onChange={setDraft}
        />
      </div>
    </Dialog>
  )
}
