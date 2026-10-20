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
import { useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { safeNumberFieldProps } from '../utils/numeric-field'

type LogExportFormValues = {
  LogExportMaxDays: number
  LogExportFileRetentionHours: number
}

export function LogExportFields() {
  const { t } = useTranslation()
  const form = useFormContext<LogExportFormValues>()

  return (
    <>
      <FormField
        control={form.control}
        name='LogExportMaxDays'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Max export range (days)')}</FormLabel>
            <FormControl>
              <Input
                type='number'
                min={1}
                max={365}
                step={1}
                {...safeNumberFieldProps(field)}
              />
            </FormControl>
            <FormDescription>
              {t('Maximum time span allowed for a single log export')}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name='LogExportFileRetentionHours'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('File retention (hours)')}</FormLabel>
            <FormControl>
              <Input
                type='number'
                min={1}
                max={8760}
                step={1}
                {...safeNumberFieldProps(field)}
              />
            </FormControl>
            <FormDescription>
              {t('How long a generated export file is kept before it expires')}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  )
}
