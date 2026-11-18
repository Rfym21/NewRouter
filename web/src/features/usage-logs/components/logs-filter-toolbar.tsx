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
import type { Table } from '@tanstack/react-table'
import { ChevronDown, Loader2, SlidersHorizontal } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ComponentProps,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'

import { DataTableViewOptions } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
  DrawerTrigger,
} from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { useMediaQuery } from '@/hooks'
import { cn } from '@/lib/utils'

interface LogsFilterToolbarProps<TData> {
  table: Table<TData>
  primaryFilters: ReactNode
  advancedFilters?: ReactNode
  compactMobile?: boolean
  mobilePinnedFilters?: ReactNode
  mobileFilters?: ReactNode
  mobileFilterCount?: number
  stats?: ReactNode
  actionStart?: ReactNode
  actionEnd?: ReactNode
  columnOptions?: ReactNode
  hasActiveFilters: boolean
  hasAdvancedActiveFilters?: boolean
  advancedFilterCount?: number
  searchLoading?: boolean
  onReset: () => void
  onSearch: () => void
  className?: string
}

interface LogsFilterFieldProps {
  children: ReactNode
  wide?: boolean
  className?: string
}

export function LogsFilterField(props: LogsFilterFieldProps) {
  return (
    <div
      className={cn(
        'min-w-0 [&_[data-slot=select-trigger]]:w-full [&_[data-slot=select-trigger]]:text-sm [&_[data-slot=select-value]]:leading-5',
        props.wide && 'sm:col-span-2',
        props.className
      )}
    >
      {props.children}
    </div>
  )
}

export function LogsFilterInput(props: ComponentProps<typeof Input>) {
  return (
    <Input
      {...props}
      autoComplete='off'
      className={cn('h-8 min-w-0 text-sm leading-5', props.className)}
    />
  )
}

const FAB_POSITION_KEY = 'newapi:logs-filter-fab:v1'

