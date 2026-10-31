<#
.SYNOPSIS
  Arth API task runner for Windows (same task names as the Makefile).
.EXAMPLE
  ./scripts/dev.ps1 test
  ./scripts/dev.ps1 migrate-status
.NOTES
  Runs from apps/api regardless of the current directory. Programs load ./.env themselves; this script never reads it.
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet('run', 'worker', 'test', 'vet', 'build', 'migrate-up', 'migrate-down', 'migrate-status',
        'generate', 'sqlc', 'docker-build', 'help')]
    [string]$Task = 'help'
)

$ErrorActionPreference = 'Stop'
$ApiDir = Split-Path -Parent $PSScriptRoot
$SqlcImage = 'sqlc/sqlc:1.30.0'
$Image = 'arth-api:dev'

function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments)
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

function Invoke-Build {
    $prev = $env:CGO_ENABLED
    $env:CGO_ENABLED = '0'
    try {
        New-Item -ItemType Directory -Force (Join-Path $ApiDir 'bin') | Out-Null
        Invoke-Native go @('build', '-trimpath', '-ldflags', '-s -w', '-o', 'bin/api.exe', './cmd/api')
        Invoke-Native go @('build', '-trimpath', '-ldflags', '-s -w', '-o', 'bin/worker.exe', './cmd/worker')
    }
    finally { $env:CGO_ENABLED = $prev }
}

function Invoke-Sqlc {
    $src = (Resolve-Path $ApiDir).Path -replace '\\', '/'
    Invoke-Native docker @('run', '--rm', '-v', "${src}:/src", '-w', '/src', $SqlcImage, 'generate')
}

Push-Location $ApiDir
try {
    switch ($Task) {
        'run'            { Invoke-Native go @('run', './cmd/api') }
        'worker'         { Invoke-Native go @('run', './cmd/worker') }
        'test'           { Invoke-Native go @('test', './...') }
        'vet'            { Invoke-Native go @('vet', './...') }
        'build'          { Invoke-Build }
        'migrate-up'     { Invoke-Native go @('run', './cmd/api', 'migrate', 'up') }
        'migrate-down'   { Invoke-Native go @('run', './cmd/api', 'migrate', 'down') }
        'migrate-status' { Invoke-Native go @('run', './cmd/api', 'migrate', 'status') }
        'generate'       { Invoke-Native go @('generate', './...'); Invoke-Sqlc }
        'sqlc'           { Invoke-Sqlc }
        'docker-build'   { Invoke-Native docker @('build', '-t', $Image, '.') }
        'help' {
            Write-Output 'usage: ./scripts/dev.ps1 <task>'
            Write-Output 'tasks: run worker test vet build migrate-up migrate-down migrate-status generate sqlc docker-build'
        }
    }
}
finally { Pop-Location }
