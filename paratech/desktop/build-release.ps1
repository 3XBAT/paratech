<#
.SYNOPSIS
Builds the Windows installer of the Learning Assistant desktop app (NSIS, per-user, no admin rights).

.DESCRIPTION
Reads the version from desktop\VERSION (X.Y.Z; -Version overrides it). If HEAD has a vX.Y.Z git tag, it must match.
Then it finds makensis (NSIS 3.x) before anything else, builds the app with build-alpha.ps1 into
paratech\dist\release\<version>\app (FRONTEND_DEVSERVER_URL, GOFLAGS, GOAMD64, GOEXPERIMENT and GOWORK cleared for
the build), checks the exe with `go version -m`, optionally adds the whisper-cli files, and runs makensis on
desktop\build\windows\nsis\project.nsi. The plan shows the HEAD commit and whether the tree is dirty. The installer is
paratech\dist\release\learning-assistant-<version>-windows-amd64-setup.exe (ignored by git).

The WebView2 bootstrapper (MicrosoftEdgeWebview2Setup.exe) is taken from the Wails module in the Go module cache
(the file `wails3 generate webview2bootstrapper` writes); the script downloads nothing itself.
Needs Go, Node.js/npm and NSIS 3.x; installs nothing and needs no admin rights. Environment variables are set only
for the build and restored afterwards. No auto-update. See docs/refs/packaging.md.

GNU-style --help and --dry-run work too; any other unknown argument is an error.

.PARAMETER Version
Version X.Y.Z to build instead of the one in desktop\VERSION.

.PARAMETER WhisperDir
Folder with whisper-cli.exe, whisper.dll, ggml.dll, ggml-base.dll and ggml-cpu-*.dll (the Release folder of
whisper-bin-x64.zip, docs/qa/run-alpha.md section 6). Whisper files already staged are removed first. Without it the
installer has no whisper files.

.PARAMETER SkipAppBuild
Reuse the app already staged in paratech\dist\release\<version>\app by an earlier run (for example after signing
learning-assistant.exe there) instead of building it again. Its `go version -m` must still show a release build.

.PARAMETER DryRun
Print the plan (version, paths, tools found, commands, installer contents) and exit 0 without building.
-WhatIf and --dry-run do the same.

.PARAMETER Help
Print this help. --help does the same.

.PARAMETER ExtraArgs
Not set by name: takes the GNU-style --help and --dry-run; anything else is an error.

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-release.ps1 -DryRun

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-release.ps1 -WhisperDir $env:USERPROFILE\Downloads\whisper-bin-x64\Release
#>
[CmdletBinding(PositionalBinding = $false, SupportsShouldProcess = $true)]
param(
    [string]$Version = '',
    [string]$WhisperDir = '',
    [switch]$SkipAppBuild,
    [switch]$DryRun,
    [switch]$Help,
    # powershell -File binds --help to -Help itself, but not --dry-run (or anything with -Command).
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$ExtraArgs = @()
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$desktopDir = $PSScriptRoot
$moduleRoot = Split-Path -Parent $desktopDir
$exeName = 'learning-assistant.exe'
$alphaScript = Join-Path $desktopDir 'build-alpha.ps1'
$versionFile = Join-Path $desktopDir 'VERSION'
$nsiFile = Join-Path $desktopDir 'build\windows\nsis\project.nsi'
$releaseRoot = Join-Path $moduleRoot 'dist\release'
$wailsModule = 'github.com/wailsapp/wails/v3'
$bootstrapperRel = 'internal\commands\webview2\MicrosoftEdgeWebview2Setup.exe'
$whisperFixed = @('whisper-cli.exe', 'whisper.dll', 'ggml.dll', 'ggml-base.dll')
$whisperCpuPattern = 'ggml-cpu-*.dll'
$nsisHint = 'Install NSIS 3.x from https://nsis.sourceforge.io/Download (makensis is looked up on PATH, then in ' +
    'Program Files (x86)\NSIS and Program Files\NSIS) and open a new terminal. This script installs nothing.'
# A release must never take the UI from a dev server (the production tag ignores it as well) or pick up build
# settings of the shell. A cleared GOWORK means the default lookup: build-alpha.ps1 runs `go -C paratech`, which
# finds paratech\go.work. build-alpha.ps1 sets CGO_ENABLED, GOOS and GOARCH itself.
$buildEnv = @{ FRONTEND_DEVSERVER_URL = $null; GOFLAGS = $null; GOAMD64 = $null; GOEXPERIMENT = $null; GOWORK = $null }
$buildEnvNames = 'FRONTEND_DEVSERVER_URL, GOFLAGS, GOAMD64, GOEXPERIMENT and GOWORK'
$whisperStale = $whisperFixed + @($whisperCpuPattern)

function Write-Step([string]$Message) {
    Write-Host "==> $Message"
}

# Runs a native command and fails on a non-zero exit code (see build-alpha.ps1: only the exit code decides).
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

# Runs a native command and returns @{ Code; Lines } (stdout only). A hashtable, because PowerShell unrolls a
# returned one-line array into a string.
function Invoke-Capture([string]$Exe, [string[]]$Arguments) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & $Exe @Arguments 2>$null
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
    $lines = @()
    if ($null -ne $out) {
        $lines = @($out | ForEach-Object { "$_".Trim() } | Where-Object { $_ -ne '' })
    }
    return @{ Code = $code; Lines = $lines }
}

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
        [Environment]::SetEnvironmentVariable($name, $Saved[$name], 'Process')
    }
}

