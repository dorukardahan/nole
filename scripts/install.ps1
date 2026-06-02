#Requires -Version 5.1
<#
  Nólë installer (Windows / PowerShell): downloads a prebuilt release binary,
  verifies its SHA256 checksum, and installs it to a per-user directory. It is
  read-only toward your environment: it touches NO secrets, sends NO telemetry,
  and only writes the single binary (plus a user-PATH entry).

  Recommended (supply-chain-cautious) usage — download, inspect, then run:

    irm https://raw.githubusercontent.com/dorukardahan/nole/main/scripts/install.ps1 -OutFile install.ps1
    notepad install.ps1     # read it first
    powershell -ExecutionPolicy Bypass -File .\install.ps1

  Pipe-to-iex also works if you trust the source (this bypasses execution policy
  because nothing is written to disk as a .ps1 file):

    irm https://raw.githubusercontent.com/dorukardahan/nole/main/scripts/install.ps1 | iex

  INTEGRITY MODEL (two layers, only the first is mandatory) — identical to
  scripts/install.sh:
    1. SHA256 is the MANDATORY integrity floor. The installer downloads
       SHA256SUMS, verifies the asset against it with Get-FileHash, and FAILS
       CLOSED on any mismatch. Get-FileHash ships in PowerShell 5.1+, so the
       zero-dependency path always works with no extra tooling.
    2. GitHub build-provenance attestation (Sigstore-backed, via
       `gh attestation verify`) is an ADDITIVE, best-effort second gate. It runs
       only when a usable `gh` is present, FAILS CLOSED on a real verification
       mismatch, and is SKIPPED with a clear log line otherwise (no gh, old gh,
       offline, anonymous, or a pre-signing release). See NOLE_INSTALL_VERIFY.

  Overrides (all optional; same names + semantics as install.sh):
    NOLE_INSTALL_VERSION       pin a release tag (e.g. v0.10.0); default: latest
    NOLE_INSTALL_DIR           install directory; default: %LOCALAPPDATA%\Programs\nole
    NOLE_INSTALL_REPO          owner/repo; default: dorukardahan/nole
    NOLE_INSTALL_API_URL       releases API base; default: https://api.github.com
    NOLE_INSTALL_DOWNLOAD_URL  asset download base; default: https://github.com
    NOLE_INSTALL_VERIFY        auto|require|off  (default: auto)
        auto     verify the attestation when a usable gh is present; soft-skip
                 (install on SHA256 alone) when it is not — the zero-dep default.
        require  treat an unusable/absent verifier, or an unverifiable
                 attestation, as a hard error. For supply-chain-strict installs.
        off      skip the attestation gate entirely (SHA256 still mandatory).
#>

[CmdletBinding()]
param()

# --- set -euo pipefail equivalent ---
Set-StrictMode -Version Latest          # ~ -u  (unset var / property access = error)
$ErrorActionPreference = 'Stop'          # ~ -e  (cmdlet errors escalate so try/catch / fail-closed works)

# NATIVE commands (gh) must NOT auto-throw on a nonzero exit: the attestation
# three-way taxonomy deliberately reads $LASTEXITCODE, and a thrown error would
# skip the soft-skip classification and break the zero-dependency floor.
# $ErrorActionPreference='Stop' above still governs CMDLETS (Invoke-WebRequest /
# Get-FileHash) so they fail-closed. $PSNativeCommandUseErrorActionPreference
# defaults to $true on PS 7.4+, so disable it explicitly where the var exists.
if (Get-Variable -Name PSNativeCommandUseErrorActionPreference -ErrorAction SilentlyContinue) {
    $PSNativeCommandUseErrorActionPreference = $false
}

# TLS 1.2 floor for PS 5.1 (.NET defaults to TLS 1.0 on older Windows; GitHub serves
# TLS 1.2+ only, so a missing floor surfaces as a misleading "connection" error).
# Additive -bor keeps TLS 1.3 where the OS offers it. No-op on PS 7+.
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12 } catch {}

