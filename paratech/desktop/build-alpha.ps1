<#
.SYNOPSIS
Builds the alpha of the Learning Assistant desktop app: the React UI, then the Windows binary with the UI embedded.

.DESCRIPTION
Runs `npm ci` and `npm run build` in desktop/frontend, then `go build -tags production -trimpath` of
./desktop/cmd/desktop with CGO_ENABLED=0 for windows/amd64. The exe goes to paratech\dist\alpha (ignored by git)
unless -OutDir says otherwise. Needs Go and Node.js/npm on PATH; installs nothing and needs no admin rights.
Environment variables are set only for the build and restored afterwards.

First it puts the speech recognition component (whisper-cli.exe and its DLLs) into the output folder. The pinned set
is desktop\internal\app\speechcomponent.json, the manifest the app embeds. The official whisper.cpp archive is
downloaded into paratech\dist\cache\speech-component\<version> (ignored by git; a cached archive is reused only if its
size and SHA-256 match) and checked BEFORE it is opened; only the manifest files are unpacked, each checked again.
Nothing from the archive is run. Whisper files already in the output folder are removed first. Offline: -WhisperZip
(a copy of the archive) or -WhisperDir (its unpacked Release folder) pass the same checks.

-Smoke then checks the component next to the exe, starts the exe headless with a throwaway data directory inside the
output folder, waits for the start log lines (the speech engine among them) and stops the process again (a hard stop:
see docs/qa/run-alpha.md).

.EXAMPLE
powershell -NoProfile -ExecutionPolicy Bypass -File .\paratech\desktop\build-alpha.ps1 -Smoke
#>
[CmdletBinding()]
param(
    # Output folder of the exe; relative paths are taken from the current directory.
    [string]$OutDir = '',
    # Start the built exe headless and check that it starts.
    [switch]$Smoke,
    # Local copy of the pinned whisper.cpp archive instead of the download (same checks).
    [string]$WhisperZip = '',
    # Unpacked Release folder of the pinned archive instead of the download (every file is checked).
    [string]$WhisperDir = '',
    # Dev build without the speech component (the app then offers to install it).
    [switch]$SkipWhisper,
    # Only put the speech component into the output folder; no frontend or Go build (build-release.ps1 uses it).
    [switch]$ComponentOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$desktopDir = $PSScriptRoot
$moduleRoot = Split-Path -Parent $desktopDir
$frontendDir = Join-Path $desktopDir 'frontend'
$exeName = 'learning-assistant.exe'
$manifestPath = Join-Path $desktopDir 'internal\app\speechcomponent.json'
$cacheRoot = Join-Path $moduleRoot 'dist\cache\speech-component'
# Removed from the output folder before the component is copied: also any ggml-cpu-*.dll of an older set.
$whisperStalePatterns = @('whisper-cli.exe', 'whisper.dll', 'ggml.dll', 'ggml-base.dll', 'ggml-cpu-*.dll')

# Log lines that prove a successful start (internal/app; the speech lines are in speech.go). smoke-data is not the
# default data dir, so it must have its own Credential Manager profile and never reach the real secrets (ADR-0014).
$smokeLines = @('instance lock acquired', 'secrets_profile=isolated', 'background work started')
$speechReadyLine = 'speech recognition engine ready'
$speechMissingLine = 'speech recognition is not available'
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

function Resolve-FullPath([string]$Path) {
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        $Path = Join-Path (Get-Location).ProviderPath $Path
    }
    return [System.IO.Path]::GetFullPath($Path)
}

# --- speech recognition component --------------------------------------------------------------------------------