function Find-App([string]$Name) {
    $cmd = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue |
        Where-Object { $_.Extension -eq '.exe' } | Select-Object -First 1
    if ($null -eq $cmd) {
        return $null
    }
    return $cmd.Source
}

function Find-Makensis {
    $onPath = Find-App 'makensis'
    if ($null -ne $onPath) {
        return $onPath
    }
    foreach ($base in @(${env:ProgramFiles(x86)}, $env:ProgramFiles)) {
        if ([string]::IsNullOrEmpty($base)) {
            continue
        }
        $candidate = Join-Path $base 'NSIS\makensis.exe'
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }
    return $null
}

# Same rule as build-alpha.ps1: npm.cmd first, then npm.exe.
function Find-Npm {
    $cmd = @('npm.cmd', 'npm.exe') | ForEach-Object {
        Get-Command $_ -CommandType Application -ErrorAction SilentlyContinue
    } | Where-Object { @('.exe', '.cmd', '.bat') -contains $_.Extension } | Select-Object -First 1
    if ($null -eq $cmd) {
        return $null
    }
    return $cmd.Source
}

function Resolve-FullPath([string]$Path) {
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        $Path = Join-Path (Get-Location).ProviderPath $Path
    }
    return [System.IO.Path]::GetFullPath($Path)
}

# Returns the whisper files of a folder; fails if one is missing.
function Get-WhisperFiles([string]$Dir) {
    if (-not (Test-Path -LiteralPath $Dir -PathType Container)) {
        throw "Whisper folder '$Dir' does not exist."
    }
    $files = @()
    foreach ($name in $whisperFixed) {
        $path = Join-Path $Dir $name
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            throw "'$name' is missing in '$Dir'. Take the Release folder of whisper-bin-x64.zip (docs/qa/run-alpha.md section 6)."
        }
        $files += $path
    }
    $cpu = @(Get-ChildItem -LiteralPath $Dir -Filter $whisperCpuPattern -File | Sort-Object Name)
    if ($cpu.Count -eq 0) {
        throw "No $whisperCpuPattern in '$Dir'. Take the Release folder of whisper-bin-x64.zip (docs/qa/run-alpha.md section 6)."
    }
    $files += @($cpu | ForEach-Object { $_.FullName })
    return $files
}