# --- config (env overrides mirror install.sh; TrimEnd('/') mirrors ${VAR%/}) ---
$Repo        = if ($env:NOLE_INSTALL_REPO)         { $env:NOLE_INSTALL_REPO }         else { 'dorukardahan/nole' }
$ApiUrl      = if ($env:NOLE_INSTALL_API_URL)      { $env:NOLE_INSTALL_API_URL }      else { 'https://api.github.com' }
$ApiUrl      = $ApiUrl.TrimEnd('/')
$DownloadUrl = if ($env:NOLE_INSTALL_DOWNLOAD_URL) { $env:NOLE_INSTALL_DOWNLOAD_URL } else { 'https://github.com' }
$DownloadUrl = $DownloadUrl.TrimEnd('/')
$InstallDir  = if ($env:NOLE_INSTALL_DIR)          { $env:NOLE_INSTALL_DIR }          else { Join-Path $env:LOCALAPPDATA 'Programs\nole' }
$VerifyMode  = if ($env:NOLE_INSTALL_VERIFY)       { $env:NOLE_INSTALL_VERIFY }       else { 'auto' }

# The first Nólë release whose assets carry a GitHub build-provenance attestation.
# install.ps1 is fetched fresh from main, so this constant is current. For a resolved
# version >= SIGNED_SINCE a MISSING attestation while the API is reachable is treated
# as tampering and FAILS CLOSED; older (pre-signing) versions, an unreachable API, or
# an absent verifier soft-skip.
$SignedSince = [version]'0.10.0'

# Minimum gh free of CVE-2026-48501 (gh attestation leaked the host auth token to
# Sigstore TUF mirrors before 2.93.0). Older gh is treated as "no verifier" so the
# installer never invokes a token-leaking binary.
$GhMin       = [version]'2.93.0'

$UserAgent   = @{ 'User-Agent' = 'nole-install' }   # GitHub API rejects requests with no UA

# --- logging (mirror install.sh: stdout "nole-install: ", stderr "nole-install: error: ") ---
function Write-Log { param([string]$Message) Write-Host "nole-install: $Message" }
function Stop-WithError {
    param([string]$Message)
    [Console]::Error.WriteLine("nole-install: error: $Message")
    exit 1
}

# --- version helpers (identical semantics to install.sh ver_ge / is_clean_release / looks_like_release_tag) ---

# Strip a leading 'v', any +build, any -prerelease, returning the bare core string.
function Get-VersionCore {
    param([string]$V)
    $core = $V
    # Strip a LOWERCASE 'v' only — bash `${core#v}` does not strip 'V', so "V0.10.0"
    # is treated as a non-release ref (soft-skip), and PS must match that.
    if ($core.StartsWith('v')) { $core = $core.Substring(1) }
    $core = ($core -split '\+', 2)[0]   # drop +build
    $core = ($core -split '-', 2)[0]    # drop -prerelease
    return $core
}

# Test-VersionGE A B -> $true if version A >= B. Both MAJOR.MINOR.PATCH; v-prefix / -pre
# / +build stripped. Any empty or non-numeric segment -> $false (so dev builds / garbage
# never trip a fail-closed path), mirroring install.sh ver_ge and internal/selfupdate.
function Test-VersionGE {
    param([string]$A, [string]$B)
    $ac = (Get-VersionCore $A) -split '\.'
    $bc = (Get-VersionCore $B) -split '\.'
    foreach ($seg in @($ac[0], $ac[1], $ac[2], $bc[0], $bc[1], $bc[2])) {
        if ([string]::IsNullOrEmpty($seg) -or ($seg -notmatch '^\d+$')) { return $false }
    }
    [int]$a1 = $ac[0]; [int]$a2 = $ac[1]; [int]$a3 = $ac[2]
    [int]$b1 = $bc[0]; [int]$b2 = $bc[1]; [int]$b3 = $bc[2]
    if ($a1 -ne $b1) { return ($a1 -gt $b1) }
    if ($a2 -ne $b2) { return ($a2 -gt $b2) }
    return ($a3 -ge $b3)
}

# Test-IsCleanRelease -> $true if it strips to EXACTLY three numeric segments.
function Test-IsCleanRelease {
    param([string]$V)
    $core = Get-VersionCore $V
    if ($core -notmatch '^[0-9.]+$') { return $false }
    $parts = $core -split '\.'
    if ($parts.Count -ne 3) { return $false }
    foreach ($p in $parts) { if ([string]::IsNullOrEmpty($p)) { return $false } }
    return $true
}

# Test-LooksLikeReleaseTag -> $true if SHAPED like a release tag (optional lowercase
# v then a digit). -cmatch (case-SENSITIVE) mirrors bash `case $v in v[0-9]*|[0-9]*)`:
# "V0.10" is NOT a release tag in bash, so PS must not treat it as one either.
function Test-LooksLikeReleaseTag {
    param([string]$V)
    return ($V -cmatch '^v?[0-9]')
}

