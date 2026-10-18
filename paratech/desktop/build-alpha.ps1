<#
.SYNOPSIS
Builds the alpha of the Learning Assistant desktop app: the React UI, then the Windows binary with the UI embedded.

.DESCRIPTION
Runs `npm ci` and `npm run build` in desktop/frontend, then `go build -tags production -trimpath` of
./desktop/cmd/desktop with CGO_ENABLED=0 for windows/amd64. The exe goes to paratech\dist\alpha (ignored by git)
unless -OutDir says otherwise. Needs Go and Node.js/npm on PATH; installs nothing and needs no admin rights.
Environment variables are set only for the build and restored afterwards.

-Smoke then starts the exe headless with a throwaway data directory inside the output folder, waits for the
start log lines and stops the process again (a hard stop: see docs/qa/run-alpha.md).

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-alpha.ps1 -Smoke
#>
[CmdletBinding()]
param(
    # Output folder of the exe; relative paths are taken from the current directory.
    [string]$OutDir = '',
    # Start the built exe headless and check that it starts.
    [switch]$Smoke
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$desktopDir = $PSScriptRoot
$moduleRoot = Split-Path -Parent $desktopDir
$frontendDir = Join-Path $desktopDir 'frontend'
$exeName = 'learning-assistant.exe'

# Log lines that prove a successful start (internal/app). smoke-data is not the default data dir, so it must have its
# own Credential Manager profile and never reach the real secrets (ADR-0014).
$smokeLines = @('instance lock acquired', 'secrets_profile=isolated', 'background work started')
$smokeTimeoutSeconds = 60

function Write-Step([string]$Message) {
    Write-Host "==> $Message"
}

# Runs a native command and fails on a non-zero exit code. Native tools write progress and warnings to stderr;
# with 'Stop' Windows PowerShell 5.1 could turn such lines into errors, so only the exit code decides.
function Invoke-Native([string]$Exe, [string[]]$Arguments) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $Exe @Arguments
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    if ($code -ne 0) {
        throw "'$Exe $($Arguments -join ' ')' failed with exit code $code"
    }
}

function Find-Tool([string]$Name, [string]$Hint) {
    $cmd = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($null -eq $cmd) {
        throw "'$Name' was not found on PATH. $Hint This script installs nothing."
    }
    return $cmd.Source
}

# Sets process environment variables and returns their previous values for Restore-Env.
function Set-Env([hashtable]$Values) {
    $saved = @{}
    foreach ($name in $Values.Keys) {
        $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
        [Environment]::SetEnvironmentVariable($name, $Values[$name], 'Process')
    }
    return $saved
}

function Restore-Env([hashtable]$Saved) {
    foreach ($name in $Saved.Keys) {
        # $null removes a variable that did not exist before.
        [Environment]::SetEnvironmentVariable($name, $Saved[$name], 'Process')
    }
}

# Reads a file another process still writes to.
function Read-SharedText([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) {
        return ''
    }
    $stream = [System.IO.File]::Open($Path, 'Open', 'Read', 'ReadWrite')
    try {
        $reader = New-Object System.IO.StreamReader($stream)
        return $reader.ReadToEnd()
    } finally {
        $stream.Dispose()
    }
}

function Test-AllLines([string]$Text) {
    foreach ($line in $smokeLines) {
        if (-not $Text.Contains($line)) {
            return $false
        }
    }
    return $true
}

