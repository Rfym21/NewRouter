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
import type { IconBaseProps, IconType } from 'react-icons'
import {
  LuBadgeDollarSign,
  LuBanknote,
  LuCircleDollarSign,
  LuCoins,
  LuCreditCard,
  LuHandCoins,
  LuLandmark,
  LuPiggyBank,
  LuQrCode,
  LuReceipt,
  LuWallet,
} from 'react-icons/lu'
import {
  SiAlipay,
  SiAmericanexpress,
  SiApplepay,
  SiBinance,
  SiBitcoin,
  SiCashapp,
  SiDiscover,
  SiEthereum,
  SiGooglepay,
  SiJcb,
  SiKlarna,
  SiMastercard,
  SiPayoneer,
  SiPaypal,
  SiRevolut,
  SiSquare,
  SiStripe,
  SiTether,
  SiVisa,
  SiWechat,
  SiWise,
} from 'react-icons/si'

// 动态 import('react-icons/xx') 会把整个图标集打成数 MB 的 chunk 并经
// go:embed 嵌入后端二进制。此组件仅用于支付方式图标，故改为精选静态
// 映射（可被 tree-shake，仅打包所列图标）。支付方式配置引用了未收录的
// 图标名时渲染为空，与原先未知名称的降级行为一致；按需在此扩充映射。
const PAYMENT_ICONS: Record<string, IconType> = {
  LuBadgeDollarSign,
  LuBanknote,
  LuCircleDollarSign,
  LuCoins,
  LuCreditCard,
  LuHandCoins,
  LuLandmark,
  LuPiggyBank,
  LuQrCode,
  LuReceipt,
  LuWallet,
  SiAlipay,
  SiAmericanexpress,
  SiApplepay,
  SiBinance,
  SiBitcoin,
  SiCashapp,
  SiDiscover,
  SiEthereum,
  SiGooglepay,
  SiJcb,
  SiKlarna,
  SiMastercard,
  SiPayoneer,
  SiPaypal,
  SiRevolut,
  SiSquare,
  SiStripe,
  SiTether,
  SiVisa,
  SiWechat,
  SiWise,
}

type ReactIconByNameProps = IconBaseProps & {
  name?: string | null
}

export function ReactIconByName({ name, ...props }: ReactIconByNameProps) {
  const Icon = PAYMENT_ICONS[name?.trim() ?? '']
  if (!Icon) return null
  return <Icon {...props} />
}
