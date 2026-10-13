// Compatibility aliases for protocol types extracted into RelayKit.
package types

import kit "new-router/relaykit/types"

type FileSource = kit.FileSource
type URLSource = kit.URLSource
type Base64Source = kit.Base64Source

var NewURLFileSource = kit.NewURLFileSource
var NewBase64FileSource = kit.NewBase64FileSource
var NewFileSourceFromData = kit.NewFileSourceFromData

type CachedFileData = kit.CachedFileData

var NewMemoryCachedData = kit.NewMemoryCachedData
var NewDiskCachedData = kit.NewDiskCachedData
