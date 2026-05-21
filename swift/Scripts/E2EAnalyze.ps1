$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$SwiftDir = Split-Path -Parent $ScriptDir
$PackageDir = Join-Path $SwiftDir 'WendyE2ETests'
$DefaultOutputDir = if ($env:WENDY_E2E_OUTPUT_DIR) {
    $env:WENDY_E2E_OUTPUT_DIR
} elseif ($env:SystemRoot) {
    Join-Path (Join-Path $env:SystemRoot 'Temp') 'wendy'
} else {
    Join-Path ([System.IO.Path]::GetTempPath()) 'wendy'
}

$OutputDir = $DefaultOutputDir
$OpenReport = $true
$Stage = 'all'

function Show-Usage {
    @"
Usage: E2EAnalyze.ps1 [--output-dir DIR] [--stage STAGE] [--open|--no-open]

Analyze raw Swift E2E runs found in an output directory.

Stages:
  aggregate  Aggregate raw runs into matching aggregate directories.
  review     Review existing aggregate results.
  report     Render existing aggregate HTML reports.
  all        Run aggregate, review, and report; default.
"@ | Write-Output
}

$i = 0
while ($i -lt $args.Count) {
    switch ($args[$i]) {
        '--output-dir' { $OutputDir = $args[$i + 1]; $i += 2; continue }
        '--stage' { $Stage = $args[$i + 1]; $i += 2; continue }
        '--open' { $OpenReport = $true; $i += 1; continue }
        '--no-open' { $OpenReport = $false; $i += 1; continue }
        '--help' { Show-Usage; exit 0 }
        '-h' { Show-Usage; exit 0 }
        default { throw "Unknown option: $($args[$i])" }
    }
}

$Stage = $Stage.ToLowerInvariant()
if ($Stage -notin @('aggregate', 'review', 'report', 'all')) {
    throw 'ERROR: --stage must be aggregate, review, report, or all.'
}

$OutputDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($OutputDir)
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$OutputDir = (Resolve-Path -LiteralPath $OutputDir).Path

function Test-RawRunDirectory([System.IO.DirectoryInfo]$Directory) {
    if ($Directory.Name -notmatch '\.\d{4}$') { return $false }
    $infoPath = Join-Path $Directory.FullName 'info.json'
    if (-not (Test-Path -LiteralPath $infoPath -PathType Leaf)) { return $false }
    try {
        $info = Get-Content -Raw -LiteralPath $infoPath | ConvertFrom-Json
        return $info.kind -ne 'swift-e2e-aggregate'
    } catch {
        return $false
    }
}

function Test-AggregateDirectory([System.IO.DirectoryInfo]$Directory) {
    $infoPath = Join-Path $Directory.FullName 'info.json'
    if (-not (Test-Path -LiteralPath $infoPath -PathType Leaf)) { return $false }
    try {
        $info = Get-Content -Raw -LiteralPath $infoPath | ConvertFrom-Json
        return $info.kind -eq 'swift-e2e-aggregate'
    } catch {
        return $false
    }
}

function Get-AggregateDirectoryForRun([string]$RunID) {
    $runBase = $RunID -replace '\.[^.]+$', ''
    $aggregateName = $runBase -replace '\.[^.]+$', ''
    return Join-Path $OutputDir $aggregateName
}

$runDirs = Get-ChildItem -LiteralPath $OutputDir -Directory |
    Where-Object { Test-RawRunDirectory $_ } |
    Sort-Object Name

if ($runDirs) {
    $aggregateDirs = $runDirs |
        ForEach-Object { Get-AggregateDirectoryForRun $_.Name } |
        Sort-Object -Unique
} else {
    $aggregateDirs = Get-ChildItem -LiteralPath $OutputDir -Directory |
        Where-Object { Test-AggregateDirectory $_ } |
        Sort-Object Name |
        ForEach-Object { $_.FullName }
}

if ($Stage -in @('aggregate', 'all') -and -not $runDirs) {
    throw "ERROR: no raw Swift E2E run directories found in $OutputDir."
}
if ($Stage -in @('review', 'report') -and -not $aggregateDirs) {
    throw "ERROR: no Swift E2E aggregate directories found in $OutputDir."
}

Write-Output '==> Analyzing Swift E2E runs'
Write-Output "    Stage:      $Stage"
Write-Output "    Output dir: $OutputDir"
$runDirs | ForEach-Object { Write-Output "    Run:        $($_.FullName)" }
$aggregateDirs | ForEach-Object { Write-Output "    Aggregate:  $_" }

$status = 0

if ($Stage -in @('aggregate', 'all')) {
    Push-Location $PackageDir
    try {
        & swift run swift-e2e-testing aggregate --output-dir $OutputDir @($runDirs | ForEach-Object { $_.FullName })
        $aggregateStatus = $LASTEXITCODE
    } finally {
        Pop-Location
    }
    if ($aggregateStatus -ne 0) { $status = $aggregateStatus }
}

if ($Stage -in @('review', 'all')) {
    foreach ($aggregateDir in $aggregateDirs) {
        & (Join-Path $ScriptDir 'E2EReview.ps1') --run-dir $aggregateDir
        $reviewStatus = $LASTEXITCODE
        if ($status -eq 0 -and $reviewStatus -ne 0) { $status = $reviewStatus }
    }
}

if ($Stage -in @('report', 'all')) {
    foreach ($aggregateDir in $aggregateDirs) {
        & (Join-Path $ScriptDir 'E2EReport.ps1') --run-dir $aggregateDir
        $reportStatus = $LASTEXITCODE
        if ($status -eq 0 -and $reportStatus -ne 0) { $status = $reportStatus }
    }

    $latestReport = $aggregateDirs |
        ForEach-Object { Join-Path $_ 'index.html' } |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Sort-Object { (Get-Item -LiteralPath $_).LastWriteTimeUtc } |
        Select-Object -Last 1

    if ($latestReport) {
        if ($OpenReport) {
            Start-Process $latestReport
        } else {
            Write-Output "HTML report: $latestReport"
        }
    } else {
        Write-Error 'HTML report not found in analyzed aggregate directories.'
        if ($status -eq 0) { $status = 1 }
    }
}

exit $status
