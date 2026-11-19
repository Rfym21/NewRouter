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
import {
  Bird,
  Bot,
  Box,
  Braces,
  Cloud,
  Code,
  Download,
  Hammer,
  MessageCircle,
  MessageSquare,
  MonitorPlay,
  Network,
  PawPrint,
  Plug,
  Repeat2,
  Route,
  Router,
  Send,
  Sparkles,
  Terminal,
  Waves,
  Waypoints,
  Wheat,
  Workflow,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { IconType } from 'react-icons'
import {
  SiAndroidstudio,
  SiApple,
  SiArc,
  SiAxios,
  SiBrave,
  SiBruno,
  SiBun,
  SiCurl,
  SiDart,
  SiDeno,
  SiDotnet,
  SiEclipseide,
  SiElectron,
  SiFirefox,
  SiGo,
  SiGoogle,
  SiGooglechrome,
  SiHoppscotch,
  SiHttpie,
  SiInsomnia,
  SiJetbrains,
  SiKotlin,
  SiNeovim,
  SiNextdotjs,
  SiNodedotjs,
  SiOpenjdk,
  SiOpera,
  SiPerl,
  SiPhp,
  SiPostman,
  SiPuppeteer,
  SiPython,
  SiQodo,
  SiRaycast,
  SiRuby,
  SiRust,
  SiSafari,
  SiSelenium,
  SiSwift,
  SiSymfony,
  SiVim,
  SiVivaldi,
  SiWarp,
  SiXcode,
  SiZedindustries,
} from 'react-icons/si'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

const SIMPLE_ICONS: Record<string, IconType> = {
  SiAndroidstudio,
  SiApple,
  SiArc,
  SiAxios,
  SiBrave,
  SiBruno,
  SiBun,
  SiCurl,
  SiDart,
  SiDeno,
  SiDotnet,
  SiEclipseide,
  SiElectron,
  SiFirefox,
  SiGo,
  SiGoogle,
  SiGooglechrome,
  SiHoppscotch,
  SiHttpie,
  SiInsomnia,
  SiJetbrains,
  SiKotlin,
  SiNeovim,
  SiNextdotjs,
  SiNodedotjs,
  SiOpenjdk,
  SiOpera,
  SiPerl,
  SiPhp,
  SiPostman,
  SiPuppeteer,
  SiPython,
  SiQodo,
  SiRaycast,
  SiRuby,
  SiRust,
  SiSafari,
  SiSelenium,
  SiSwift,
  SiSymfony,
  SiVim,
  SiVivaldi,
  SiWarp,
  SiXcode,
  SiZedindustries,
}

const LUCIDE_ICONS: Record<string, LucideIcon> = {
  Bird,
  Bot,
  Box,
  Braces,
  Cloud,
  Code,
  Download,
  Hammer,
  MessageCircle,
  MessageSquare,
  MonitorPlay,
  Network,
  PawPrint,
  Plug,
  Repeat2,
  Route,
  Router,
  Send,
  Sparkles,
  Terminal,
  Waves,
  Waypoints,
  Wheat,
  Workflow,
  Zap,
}

function clientIcon(icon: string | undefined, size: number) {
  if (!icon) return null
  const [kind, key] = icon.split(':', 2)
  if (kind === 'lobe' && key) return getLobeIcon(key, size)
  if (kind === 'si' && key) {
    const Icon = SIMPLE_ICONS[key]
    return Icon ? <Icon className='size-full' aria-hidden='true' /> : null
  }
  if (kind === 'lu' && key) {
    const Icon = LUCIDE_ICONS[key]
    return Icon ? <Icon className='size-full' aria-hidden='true' /> : null
  }
  return null
}

export function ClientBadge(props: {
  name: string
  icon?: string
  rawUserAgent?: string
  className?: string
}) {
  const { t } = useTranslation()
  const label = props.name === 'Other' ? t('Other') : props.name
  const icon = clientIcon(props.icon, 14)
  const fallback = (label || '?').slice(0, 1).toUpperCase()

  const badge = (
    <span
      className={cn(
        'border-border/60 bg-muted/30 text-foreground inline-flex h-6 max-w-[180px] items-center gap-1.5 rounded-md border px-2 text-xs',
        props.className
      )}
    >
      <span className='flex size-3.5 shrink-0 items-center justify-center overflow-hidden'>
        {icon ?? (
          <span className='bg-muted text-muted-foreground flex size-3.5 items-center justify-center rounded-full text-[9px] font-semibold'>
            {fallback}
          </span>
        )}
      </span>
      <span className='truncate'>{label}</span>
    </span>
  )

  if (!props.rawUserAgent) return badge

  return (
    <TooltipProvider delay={300}>
      <Tooltip>
        <TooltipTrigger render={badge} />
        <TooltipContent side='top' className='max-w-sm break-all'>
          {props.rawUserAgent}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