# Test-VersionIsSigned -> $true if the resolved release is at/after the signing cutover.
function Test-VersionIsSigned {
    param([string]$V)
    return (Test-VersionGE -A $V -B $SignedSince.ToString())
}

# Invoke-Gh runs gh capturing combined stdout+stderr and the exit code, with
# $ErrorActionPreference relaxed for the call. On PowerShell < 7.2 (incl. stock
# Windows PowerShell 5.1 — the advertised `powershell -File` path) a redirected
# native stderr (2>&1) is surfaced as a NativeCommandError that OBEYS the
# script-wide 'Stop', so a normal soft-skippable gh failure that writes stderr
# (offline/anonymous verify, old gh) would THROW before $LASTEXITCODE is read,
# aborting the install instead of falling back to SHA256-only. Relaxing the
# preference around the native call (we read the exit code explicitly) preserves
# the intended soft-skip / fail-closed taxonomy. Returns @{ Output; Code }.
function Invoke-Gh {
    param([string[]]$GhArgs)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = (& gh @GhArgs 2>&1 | Out-String)
        return [pscustomobject]@{ Output = $out; Code = $LASTEXITCODE }
    } finally {
        $ErrorActionPreference = $prev
    }
}

# Test-GhVersionOk -> $true if the installed gh is >= GH_MIN_VERSION (CVE-2026-48501 fix).
function Test-GhVersionOk {
    # gh --version prints 2+ lines ("gh version 2.93.0 (2026-04-01)\n..."); take line 0.
    $r = Invoke-Gh @('--version')
    if ($r.Code -ne 0 -or -not $r.Output) { return $false }
    $first = ($r.Output -split "`r?`n")[0]
    if ($first -notmatch 'gh version (\d+\.\d+\.\d+)') { return $false }
    return ([version]$matches[1] -ge $GhMin)
}

# Get-GhHostFromApi -> the gh --hostname for a GHE/mirror API base, '' for the public
# default (api.github.com / github.com). Keeps `gh attestation verify` pointed at the
# same GitHub host the install was configured against. Mirrors install.sh gh_host_from_api.
function Get-GhHostFromApi {
    if ($ApiUrl -match '^[a-zA-Z][a-zA-Z0-9+.-]*://([^/]+)') {
        $h = $matches[1]
        if ($h -eq 'api.github.com' -or $h -eq 'github.com') { return '' }
        return $h
    }
    return ''
}

