// Compatibility aliases for protocol types extracted into RelayKit.
package types

import kit "new-router/relaykit/types"

type ConversionDiagnosticSeverity = kit.ConversionDiagnosticSeverity

const ConversionDiagnosticWarning = kit.ConversionDiagnosticWarning
const ConversionDiagnosticError = kit.ConversionDiagnosticError

type ConversionDiagnostic = kit.ConversionDiagnostic
type ConversionLossPolicy = kit.ConversionLossPolicy

const ConversionLossPolicySafe = kit.ConversionLossPolicySafe
const ConversionLossPolicyStrict = kit.ConversionLossPolicyStrict
const ConversionLossPolicyAllow = kit.ConversionLossPolicyAllow

type ConversionLossError = kit.ConversionLossError

var RejectConversionLoss = kit.RejectConversionLoss
