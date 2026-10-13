// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

type ChannelSettings = kit.ChannelSettings

const HTTPProtocolAuto = kit.HTTPProtocolAuto
const HTTPProtocolHTTP1 = kit.HTTPProtocolHTTP1
const MaxHTTP2ConnectionShards = kit.MaxHTTP2ConnectionShards

type VertexKeyType = kit.VertexKeyType

const VertexKeyTypeJSON = kit.VertexKeyTypeJSON
const VertexKeyTypeAPIKey = kit.VertexKeyTypeAPIKey

type AwsKeyType = kit.AwsKeyType

const AwsKeyTypeAKSK = kit.AwsKeyTypeAKSK
const AwsKeyTypeApiKey = kit.AwsKeyTypeApiKey

type ClaudeCodeSimulationConfig = kit.ClaudeCodeSimulationConfig
type ChannelOtherSettings = kit.ChannelOtherSettings
type FieldPassThroughRule = kit.FieldPassThroughRule

const AdvancedCustomAuthTypeNone = kit.AdvancedCustomAuthTypeNone
const AdvancedCustomAuthTypeHeader = kit.AdvancedCustomAuthTypeHeader
const AdvancedCustomAuthTypeQuery = kit.AdvancedCustomAuthTypeQuery

type AdvancedCustomConfig = kit.AdvancedCustomConfig
type AdvancedCustomRoute = kit.AdvancedCustomRoute
type AdvancedCustomRouteAuth = kit.AdvancedCustomRouteAuth

const AdvancedCustomModelListPath = kit.AdvancedCustomModelListPath
const AdvancedCustomBalancePath = kit.AdvancedCustomBalancePath

var IsAdvancedCustomConverterAllowed = kit.IsAdvancedCustomConverterAllowed