function Invoke-Smoke([string]$Exe, [string]$Dir) {
    $dataDir = Join-Path $Dir 'smoke-data'
    $stderrLog = Join-Path $Dir 'smoke.log'
    $stdoutLog = Join-Path $Dir 'smoke-stdout.log'
    Write-Step "Smoke: starting $exeName headless (data: $dataDir, log: $stderrLog)"

    # Flags win over LA_* variables of this session; the log level is fixed so that the info lines appear.
    $arguments = @('--headless', '--log-level', 'info', '--data-dir', ('"{0}"' -f $dataDir))
    $proc = $null
    $text = ''
    try {
        $proc = Start-Process -FilePath $Exe -ArgumentList $arguments -NoNewWindow -PassThru `
            -RedirectStandardError $stderrLog -RedirectStandardOutput $stdoutLog
        $deadline = (Get-Date).AddSeconds($smokeTimeoutSeconds)
        while ((Get-Date) -lt $deadline) {
            $text = Read-SharedText $stderrLog
            if ((Test-AllLines $text) -or $proc.HasExited) {
                break
            }
            Start-Sleep -Milliseconds 500
        }
        $text = Read-SharedText $stderrLog
    } finally {
        # Never leave the process running. Headless it has no window to close and PowerShell cannot send it
        # Ctrl+Break alone, so the stop is hard; the throwaway database and the instance lock survive that.
        if ($null -ne $proc -and -not $proc.HasExited) {
            # It may exit right after the check; that must not fail the script.
            Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
            $null = $proc.WaitForExit(10000)
        }
    }

    Write-Host '--- smoke log ---'
    Write-Host $text.TrimEnd()
    Write-Host '-----------------'
    if (-not (Test-AllLines $text)) {
        throw "Smoke failed: the log does not contain all of: $($smokeLines -join '; ')"
    }
    if ($text -match 'level=ERROR') {
        throw 'Smoke failed: the log has ERROR lines.'
    }
    Write-Step 'Smoke passed: the app started (stopped hard afterwards).'
}

$goExe = Find-Tool 'go' 'Install Go (see go.work for the version) and open a new terminal.'
# npm.cmd first, then npm.exe (Volta-style shims): the extensionless npm next to them is a sh script that
# & cannot run, and npm.ps1 depends on the execution policy. So only runnable Windows files are accepted.
$npmCmd = @('npm.cmd', 'npm.exe') | ForEach-Object {
    Get-Command $_ -CommandType Application -ErrorAction SilentlyContinue
} | Where-Object { @('.exe', '.cmd', '.bat') -contains $_.Extension } | Select-Object -First 1
if ($null -eq $npmCmd) {
    throw "'npm' was not found on PATH. Install Node.js 22 with npm and open a new terminal. This script installs nothing."
}
$npmExe = $npmCmd.Source

if ($OutDir -eq '') {
    $OutDir = Join-Path $moduleRoot 'dist\alpha'
} elseif (-not [System.IO.Path]::IsPathRooted($OutDir)) {
    $OutDir = Join-Path (Get-Location).ProviderPath $OutDir
}
$OutDir = [System.IO.Path]::GetFullPath($OutDir)
$exePath = Join-Path $OutDir $exeName

Write-Step "Frontend: npm ci and npm run build in $frontendDir"
Push-Location -LiteralPath $frontendDir
try {
    Invoke-Native $npmExe @('ci', '--no-audit', '--no-fund')
    Invoke-Native $npmExe @('run', 'build')
} finally {
    Pop-Location
}

Write-Step "Go: building $exePath (tags production, CGO_ENABLED=0, windows/amd64)"
$null = New-Item -ItemType Directory -Force -Path $OutDir
$saved = Set-Env @{ CGO_ENABLED = '0'; GOOS = 'windows'; GOARCH = 'amd64' }
try {
    Invoke-Native $goExe @('-C', $moduleRoot, 'build', '-tags', 'production', '-trimpath', '-o', $exePath, './desktop/cmd/desktop')
} finally {
    Restore-Env $saved
}

# The embedded build settings must show the release flags.
$info = (& $goExe version -m $exePath | Out-String)
if ($LASTEXITCODE -ne 0 -or -not $info.Contains('-tags=production') -or -not $info.Contains('CGO_ENABLED=0')) {
    throw "The built exe does not report -tags=production and CGO_ENABLED=0:`n$info"
}
$size = [math]::Round((Get-Item -LiteralPath $exePath).Length / 1MB, 1)
Write-Step "Built $exePath ($size MB)"

if ($Smoke) {
    Invoke-Smoke $exePath $OutDir
}
