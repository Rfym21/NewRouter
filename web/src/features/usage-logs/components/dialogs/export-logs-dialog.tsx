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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'

import { useLogsViewScope } from '../usage-logs-provider'
import { LogExportDialog } from './log-export-dialog'

/** Standalone entry point for the unified export dialog. */
export function ExportLogsDialog() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { canManageScope } = useLogsViewScope()
  const [open, setOpen] = useState(false)
  if (!canManageScope && status?.log_export_enabled === false) return null
  return (
    <>
      <Button variant='outline' onClick={() => setOpen(true)}>
        {t('Export')}
      </Button>
      {open && <LogExportDialog open={open} onOpenChange={setOpen} />}
    </>
  )
}
