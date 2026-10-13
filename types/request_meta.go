// Compatibility aliases for protocol types extracted into RelayKit.
package types

import kit "new-router/relaykit/types"

type FileType = kit.FileType

const FileTypeImage = kit.FileTypeImage
const FileTypeAudio = kit.FileTypeAudio
const FileTypeVideo = kit.FileTypeVideo
const FileTypeFile = kit.FileTypeFile

type TokenType = kit.TokenType

const TokenTypeTextNumber = kit.TokenTypeTextNumber
const TokenTypeTokenizer = kit.TokenTypeTokenizer
const TokenTypeImage = kit.TokenTypeImage

type TokenCountMeta = kit.TokenCountMeta
type FileMeta = kit.FileMeta

var NewFileMeta = kit.NewFileMeta
var NewImageFileMeta = kit.NewImageFileMeta

type RequestMeta = kit.RequestMeta
