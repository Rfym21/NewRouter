param(
    [Parameter(Mandatory = $true)][string]$Endpoint,
    [Parameter(Mandatory = $true)][string]$Model,
    [double]$InputPricePerMillion = -1,
    [double]$ReadPricePerMillion = -1,
    [double]$WritePricePerMillion = -1,
    [double]$OutputPricePerMillion = -1
)

$ErrorActionPreference = 'Stop'
if (-not $env:ANONYMIZATION_EXPERIMENT_API_KEY) {
    throw 'Set ANONYMIZATION_EXPERIMENT_API_KEY in the environment before running.'
}

# Exactly four paid requests using synthetic text. Credentials stay in the
# environment and are never command-line arguments or measurement output.
$taskExperimentPrices = @($InputPricePerMillion, $ReadPricePerMillion, $WritePricePerMillion, $OutputPricePerMillion)
if (($taskExperimentPrices | Where-Object { $_ -ge 0 }).Count -gt 0 -and ($taskExperimentPrices | Where-Object { $_ -lt 0 }).Count -gt 0) {
    throw 'Supply all four prices in one currency, or omit all four.'
}
$taskExperimentNames = @('ANONYMIZATION_LIVE_EXPERIMENT', 'ANONYMIZATION_EXPERIMENT_URL', 'ANONYMIZATION_EXPERIMENT_MODEL', 'ANONYMIZATION_EXPERIMENT_PRICES')
$taskPreviousEnvironment = @{}
foreach ($taskName in $taskExperimentNames) {
    $taskPreviousEnvironment[$taskName] = [Environment]::GetEnvironmentVariable($taskName, 'Process')
}
Push-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
try {
    $env:ANONYMIZATION_LIVE_EXPERIMENT = '1'
    $env:ANONYMIZATION_EXPERIMENT_URL = $Endpoint
    $env:ANONYMIZATION_EXPERIMENT_MODEL = $Model
    if ($InputPricePerMillion -ge 0) {
        $env:ANONYMIZATION_EXPERIMENT_PRICES = @{ input = $InputPricePerMillion; read = $ReadPricePerMillion; write = $WritePricePerMillion; output = $OutputPricePerMillion } | ConvertTo-Json -Compress
    }
    else { $env:ANONYMIZATION_EXPERIMENT_PRICES = $null }
    & go test ./pkg/anonymizer -run '^TestLivePromptCacheExperiment$' -count=1 -v -timeout=10m
    if ($LASTEXITCODE -ne 0) { throw 'Prompt cache experiment failed; see the sanitized test output.' }
}
finally {
    Pop-Location
    foreach ($taskName in $taskExperimentNames) {
        [Environment]::SetEnvironmentVariable($taskName, $taskPreviousEnvironment[$taskName], 'Process')
    }
}