/** The saved spot of the floating filter button, when one was dragged. */
function readFabPosition(): { x: number; y: number } | null {
  try {
    const raw = localStorage.getItem(FAB_POSITION_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as { x?: number; y?: number }
    if (typeof parsed.x !== 'number' || typeof parsed.y !== 'number') return null
    return { x: parsed.x, y: parsed.y }
  } catch {
    return null
  }
}

export function LogsFilterToolbar<TData>(props: LogsFilterToolbarProps<TData>) {
  const { t } = useTranslation()
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [mobileFiltersOpen, setMobileFiltersOpen] = useState(false)
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [fabPosition, setFabPosition] = useState(readFabPosition)
  const [fabDragging, setFabDragging] = useState(false)
  const fabRef = useRef<HTMLDivElement | null>(null)
  const fabDrag = useRef<{
    pointerId: number
    startX: number
    startY: number
    originX: number
    originY: number
    moved: boolean
  } | null>(null)
  const suppressFabClick = useRef(false)

  const clampFab = useCallback((position: { x: number; y: number }) => {
    const element = fabRef.current
    const width = element?.offsetWidth ?? 96
    const height = element?.offsetHeight ?? 44
    const margin = 8
    return {
      x: Math.min(
        Math.max(position.x, margin),
        Math.max(margin, window.innerWidth - width - margin)
      ),
      y: Math.min(
        Math.max(position.y, margin),
        Math.max(margin, window.innerHeight - height - margin)
      ),
    }
  }, [])

  useEffect(() => {
    if (!fabPosition) return
    const onResize = () =>
      setFabPosition((current) => (current ? clampFab(current) : current))
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  }, [fabPosition, clampFab])

  const handleFabPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    const element = fabRef.current
    if (!element) return
    const rect = element.getBoundingClientRect()
    fabDrag.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      originX: rect.left,
      originY: rect.top,
      moved: false,
    }
    element.setPointerCapture?.(event.pointerId)
  }

  const handleFabPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = fabDrag.current
    if (!drag || drag.pointerId !== event.pointerId) return
    const dx = event.clientX - drag.startX
    const dy = event.clientY - drag.startY
    if (!drag.moved && Math.hypot(dx, dy) < 6) return
    drag.moved = true
    setFabDragging(true)
    setFabPosition(clampFab({ x: drag.originX + dx, y: drag.originY + dy }))
  }

  const handleFabPointerEnd = (event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = fabDrag.current
    if (!drag || drag.pointerId !== event.pointerId) return
    fabDrag.current = null
    setFabDragging(false)
    if (!drag.moved) return
    // A drag must not count as the tap that opens the panel.
    suppressFabClick.current = true
    setFabPosition((current) => {
      if (current) {
        try {
          localStorage.setItem(FAB_POSITION_KEY, JSON.stringify(current))
        } catch {
          // Keep the spot for this visit when storage is unavailable.
        }
      }
      return current
    })
  }

  const fabStyle: CSSProperties = fabPosition
    ? { left: fabPosition.x, top: fabPosition.y }
    : { right: '1rem', bottom: 'calc(env(safe-area-inset-bottom) + 1rem)' }


  const hasAdvancedFilters = props.advancedFilters != null
  const activeAdvancedCount =
    props.advancedFilterCount ?? (props.hasAdvancedActiveFilters ? 1 : 0)
  const activeMobileFilterCount = props.mobileFilterCount ?? activeAdvancedCount

  const handleMobileReset = () => {
    props.onReset()
    setMobileFiltersOpen(false)
  }

  const handleMobileSearch = () => {
    props.onSearch()
    setMobileFiltersOpen(false)
  }

  const advancedToggle = hasAdvancedFilters ? (
    <Button
      type='button'
      variant='ghost'
      onClick={() => setAdvancedOpen((open) => !open)}
      aria-expanded={advancedOpen}
      className={cn(
        'text-muted-foreground hover:text-foreground gap-1 px-2',
        props.hasAdvancedActiveFilters &&
          !advancedOpen &&
          'text-primary hover:text-primary'
      )}
    >
      {advancedOpen ? t('Collapse') : t('Expand')}
      {activeAdvancedCount > 0 && (
        <Badge className='ml-0.5 size-5 justify-center p-0 text-[10px]'>
          {activeAdvancedCount}
        </Badge>
      )}
      <ChevronDown
        className={cn(
          'size-3.5 transition-transform duration-200',
          advancedOpen && 'rotate-180'
        )}
      />
    </Button>
  ) : null

  // A phone keeps the toolbar out of the layout: one floating button opens a
  // panel with the filters and the QPD/QPM/RPM/TPM statistics, so the log list
  // keeps the screen instead of the controls.
  if (isMobile) {
    return (
      <Drawer
        modal={false}
        open={mobileFiltersOpen}
        onOpenChange={setMobileFiltersOpen}
      >
        <div
          ref={fabRef}
          className={cn(
            'fixed z-40 touch-none select-none',
            fabDragging ? 'cursor-grabbing' : 'cursor-grab'
          )}
          style={fabStyle}
          onPointerDown={handleFabPointerDown}
          onPointerMove={handleFabPointerMove}
          onPointerUp={handleFabPointerEnd}
          onPointerCancel={handleFabPointerEnd}
          onClickCapture={(event) => {
            // A drag must not count as the tap that opens the panel.
            if (!suppressFabClick.current) return
            suppressFabClick.current = false
            event.preventDefault()
            event.stopPropagation()
          }}
        >
          <DrawerTrigger asChild>
            <Button
              type='button'
              aria-label={t('Filter')}
              className={cn(
                'h-11 gap-1.5 rounded-full px-4 shadow-lg',
                activeMobileFilterCount > 0 &&
                  'ring-primary/30 ring-2 ring-offset-1'
              )}
            >
              <SlidersHorizontal className='size-4' aria-hidden='true' />
              {t('Filter')}
              {activeMobileFilterCount > 0 && (
                <Badge
                  aria-hidden='true'
                  className='ml-0.5 size-5 justify-center p-0 text-[10px]'
                >
                  {activeMobileFilterCount}
                </Badge>
              )}
            </Button>
          </DrawerTrigger>
        </div>

        <DrawerContent className='max-h-[85dvh] p-0'>
          <div className='mx-auto flex w-full max-w-md flex-1 flex-col overflow-hidden'>
            <DrawerHeader className='border-border/70 border-b px-4 py-3 text-left'>
              <DrawerTitle>{t('Filter')}</DrawerTitle>
              <DrawerDescription>
                {t('Adjust filters, then search to refresh the logs.')}
              </DrawerDescription>
            </DrawerHeader>
            <div className='flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 py-3'>
              {props.stats}
              {props.mobilePinnedFilters}
              {props.mobileFilters ?? (
                <>
                  {props.primaryFilters}
                  {props.advancedFilters}
                </>
              )}
              {(props.actionStart ||
                props.actionEnd ||
                props.columnOptions) && (
                <div className='flex flex-wrap items-center gap-2 border-t pt-3'>
                  {props.actionStart}
                  {props.actionEnd}
                  {props.columnOptions ?? (
                    <DataTableViewOptions table={props.table} />
                  )}
                </div>
              )}
            </div>
            <DrawerFooter className='border-border/70 grid grid-cols-2 gap-2 border-t px-4 py-3'>
              <Button
                type='button'
                variant='outline'
                onClick={handleMobileReset}
                disabled={!props.hasActiveFilters}
              >
                {t('Reset')}
              </Button>
              <Button
                type='button'
                onClick={handleMobileSearch}
                disabled={props.searchLoading}
                aria-busy={props.searchLoading}
              >
                {props.searchLoading && <Loader2 className='animate-spin' />}
                {t('Search')}
              </Button>
            </DrawerFooter>
          </div>
        </DrawerContent>
      </Drawer>
    )
  }

  return (
    <div
      className={cn(
        'bg-card/50 rounded-lg border p-2.5 sm:p-3',
        props.className
      )}
    >
      <div className='flex flex-wrap items-start gap-2'>
        <div className='grid min-w-0 flex-1 grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
          {props.primaryFilters}
        </div>
        {advancedToggle && (
          <div className='flex shrink-0 items-center justify-end'>
            {advancedToggle}
          </div>
        )}
      </div>

      {advancedOpen && props.advancedFilters && (
        <div className='mt-2 grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(10rem,1fr))]'>
          {props.advancedFilters}
        </div>
      )}

      <div className='mt-2 flex flex-wrap items-center gap-2'>
        {props.stats}
        <div className='ms-auto flex flex-wrap items-center justify-end gap-1.5 sm:gap-2'>
          {props.actionStart}
          <Button
            type='button'
            variant='outline'
            onClick={props.onReset}
            disabled={!props.hasActiveFilters}
          >
            {t('Reset')}
          </Button>
          <Button
            type='button'
            onClick={props.onSearch}
            disabled={props.searchLoading}
          >
            {props.searchLoading && <Loader2 className='animate-spin' />}
            {t('Search')}
          </Button>
          {props.actionEnd}
          {props.columnOptions ?? <DataTableViewOptions table={props.table} />}
        </div>
      </div>
    </div>
  )
}