# --- optional, additive attestation verification (GitHub build provenance) ---
# PRECONDITION: SHA256 has ALREADY passed. Three-way fail taxonomy, identical to install.sh:
#   - verifier unusable (gh absent / too old / pre-CVE-fix) or VERIFY_MODE=off -> soft skip
#   - attestation INVALID, or provably absent on a KNOWN-signed version (API reachable) -> fail closed
#   - API unreachable / anonymous / pre-signing release -> soft skip
# In `require` mode every soft-skip becomes a hard error.
function Invoke-AttestVerify {
    param([string]$File, [string]$Asset, [string]$Version)

    if ($VerifyMode -ceq 'off') {
        Write-Log "attestation check disabled (NOLE_INSTALL_VERIFY=off) — SHA256 already verified"
        return
    }

    # Capability probe: need gh, the `attestation verify` subcommand, and a gh new
    # enough to be free of CVE-2026-48501.
    $reason = $null
    if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
        $reason = 'gh not installed'
    } else {
        $probe = Invoke-Gh @('attestation', 'verify', '--help')
        if ($probe.Code -ne 0) {
            $reason = "installed gh lacks 'attestation verify'"
        } elseif (-not (Test-GhVersionOk)) {
            $reason = "gh < $($GhMin) (CVE-2026-48501)"
        }
    }
    if ($reason) {
        if ($VerifyMode -ceq 'require') {
            Stop-WithError "NOLE_INSTALL_VERIFY=require but no usable attestation verifier: $reason. Install GitHub CLI gh >= $($GhMin), or unset NOLE_INSTALL_VERIFY."
        }
        Write-Log "signature verifier unavailable ($reason) — skipping attestation check (SHA256 already verified)"
        return
    }

    # gh is present and adequate. Verify the binary against the repo's build-provenance
    # attestation, hardened to the exact release-workflow signer identity. install.ps1
    # passes NO token of its own; gh uses whatever host auth exists. An anonymous host
    # lands in the 'unreachable' soft-skip branch below — NOT a failure. A fork install
    # (NOLE_INSTALL_REPO) requires that fork to carry its own attestation signed by its
    # own release.yml, else this fails closed (the secure outcome).
    #
    # gh is a NATIVE exe: with $PSNativeCommandUseErrorActionPreference disabled above, a
    # nonzero exit does NOT throw — read $LASTEXITCODE on the very NEXT line (it holds
    # only the LAST native exe's code).
    $ghArgs = @('attestation', 'verify', $File, '--repo', $Repo, '--signer-workflow', "$Repo/.github/workflows/release.yml")
    $ghHost = Get-GhHostFromApi
    if ($ghHost) { $ghArgs += @('--hostname', $ghHost) }
    # Capture output once for INTERNAL classification only; it is NEVER surfaced (it can
    # carry private URLs / auth detail — AGENTS.md: never print private URLs).
    $r   = Invoke-Gh $ghArgs
    $out = $r.Output
    $rc  = $r.Code
    if ($rc -eq 0) {
        Write-Log "attestation verified (build provenance, $Repo)"
        return
    }

    # rc != 0. The SIGNED_SINCE version floor — not fragile message parsing — decides the
    # security-relevant case. Only the unreachable/auth set is substring-matched, kept
    # CONSERVATIVE so a MISSED pattern falls through to the (safe) fail-closed-on-signed
    # arm rather than soft-skipping a genuine verification failure.
    $unreachable = @(
        'HTTP 401', 'HTTP 403', 'authentication', 'Unauthorized', 'log in', 'gh auth',
        'rate limit', 'connection refused', 'no such host', 'i/o timeout', 'deadline exceeded',
        'dial tcp', 'lookup ', 'network is unreachable', 'no route to host', 'TLS handshake', 'server misbehaving'
    )
    foreach ($needle in $unreachable) {
        if ($out -clike "*$needle*") {
            if ($VerifyMode -ceq 'require') {
                Stop-WithError "NOLE_INSTALL_VERIFY=require: could not reach or authenticate to the attestation API to verify $Asset (run 'gh attestation verify' manually for details)"
            }
            Write-Log "attestation API unreachable/unauthenticated — skipping attestation check (SHA256 already verified)"
            return
        }
    }

    # Reachable, but the artifact did NOT verify. SIGNED_SINCE floor decides.
    if (Test-IsCleanRelease $Version) {
        if (Test-VersionIsSigned $Version) {
            Stop-WithError "attestation verification FAILED for $Asset ($Version) — refusing to install (possible tampering; set NOLE_INSTALL_VERIFY=off to override, or run 'gh attestation verify' manually for the reason)"
        }
        # A well-formed release BELOW the cutover -> genuinely pre-signing -> soft-skip below.
    } elseif (Test-LooksLikeReleaseTag $Version) {
        # Version-SHAPED but MALFORMED (e.g. an attacker-served "v0.10" on the unpinned
        # latest path). We cannot confirm it predates signing, so bias to fail-closed.
        Stop-WithError "attestation verification FAILED for ${Asset}: malformed release tag '$Version' could not be confirmed pre-signing — refusing to install (set NOLE_INSTALL_VERIFY=off to override)"
    }
    # Pre-signing clean release, or a non-release ref (dev/branch) -> soft-skip.
    if ($VerifyMode -ceq 'require') {
        Stop-WithError "NOLE_INSTALL_VERIFY=require but $Asset ($Version) has no verifiable attestation (predates signing or is not a release tag)"
    }
    Write-Log "no verifiable attestation for $Version (pre-signing release) — skipping attestation check (SHA256 already verified)"
}

