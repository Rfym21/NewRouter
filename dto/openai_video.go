// Compatibility aliases for protocol types extracted into RelayKit.
package dto

import kit "new-router/relaykit/dto"

const VideoStatusUnknown = kit.VideoStatusUnknown
const VideoStatusQueued = kit.VideoStatusQueued
const VideoStatusInProgress = kit.VideoStatusInProgress
const VideoStatusCompleted = kit.VideoStatusCompleted
const VideoStatusFailed = kit.VideoStatusFailed

type OpenAIVideo = kit.OpenAIVideo

var NewOpenAIVideo = kit.NewOpenAIVideo

type OpenAIVideoError = kit.OpenAIVideoError
