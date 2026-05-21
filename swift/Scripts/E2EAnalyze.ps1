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

function Show-Usage {
    @"
Usage: E2EAnalyze.ps1 [--output-dir DIR] [--open|--no-open]

Analyze all raw Swift E2E runs found in an output directory.

The script deletes matching aggregate directories, aggregates raw runs,
reviews the aggregate results, renders HTML reports, and opens the newest
report.
"@ | Write-Output
}

$i = 0
while ($i -lt $args.Count) {
    switch ($args[$i]) {
        '--output-dir' { $OutputDir = $args[$i + 1]; $i += 2; continue }
        '--open' { $OpenReport = $true; $i += 1; continue }
        '--no-open' { $OpenReport = $false; $i += 1; continue }
        '--help' { Show-Usage; exit 0 }
        '-h' { Show-Usage; exit 0 }
        default { throw "Unknown option: $($args[$i])" }
    }
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

function Get-AggregateDirectoryForRun([string]$RunID) {
    $runBase = $RunID -replace '\.[^.]+$', ''
    $aggregateName = $runBase -replace '\.[^.]+$', ''
    return Join-Path $OutputDir $aggregateName
}

$runDirs = Get-ChildItem -LiteralPath $OutputDir -Directory |
    Where-Object { Test-RawRunDirectory $_ } |
    Sort-Object Name

if (-not $runDirs) {
    throw "ERROR: no raw Swift E2E run directories found in $OutputDir."
}

$aggregateDirs = $runDirs |
    ForEach-Object { Get-AggregateDirectoryForRun $_.Name } |
    Sort-Object -Unique

Write-Output '==> Analyzing Swift E2E runs'
Write-Output "    Output dir: $OutputDir"
$runDirs | ForEach-Object { Write-Output "    Run:        $($_.FullName)" }
$aggregateDirs | ForEach-Object { Write-Output "    Aggregate:  $_" }

$aggregateDirs | ForEach-Object {
    Remove-Item -LiteralPath $_ -Recurse -Force -ErrorAction SilentlyContinue
}

$status = 0
Push-Location $PackageDir
try {
    & swift run swift-e2e-testing aggregate --output-dir $OutputDir @($runDirs | ForEach-Object { $_.FullName })
    $aggregateStatus = $LASTEXITCODE
} finally {
    Pop-Location
}
if ($aggregateStatus -ne 0) { $status = $aggregateStatus }

foreach ($aggregateDir in $aggregateDirs) {
    & (Join-Path $ScriptDir 'E2EReview.ps1') --run-dir $aggregateDir
    $reviewStatus = $LASTEXITCODE
    if ($status -eq 0 -and $reviewStatus -ne 0) { $status = $reviewStatus }

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

exit $status