# The bootstrapper embedded in the Wails module (internal/commands/generate_webview2.go). go.sum is checked only
# when the module is downloaded, not when it is read from the cache later; the Authenticode check is the guard at
# build time.
function Find-Bootstrapper([string]$GoExe) {
    if ($null -eq $GoExe) {
        return $null
    }
    $saved = Set-Env $buildEnv
    try {
        $r = Invoke-Capture $GoExe @('-C', $moduleRoot, 'list', '-m', '-f', '{{.Dir}}', $wailsModule)
    } finally {
        Restore-Env $saved
    }
    # Dir is empty while the module is not in the cache.
    if ($r.Code -ne 0 -or $r.Lines.Count -eq 0) {
        return $null
    }
    $path = Join-Path $r.Lines[0] $bootstrapperRel
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        return $null
    }
    return $path
}

function Test-MicrosoftSignature([string]$Path) {
    $sig = Get-AuthenticodeSignature -LiteralPath $Path
    return ($sig.Status -eq 'Valid' -and $null -ne $sig.SignerCertificate -and
        $sig.SignerCertificate.Subject -match '(^|, )O=Microsoft Corporation(,|$)')
}

# Parsed is false when `makensis -VERSION` fails or prints something that is not vX.Y.
function Get-MakensisMajor([string]$Makensis) {
    $r = Invoke-Capture $Makensis @('-VERSION')
    if ($r.Code -ne 0) {
        return @{ Text = "'makensis -VERSION' failed (exit code $($r.Code))"; Major = 0; Parsed = $false }
    }
    if ($r.Lines.Count -eq 0) {
        return @{ Text = "'makensis -VERSION' printed nothing"; Major = 0; Parsed = $false }
    }
    $text = $r.Lines[0]
    if ($text -match '^v?(\d+)\.') {
        return @{ Text = $text; Major = [int]$Matches[1]; Parsed = $true }
    }
    return @{ Text = "'makensis -VERSION' printed '$text'"; Major = 0; Parsed = $false }
}

# Returns the problems of the build settings embedded in the exe (`go version -m`); empty when it is a release
# build. Same strings as build-alpha.ps1. GOAMD64 must be v1 (the default) when present: testers have older CPUs,
# and v2+ binaries do not start there.
function Get-ReleaseExeProblems([string]$GoExe, [string]$Exe) {
    $saved = Set-Env $buildEnv
    try {
        $r = Invoke-Capture $GoExe @('version', '-m', $Exe)
    } finally {
        Restore-Env $saved
    }
    if ($r.Code -ne 0) {
        return @("'go version -m' failed with exit code $($r.Code) (not a Go binary?)")
    }
    $info = $r.Lines -join "`n"
    $problems = @()
    if (-not $info.Contains('-tags=production')) {
        $problems += 'no -tags=production'
    }
    if (-not $info.Contains('CGO_ENABLED=0')) {
        $problems += 'no CGO_ENABLED=0'
    }
    foreach ($m in [regex]::Matches($info, '(?m)^build\s+GOAMD64=(\S*)$')) {
        if ($m.Groups[1].Value -ne 'v1') {
            $problems += "GOAMD64=$($m.Groups[1].Value) (only v1 runs on older CPUs)"
        }
    }
    return $problems
}

# Removes whisper files left in the staging folder by an earlier run, so a smaller set of ggml-cpu-*.dll from
# -WhisperDir does not get packed together with stale ones.
function Remove-StagedWhisper([string]$Dir) {
    if (-not (Test-Path -LiteralPath $Dir -PathType Container)) {
        return
    }
    foreach ($pattern in $whisperStale) {
        Get-ChildItem -LiteralPath $Dir -Filter $pattern -File | ForEach-Object {
            Write-Host "    removing the staged $($_.Name)"
            Remove-Item -LiteralPath $_.FullName -Force
        }
    }
}

# --- arguments ---------------------------------------------------------------------------------------------------

$showHelp = $Help.IsPresent
$dry = $DryRun.IsPresent -or [bool]$WhatIfPreference
foreach ($arg in $ExtraArgs) {
    switch ($arg) {
        '--help' { $showHelp = $true }
        '--dry-run' { $dry = $true }
        default { throw "Unknown argument '$arg'. Valid: -Version X.Y.Z, -WhisperDir <folder>, -SkipAppBuild, -DryRun (--dry-run, -WhatIf), -Help (--help)." }
    }
}
if ($showHelp) {
    Get-Help -Name $PSCommandPath -Detailed | Out-String -Width 120 | Write-Host
    exit 0
}