# Reads the pinned manifest and rejects values that could make a path or a URL other than intended.
function Read-SpeechManifest {
    $m = [System.IO.File]::ReadAllText($manifestPath) | ConvertFrom-Json
    $bad = "Speech component manifest '$manifestPath'"
    if ($m.schema_version -ne 1) {
        throw "$bad has schema_version $($m.schema_version); this script knows 1."
    }
    $uri = $null
    if (-not [System.Uri]::TryCreate([string]$m.source_url, [System.UriKind]::Absolute, [ref]$uri) -or
        $uri.Scheme -ne 'https' -or @($m.download_hosts) -notcontains $uri.Host) {
        throw "$bad has source_url '$($m.source_url)' that is not https on one of its download_hosts."
    }
    if ([string]$m.version -notmatch '^[A-Za-z0-9._-]+$' -or [string]$m.sha256 -notmatch '^[0-9a-f]{64}$' -or
        [long]$m.size_bytes -le 0 -or [string]::IsNullOrEmpty([string]$m.entry_prefix)) {
        throw "$bad has an invalid version, sha256, size_bytes or entry_prefix."
    }
    $names = @{}
    foreach ($f in @($m.files)) {
        # Plain file names only: they become paths in the output folder.
        if ([string]$f.name -notmatch '^[A-Za-z0-9_-][A-Za-z0-9._-]*$' -or [string]$f.sha256 -notmatch '^[0-9a-f]{64}$' -or
            [long]$f.size_bytes -le 0 -or $names.ContainsKey(([string]$f.name).ToLowerInvariant())) {
            throw "$bad has an invalid or duplicate file entry '$($f.name)'."
        }
        $names[([string]$f.name).ToLowerInvariant()] = $true
    }
    if (-not $names.ContainsKey(([string]$m.executable).ToLowerInvariant())) {
        throw "$bad does not list its executable '$($m.executable)' in files."
    }
    return $m
}

# Returns $null when the file is a regular file with the size and SHA-256, else the problem.
function Test-PinnedFile([string]$Path, [long]$Size, [string]$Sha256) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return 'missing'
    }
    $item = Get-Item -LiteralPath $Path -Force
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        return 'is a link, not a regular file'
    }
    if ($item.Length -ne $Size) {
        return "size $($item.Length), expected $Size"
    }
    $hash = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($hash -cne $Sha256) {
        return "SHA-256 $hash, expected $Sha256"
    }
    return $null
}

# Returns the problems of the component files in a folder; empty when all of them verify.
function Get-SpeechComponentProblems([object]$Manifest, [string]$Dir) {
    $problems = @()
    foreach ($f in @($Manifest.files)) {
        $problem = Test-PinnedFile (Join-Path $Dir $f.name) ([long]$f.size_bytes) ([string]$f.sha256)
        if ($null -ne $problem) {
            $problems += "$($f.name): $problem"
        }
    }
    return $problems
}

# Returns the path of the verified archive: -WhisperZip, the cached download, or a fresh download.
function Get-SpeechArchive([object]$Manifest, [string]$LocalZip) {
    $size = [long]$Manifest.size_bytes
    $sha = [string]$Manifest.sha256
    if ($LocalZip -ne '') {
        $problem = Test-PinnedFile $LocalZip $size $sha
        if ($null -ne $problem) {
            throw "-WhisperZip '$LocalZip' is not the pinned archive $($Manifest.source_url): $problem."
        }
        Write-Host "    using $LocalZip (size and SHA-256 match)"
        return $LocalZip
    }

    $dir = Join-Path $cacheRoot $Manifest.version
    $leaf = (New-Object System.Uri ([string]$Manifest.source_url)).Segments[-1]
    $zip = Join-Path $dir $leaf
    if (Test-Path -LiteralPath $zip) {
        $problem = Test-PinnedFile $zip $size $sha
        if ($null -eq $problem) {
            Write-Host "    using the cached $zip (size and SHA-256 match)"
            return $zip
        }
        Write-Host "    removing the cached $zip ($problem)"
        Remove-Item -LiteralPath $zip -Force
    }

    $null = New-Item -ItemType Directory -Force -Path $dir
    $part = "$zip.part"
    if (Test-Path -LiteralPath $part) {
        Remove-Item -LiteralPath $part -Force
    }
    Write-Host "    downloading $($Manifest.source_url)"
    $savedProtocol = [System.Net.ServicePointManager]::SecurityProtocol
    # The progress bar makes Invoke-WebRequest of Windows PowerShell 5.1 many times slower.
    $savedProgress = $ProgressPreference
    try {
        # Windows PowerShell 5.1 may still default to TLS 1.0; add 1.2 (and 1.3 where .NET knows it).
        $protocol = $savedProtocol -bor [System.Net.SecurityProtocolType]::Tls12
        if ([enum]::GetNames([System.Net.SecurityProtocolType]) -contains 'Tls13') {
            $protocol = $protocol -bor [System.Net.SecurityProtocolType]::Tls13
        }
        [System.Net.ServicePointManager]::SecurityProtocol = $protocol
        $ProgressPreference = 'SilentlyContinue'
        Invoke-WebRequest -Uri $Manifest.source_url -OutFile $part -UseBasicParsing
    } finally {
        $ProgressPreference = $savedProgress
        [System.Net.ServicePointManager]::SecurityProtocol = $savedProtocol
    }
    $problem = Test-PinnedFile $part $size $sha
    if ($null -ne $problem) {
        Remove-Item -LiteralPath $part -Force
        throw "The downloaded $($Manifest.source_url) is not the pinned archive ($problem); it was deleted. Try again or use -WhisperZip."
    }
    Move-Item -LiteralPath $part -Destination $zip -Force
    Write-Host "    saved $zip (size and SHA-256 match)"
    return $zip
}

