// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

type Notify = kit.Notify

const ContentValueParam = kit.ContentValueParam
const NotifyTypeQuotaExceed = kit.NotifyTypeQuotaExceed
const NotifyTypeChannelUpdate = kit.NotifyTypeChannelUpdate
const NotifyTypeChannelTest = kit.NotifyTypeChannelTest

var NewNotify = kit.NewNotify