# --- version -----------------------------------------------------------------------------------------------------

if ($Version -ne '') {
    $versionSource = '-Version'
} else {
    if (-not (Test-Path -LiteralPath $versionFile -PathType Leaf)) {
        throw "Version file '$versionFile' is missing; create it (X.Y.Z) or pass -Version."
    }
    $Version = [System.IO.File]::ReadAllText($versionFile)
    $versionSource = $versionFile
}
$Version = $Version.Trim()
if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version '$Version' (from $versionSource) is not X.Y.Z with digits only, e.g. 0.1.0."
}
foreach ($part in $Version.Split('.')) {
    # NSIS VIProductVersion takes X.Y.Z.0 with 16-bit parts.
    if ($part.Length -gt 5 -or [int]$part -gt 65535) {
        throw "Version '$Version': every part must be at most 65535."
    }
}

$gitExe = Find-App 'git'
$commitText = 'unknown (git not found)'
$dirtyTagWarning = $null
if ($null -eq $gitExe) {
    $tagNote = 'git not found: tag check skipped'
} else {
    $tags = Invoke-Capture $gitExe @('-C', $moduleRoot, 'tag', '--points-at', 'HEAD')
    if ($tags.Code -ne 0) {
        $tagNote = 'not a git checkout: tag check skipped'
        $commitText = 'unknown (not a git checkout)'
    } else {
        $versionTags = @($tags.Lines | Where-Object { $_ -match '^v\d+\.\d+\.\d+$' })
        $tagged = $false
        if ($versionTags.Count -eq 0) {
            $tagNote = 'HEAD has no vX.Y.Z tag'
        } elseif ($versionTags -contains "v$Version") {
            $tagNote = "HEAD is tagged v$Version"
            $tagged = $true
        } else {
            throw "HEAD is tagged $($versionTags -join ', ') but the version is $Version (from $versionSource). Update desktop\VERSION or pass -Version."
        }
        $head = Invoke-Capture $gitExe @('-C', $moduleRoot, 'rev-parse', '--short', 'HEAD')
        $commitText = 'unknown'
        if ($head.Code -eq 0 -and $head.Lines.Count -gt 0) {
            $commitText = $head.Lines[0]
        }
        # Covers the whole working tree, not only paratech\ (untracked files count as well).
        $status = Invoke-Capture $gitExe @('-C', $moduleRoot, 'status', '--porcelain')
        if ($status.Code -ne 0) {
            $commitText += ' (tree state unknown)'
        } elseif ($status.Lines.Count -gt 0) {
            $commitText += " (DIRTY: $($status.Lines.Count) uncommitted changes)"
            if ($tagged) {
                $dirtyTagWarning = "HEAD is tagged v$Version but the tree has uncommitted changes: the installer is not exactly v$Version."
            }
        } else {
            $commitText += ' (clean)'
        }
    }
}

# --- tools and inputs --------------------------------------------------------------------------------------------

$stageDir = Join-Path $releaseRoot $Version
$appDir = Join-Path $stageDir 'app'
$installer = Join-Path $releaseRoot "learning-assistant-$Version-windows-amd64-setup.exe"