# --- arch detection -> release asset name ---
# OSArchitecture (not $env:PROCESSOR_ARCHITECTURE, which lies under x64 emulation on
# Windows-on-ARM, and not ProcessArchitecture): we install the binary matching the
# OS/hardware, so an x64-emulated shell on an ARM64 box still gets the arm64 build.
function Get-Arch {
    # Prefer RuntimeInformation.OSArchitecture — it reports the OS/hardware arch
    # correctly even under x64 emulation on Windows-on-ARM. But on stock Windows
    # PowerShell 5.1 that type can be unavailable or shadowed (leaving it blank), so
    # fall back to the environment arch vars: PROCESSOR_ARCHITEW6432 holds the REAL
    # arch when a 32-bit shell runs under WOW64, else PROCESSOR_ARCHITECTURE.
    $arch = $null
    try {
        $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    } catch {
        $arch = $null
    }
    if ([string]::IsNullOrWhiteSpace($arch)) {
        $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    switch -Regex ($arch) {
        '^(x64|amd64)$' { return 'amd64' }   # switch -Regex is case-insensitive
        '^arm64$'       { return 'arm64' }
        default {
            Stop-WithError "unsupported architecture '$arch'. Download a binary manually from https://github.com/$Repo/releases."
        }
    }
}

function Resolve-Version {
    if ($env:NOLE_INSTALL_VERSION) { return $env:NOLE_INSTALL_VERSION }
    try {
        $rel = Invoke-RestMethod -Uri "$ApiUrl/repos/$Repo/releases/latest" -Headers $UserAgent
    } catch {
        Stop-WithError "could not query the latest release (are you online? rate-limited?)"
    }
    # Read tag_name defensively: under Set-StrictMode -Version Latest, accessing a
    # NON-EXISTENT property (a malformed/hostile API base returning e.g.
    # {"message":"Not Found"}) throws a raw exception OUTSIDE the try/catch above,
    # which would bypass this clean diagnostic and abort with a .NET stack trace.
    # Guarding the read restores parity with install.sh's clean "could not parse" die.
    $tag = $null
    if ($rel -and ($rel.PSObject.Properties.Name -contains 'tag_name')) { $tag = $rel.tag_name }
    if (-not $tag) { Stop-WithError "could not parse a release tag from the API response" }
    return $tag
}

# --- main ---
function Invoke-Main {
    # Reject an unknown NOLE_INSTALL_VERIFY early. Without this a typo like 'required'
    # or 'REQUIRE' would fall through to `auto` semantics and silently weaken the very
    # policy the user was trying to strengthen — fail loud instead.
    if ($VerifyMode -cnotin @('auto', 'require', 'off')) {
        Stop-WithError "invalid NOLE_INSTALL_VERIFY='$VerifyMode' (expected one of: auto, require, off)"
    }

    $arch    = Get-Arch
    $asset   = "nole-windows-$arch.exe"     # the one shape diff from bash's nole-<os>-<arch>
    $version = Resolve-Version
    $base    = "$DownloadUrl/$Repo/releases/download/$version"

    Write-Log "installing $asset $version"

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("nole-install-" + [guid]::NewGuid().ToString())
    $staged = $null
    try {
        New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null
        $assetPath = Join-Path $tmpDir $asset
        $sumsPath  = Join-Path $tmpDir 'SHA256SUMS'

        # ProgressPreference=SilentlyContinue makes Invoke-WebRequest up to ~10x faster
        # on PS 5.1. -UseBasicParsing is mandatory on 5.1 (no IE DOM engine), no-op on 6+.
        $oldProgress = $ProgressPreference
        $ProgressPreference = 'SilentlyContinue'
        try {
            try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile $assetPath }
            catch { Stop-WithError "could not download $asset for $version" }
            try { Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS" -OutFile $sumsPath }
            catch { Stop-WithError "could not download SHA256SUMS for $version" }
        } finally {
            $ProgressPreference = $oldProgress
        }

        # --- SHA256: MANDATORY, FAIL-CLOSED, before any write to the install dir ---
        # SHA256SUMS uses relative filenames with a TWO-SPACE separator
        # ("<hash>  nole-windows-<arch>.exe"). Anchor to exactly our asset's line so an
        # unrelated/extra asset line can never satisfy the check (mirrors bash's
        # `grep -E "  ${asset}\$"`). The optional '*' handles binary-mode sums lines.
        $expected = $null
        foreach ($line in (Get-Content -LiteralPath $sumsPath)) {
            if ($line -match '^([0-9a-fA-F]{64})\s+\*?(.+?)\s*$' -and $matches[2] -eq $asset) {
                $expected = $matches[1]; break
            }
        }
        if (-not $expected) { Stop-WithError "SHA256SUMS has no entry for $asset" }

        $actual = (Get-FileHash -LiteralPath $assetPath -Algorithm SHA256).Hash
        # -ne is CASE-INSENSITIVE by default, so uppercase Get-FileHash vs lowercase sums
        # compares correctly with no .ToLower(). Do NOT switch to -cne here.
        if ($actual -ne $expected) {
            Stop-WithError "checksum verification FAILED for $asset — refusing to install"
        }
        Write-Log "checksum verified"

        # --- ADDITIVE second gate. Runs BEFORE the stage+rename so any failure leaves
        #     an existing install untouched (same guarantee as the checksum path). ---
        Invoke-AttestVerify -File $assetPath -Asset $asset -Version $version

        # --- install: stage-in-place (same volume => atomic) + Move-Item -Force ---
        # Staging INSIDE $InstallDir guarantees a same-volume move (Move-Item is atomic
        # only same-volume; cross-volume = non-atomic copy+delete). chmod +x is N/A on
        # Windows (the .exe extension makes it executable).
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
        $exe    = Join-Path $InstallDir 'nole.exe'
        # Random staging name + CreateNew (the O_EXCL analog: throws if the path
        # already exists) so a pre-planted file/symlink in a writable install dir
        # cannot redirect or hijack the staged write — mirrors the Go self-replace
        # (os.CreateTemp). Staging INSIDE $InstallDir keeps the final Move-Item a
        # same-volume (atomic) rename. chmod +x is N/A on Windows (the .exe wins).
        $staged = Join-Path $InstallDir (".nole.install." + [guid]::NewGuid().ToString() + ".exe")
        try {
            $bytes = [System.IO.File]::ReadAllBytes($assetPath)
            $fs = [System.IO.File]::Open($staged, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write)
            try { $fs.Write($bytes, 0, $bytes.Length) } finally { $fs.Dispose() }
        }
        catch { Stop-WithError "could not stage the new binary in $InstallDir (existing install left untouched)" }
        try { Move-Item -LiteralPath $staged -Destination $exe -Force }
        catch {
            # Move-Item -Force overwrites an existing FILE but cannot break a LIVE process
            # lock — irrelevant for first install, only bites a self-replace while nole.exe
            # is running (handled by self-update, not here).
            Stop-WithError "could not move the new binary into place (existing install left untouched)"
        }
        $staged = $null   # consumed by the move; cleanup must not try to remove it
        Write-Log "installed to $exe"

        # --- PATH note (persistent user scope, no admin), dedupe, warn about new shell ---
        # The 'User' EnvironmentVariableTarget is registry-backed and Windows-only; it
        # throws PlatformNotSupportedException on pwsh-on-Linux/macOS (under
        # $ErrorActionPreference='Stop' that would abort AFTER the binary is installed).
        # Guard it so the installer can be run/inspected cross-platform without faulting.
        # $IsWindows is $true only on PS-on-Windows and $false on PS 7+ Linux/macOS; on
        # Windows PowerShell 5.1 it is undefined ($null), so `-not (Test-Path
        # Variable:IsWindows)` keeps 5.1 (which only ever runs on Windows) on the
        # registry path.
        if (-not (Test-Path Variable:IsWindows) -or $IsWindows) {
            $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
            if (-not $userPath) { $userPath = '' }
            if (";$userPath;" -notlike "*;$InstallDir;*") {
                [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $InstallDir), 'User')
                # SetEnvironmentVariable persists to the registry but the CURRENT shell and
                # already-running processes won't see it until restarted — also update the
                # in-session $env:Path AND tell the user to open a new terminal.
                $env:Path = $env:Path.TrimEnd(';') + ';' + $InstallDir
                Write-Log "added $InstallDir to your user PATH — open a NEW terminal (or restart) for it to take effect"
            }
        }

        Write-Log "Nólë works with ZERO keys: keyless DDGS web search out of the box (run 'nole setup --local-extract' to add keyless local URL extraction). Provider keys are optional and only unlock higher-quality/extract routes."
        Write-Log "next: run 'nole doctor' to verify the install."
    } finally {
        # Cleanup analog of install.sh's `trap cleanup EXIT`: remove the temp dir and a
        # leftover staged file (only present on a mid-run failure; on success the move
        # consumed it and $staged was nulled). Best-effort, never masks the real error.
        if ($staged -and (Test-Path -LiteralPath $staged)) {
            Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
        }
        if (Test-Path -LiteralPath $tmpDir) {
            Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# PowerShell requires functions DEFINED before CALLED; all helpers + Invoke-Main are
# above, so the single call below is the entry point (a `main "$@"` analog).
Invoke-Main
