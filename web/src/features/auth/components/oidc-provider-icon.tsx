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
import { Shield01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useState } from 'react'

import { cn } from '@/lib/utils'

type OidcProviderIconProps = {
  iconUrl?: string
  className?: string
  'data-icon'?: 'inline-start' | 'inline-end'
}

/**
 * Renders a custom OIDC provider icon with a shield fallback.
 *
 * @param props - Remote icon URL and presentation attributes.
 * @returns The provider icon or a shield when the URL is unavailable.
 */
export function OidcProviderIcon(props: OidcProviderIconProps) {
  const iconUrl = props.iconUrl?.trim()
  const [failedIconUrl, setFailedIconUrl] = useState<string | null>(null)

  if (!iconUrl || failedIconUrl === iconUrl) {
    return (
      <HugeiconsIcon
        icon={Shield01Icon}
        strokeWidth={2}
        aria-hidden='true'
        data-icon={props['data-icon']}
        className={cn('size-4', props.className)}
      />
    )
  }

  return (
    <img
      src={iconUrl}
      alt=''
      aria-hidden='true'
      referrerPolicy='no-referrer'
      data-icon={props['data-icon']}
      className={cn('size-4 object-contain', props.className)}
      onError={() => setFailedIconUrl(iconUrl)}
    />
  )
}