$makensis = Find-Makensis
$nsisVersion = $null
if ($null -ne $makensis) {
    $nsisVersion = Get-MakensisMajor $makensis
}
$goExe = Find-App 'go'
$npmExe = Find-Npm
$bootstrapper = Find-Bootstrapper $goExe
$bootstrapperSigned = $false
if ($null -ne $bootstrapper) {
    $bootstrapperSigned = Test-MicrosoftSignature $bootstrapper
}
$whisperFiles = @()
if ($WhisperDir -ne '') {
    $WhisperDir = Resolve-FullPath $WhisperDir
    # The stale files of the staging folder are removed before the copy.
    if ($WhisperDir.TrimEnd('\') -ieq $appDir) {
        throw "-WhisperDir must not be the staging folder '$appDir'; pass the Release folder of whisper-bin-x64.zip."
    }
    $whisperFiles = @(Get-WhisperFiles $WhisperDir)
}

# --- plan --------------------------------------------------------------------------------------------------------

function Format-Found([object]$Value, [string]$Missing) {
    if ($null -eq $Value) {
        return "NOT FOUND - $Missing"
    }
    return [string]$Value
}

$makensisText = Format-Found $makensis $nsisHint
if ($null -ne $makensis) {
    $makensisText = "$makensis ($($nsisVersion.Text))"
    if (-not $nsisVersion.Parsed) {
        $makensisText += ' (version not readable; NSIS 3.x needed)'
    } elseif ($nsisVersion.Major -lt 3) {
        $makensisText += ' (NSIS 3.x needed)'
    }
}
$bootstrapperText = Format-Found $bootstrapper "no $wailsModule in the Go module cache: run 'go -C paratech mod download' (or any build) first"
if ($null -ne $bootstrapper) {
    if ($bootstrapperSigned) {
        $bootstrapperText = "$bootstrapper (signed by Microsoft Corporation)"
    } else {
        $bootstrapperText = "$bootstrapper (NOT a valid Microsoft signature)"
    }
}
$nsisArgs = @('-INPUTCHARSET', 'UTF8', "-DVERSION=$Version", "-DAPP_DIR=$appDir",
    "-DWEBVIEW2_BOOTSTRAPPER=$(if ($null -ne $bootstrapper) { $bootstrapper } else { '<bootstrapper>' })",
    "-DOUT_FILE=$installer")

if ($dry) {
    Write-Host 'Learning Assistant release build: DRY RUN (nothing is built or written)'
} else {
    Write-Host 'Learning Assistant release build'
}
Write-Host "  version:      $Version (from $versionSource; $tagNote)"
Write-Host "  commit:       $commitText"
if ($null -ne $dirtyTagWarning) {
    Write-Warning $dirtyTagWarning
}
Write-Host "  makensis:     $makensisText"
Write-Host "  go:           $(Format-Found $goExe 'install Go (see go.work) and open a new terminal')"
Write-Host "  npm:          $(Format-Found $npmExe 'install Node.js 22 with npm and open a new terminal')"
Write-Host "  WebView2:     $bootstrapperText"
Write-Host "  staging:      $appDir"
Write-Host "  installer:    $installer"
# Whisper files the installer will get: copied from -WhisperDir, or (with -SkipAppBuild) already staged.
$plannedWhisper = @($whisperFiles | ForEach-Object { Split-Path -Leaf $_ })
if ($plannedWhisper.Count -eq 0 -and $SkipAppBuild -and (Test-Path -LiteralPath (Join-Path $appDir 'whisper-cli.exe') -PathType Leaf)) {
    $plannedWhisper = @(Get-WhisperFiles $appDir | ForEach-Object { Split-Path -Leaf $_ })
}
$whisperDefine = ''
if ($plannedWhisper.Count -gt 0) {
    $whisperDefine = ' -DWITH_WHISPER'
}

Write-Host '  steps:'
if ($SkipAppBuild) {
    Write-Host "    - reuse $appDir\$exeName (-SkipAppBuild)"
} else {
    Write-Host "    - remove $appDir"
    Write-Host "    - $alphaScript -OutDir $appDir   ($buildEnvNames cleared)"
    Write-Host '      = npm ci, npm run build, go build -tags production -trimpath (CGO_ENABLED=0, windows/amd64)'
}
Write-Host "    - go version -m $appDir\$exeName   (must show -tags=production, CGO_ENABLED=0, GOAMD64 v1 if set)"
if ($whisperFiles.Count -gt 0) {
    Write-Host "    - remove the staged whisper files ($($whisperStale -join ', ')) from $appDir"
    Write-Host "    - copy the whisper files from $WhisperDir"
}
Write-Host "    - $(if ($null -ne $makensis) { $makensis } else { 'makensis' }) $($nsisArgs -join ' ')$whisperDefine $nsiFile"
Write-Host '  installer contents:'
Write-Host "    $exeName, uninstall.exe (written at install)"
foreach ($name in $plannedWhisper) {
    Write-Host "    $name"
}
if ($plannedWhisper.Count -eq 0) {
    Write-Host '    (no whisper files: copy them into the install folder later, docs/refs/packaging.md)'
}
Write-Host '    MicrosoftEdgeWebview2Setup.exe (temporary; runs only when the WebView2 Runtime is missing)'

if ($dry) {
    exit 0
}

# --- real run ----------------------------------------------------------------------------------------------------

if ($null -eq $makensis) {
    throw "makensis was not found. $nsisHint"
}
if (-not $nsisVersion.Parsed) {
    throw "Cannot read the NSIS version of '$makensis': $($nsisVersion.Text). NSIS 3.x is needed. $nsisHint"
}
if ($nsisVersion.Major -lt 3) {
    throw "makensis $($nsisVersion.Text) is too old: NSIS 3.x is needed. $nsisHint"
}
if ($null -eq $goExe) {
    throw "'go' was not found on PATH. Install Go (see go.work for the version) and open a new terminal. This script installs nothing."
}
if ($null -eq $bootstrapper) {
    throw "MicrosoftEdgeWebview2Setup.exe was not found in the Wails module ($wailsModule). Run 'go -C paratech mod download' and try again."
}
if (-not $bootstrapperSigned) {
    throw "'$bootstrapper' has no valid Microsoft Authenticode signature; refusing to ship it. Run 'go clean -modcache' and download the modules again."
}

if ($SkipAppBuild) {
    if (-not (Test-Path -LiteralPath (Join-Path $appDir $exeName) -PathType Leaf)) {
        throw "-SkipAppBuild: '$appDir\$exeName' does not exist; run once without -SkipAppBuild."
    }
    Write-Step "Reusing $appDir\$exeName"
} else {
    if (Test-Path -LiteralPath $appDir) {
        Write-Step "Removing the old staging folder $appDir"
        Remove-Item -LiteralPath $appDir -Recurse -Force
    }
    Write-Step 'Building the app with build-alpha.ps1'
    $saved = Set-Env $buildEnv
    try {
        & $alphaScript -OutDir $appDir
    } finally {
        Restore-Env $saved
    }
}

# Both paths: -SkipAppBuild may find an exe that was built (or replaced) some other way.
Write-Step "Checking the build settings of $appDir\$exeName"
$exeProblems = @(Get-ReleaseExeProblems $goExe (Join-Path $appDir $exeName))
if ($exeProblems.Count -gt 0) {
    throw "'$appDir\$exeName' is not a release build: $($exeProblems -join '; '). Run without -SkipAppBuild to build it again."
}

if ($whisperFiles.Count -gt 0) {
    Write-Step "Removing stale whisper files from $appDir"
    Remove-StagedWhisper $appDir
    Write-Step "Copying whisper files from $WhisperDir"
    foreach ($file in $whisperFiles) {
        Copy-Item -LiteralPath $file -Destination $appDir -Force
        $hash = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash
        Write-Host "    $(Split-Path -Leaf $file)  SHA-256 $hash"
    }
}
$withWhisper = Test-Path -LiteralPath (Join-Path $appDir 'whisper-cli.exe') -PathType Leaf
if ($withWhisper) {
    # A staged whisper-cli.exe must come with all its DLLs (the NSIS script lists them).
    $null = Get-WhisperFiles $appDir
    $nsisArgs += '-DWITH_WHISPER'
} else {
    Write-Host '    the installer has no whisper-cli: copy it with its DLLs into the install folder (docs/refs/packaging.md)'
}

Write-Step "makensis: $installer"
$null = New-Item -ItemType Directory -Force -Path $releaseRoot
Invoke-Native $makensis ($nsisArgs + @($nsiFile))
if (-not (Test-Path -LiteralPath $installer -PathType Leaf)) {
    throw "makensis finished but '$installer' does not exist."
}
$size = [math]::Round((Get-Item -LiteralPath $installer).Length / 1MB, 1)
$hash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash
Write-Step "Built $installer ($size MB, SHA-256 $hash)"
