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
import { useTranslation } from 'react-i18next'

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { UpstreamFieldSync } from './upstream-field-sync'
import { UpstreamRatioSync } from './upstream-ratio-sync'

export function UpstreamPriceSync() {
  const { t } = useTranslation()
  return (
    <Tabs defaultValue='models' className='flex h-full min-h-0 flex-col gap-3'>
      <TabsList className='w-fit shrink-0'>
        <TabsTrigger value='models'>{t('Models')}</TabsTrigger>
        <TabsTrigger value='fields'>{t('Fields')}</TabsTrigger>
      </TabsList>
      <TabsContent value='models' className='min-h-0 flex-1 data-hidden:hidden'>
        <UpstreamRatioSync />
      </TabsContent>
      <TabsContent value='fields' className='min-h-0 flex-1 data-hidden:hidden'>
        <UpstreamFieldSync />
      </TabsContent>
    </Tabs>
  )
}