# Copies at most $Limit bytes; more means the entry is not what the manifest says.
function Copy-Bounded([System.IO.Stream]$In, [string]$Target, [long]$Limit) {
    $out = [System.IO.File]::Open($Target, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
    try {
        $buffer = New-Object byte[] 81920
        $total = [long]0
        while (($n = $In.Read($buffer, 0, $buffer.Length)) -gt 0) {
            $total += $n
            if ($total -gt $Limit) {
                throw "more than $Limit bytes"
            }
            $out.Write($buffer, 0, $n)
        }
    } finally {
        $out.Dispose()
    }
}

# Unpacks only the manifest files of a verified archive into an empty folder. The target path is built from the
# manifest name, never from the entry name; an entry must match '<entry_prefix><name>' exactly and only once.
function Expand-SpeechArchive([object]$Manifest, [string]$Zip, [string]$Dest) {
    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $archive = [System.IO.Compression.ZipFile]::OpenRead($Zip)
    try {
        foreach ($f in @($Manifest.files)) {
            $want = [string]$Manifest.entry_prefix + [string]$f.name
            $entries = @($archive.Entries | Where-Object { $_.FullName -ceq $want })
            if ($entries.Count -ne 1) {
                throw "The archive has $($entries.Count) entries named '$want', expected exactly one."
            }
            $size = [long]$f.size_bytes
            if ($entries[0].Length -ne $size) {
                throw "Archive entry '$want' has $($entries[0].Length) bytes, expected $size."
            }
            $target = Join-Path $Dest $f.name
            $in = $entries[0].Open()
            try {
                Copy-Bounded $in $target $size
            } catch {
                throw "Unpacking '$want' failed: $($_.Exception.Message)"
            } finally {
                $in.Dispose()
            }
            $problem = Test-PinnedFile $target $size ([string]$f.sha256)
            if ($null -ne $problem) {
                throw "Unpacked '$want' does not match the manifest: $problem."
            }
        }
    } finally {
        $archive.Dispose()
    }
}

# Removes whisper files left in a folder by an earlier run, so stale libraries never sit next to the new set.
function Remove-StagedWhisper([string]$Dir) {
    if (-not (Test-Path -LiteralPath $Dir -PathType Container)) {
        return
    }
    foreach ($pattern in $whisperStalePatterns) {
        Get-ChildItem -LiteralPath $Dir -Filter $pattern -File -Force | ForEach-Object {
            Write-Host "    removing the old $($_.Name)"
            Remove-Item -LiteralPath $_.FullName -Force
        }
    }
}

# Puts the verified component into $Dir: from -WhisperDir, or unpacked from the pinned archive.
function Install-SpeechComponent([object]$Manifest, [string]$Dir, [string]$FromDir, [string]$FromZip) {
    $work = $null
    try {
        if ($FromDir -ne '') {
            $problems = @(Get-SpeechComponentProblems $Manifest $FromDir)
            if ($problems.Count -gt 0) {
                throw "-WhisperDir '$FromDir' does not hold the pinned whisper.cpp $($Manifest.version) files:`n  $($problems -join "`n  ")"
            }
            Write-Host "    using $FromDir (all files match)"
            $source = $FromDir
        } else {
            $zip = Get-SpeechArchive $Manifest $FromZip
            $work = Join-Path $cacheRoot ('unpack-' + [guid]::NewGuid().ToString('N'))
            $null = New-Item -ItemType Directory -Path $work
            Expand-SpeechArchive $Manifest $zip $work
            Write-Host "    unpacked $(@($Manifest.files).Count) files (sizes and SHA-256 match)"
            $source = $work
        }
        $null = New-Item -ItemType Directory -Force -Path $Dir
        Remove-StagedWhisper $Dir
        foreach ($f in @($Manifest.files)) {
            Copy-Item -LiteralPath (Join-Path $source $f.name) -Destination (Join-Path $Dir $f.name) -Force
        }
    } finally {
        if ($null -ne $work -and (Test-Path -LiteralPath $work)) {
            Remove-Item -LiteralPath $work -Recurse -Force
        }
    }
    $problems = @(Get-SpeechComponentProblems $Manifest $Dir)
    if ($problems.Count -gt 0) {
        throw "The speech component in '$Dir' does not verify after the copy:`n  $($problems -join "`n  ")"
    }
}

# --- smoke -------------------------------------------------------------------------------------------------------

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
    if (-not $SkipWhisper -and $text.Contains($speechMissingLine)) {
        throw "Smoke failed: the log says '$speechMissingLine' although the component is next to the exe."
    }
    Write-Step 'Smoke passed: the app started (stopped hard afterwards).'
}

# --- arguments ---------------------------------------------------------------------------------------------------

if ($SkipWhisper -and ($ComponentOnly -or $WhisperZip -ne '' -or $WhisperDir -ne '')) {
    throw '-SkipWhisper cannot be combined with -ComponentOnly, -WhisperZip or -WhisperDir.'
}
if ($WhisperZip -ne '' -and $WhisperDir -ne '') {
    throw 'Pass either -WhisperZip or -WhisperDir, not both.'
}
if ($ComponentOnly -and $Smoke) {
    throw '-Smoke needs a build; it cannot be combined with -ComponentOnly.'
}

if ($OutDir -eq '') {
    $OutDir = Join-Path $moduleRoot 'dist\alpha'
}
$OutDir = Resolve-FullPath $OutDir
$exePath = Join-Path $OutDir $exeName
if ($WhisperZip -ne '') {
    $WhisperZip = Resolve-FullPath $WhisperZip
}
if ($WhisperDir -ne '') {
    $WhisperDir = Resolve-FullPath $WhisperDir
    # The old whisper files of the output folder are removed before the copy.
    if ($WhisperDir.TrimEnd('\') -ieq $OutDir.TrimEnd('\')) {
        throw "-WhisperDir must not be the output folder '$OutDir'; pass the Release folder of the pinned archive."
    }
}
# Without the component the warning of the missing component is expected instead.
if (-not $SkipWhisper) {
    $smokeLines += $speechReadyLine
}

# Before the long build, so a failed download or a wrong file stops the script early.
if (-not $SkipWhisper) {
    $manifest = Read-SpeechManifest
    Write-Step "Speech component: whisper.cpp $($manifest.version) into $OutDir"
    Install-SpeechComponent $manifest $OutDir $WhisperDir $WhisperZip
}
if ($ComponentOnly) {
    exit 0
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
    if (-not $SkipWhisper) {
        $problems = @(Get-SpeechComponentProblems $manifest $OutDir)
        if ($problems.Count -gt 0) {
            throw "Smoke failed: the speech component next to the exe does not verify:`n  $($problems -join "`n  ")"
        }
        Write-Step "Smoke: the $(@($manifest.files).Count) speech component files next to the exe match the manifest"
    }
    Invoke-Smoke $exePath $OutDir
}
