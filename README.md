# Claude Desktop on Older Windows 10

Install procedure for getting Claude Desktop running on Windows 10 builds older
than the official 2004 (build 19041) requirement. Tested on Windows 10 1803
(build 17134).

## Status

- Chat and core functionality: works.
- Cowork (the screen-sharing assistant feature): does not work and cannot be
  made to work — it uses Windows APIs that genuinely don't exist on older
  builds. The app correctly detects this and disables the feature with an
  in-app notification. Everything else is unaffected.

## Prerequisites

1. Windows 10, build 17134 or later. (Older may work; untested.)
2. Developer Mode enabled:
   Settings -> Update & Security -> For Developers -> "Developer mode".
3. PowerShell run as Administrator.
4. ~250 MB free disk space.

## Install

Run the following from an elevated PowerShell prompt. Each block is one step.

### 1. Download the MSIX

```powershell
$url  = "https://api.anthropic.com/api/desktop/win32/x64/msix/latest/redirect"
$msix = "$env:TEMP\Claude-latest.msix"
Invoke-WebRequest -Uri $url -OutFile $msix -UseBasicParsing
```

### 2. Extract the MSIX

MSIX files are ZIP archives.

```powershell
$extractDir = "C:\ClaudeExtracted"
Copy-Item $msix "$extractDir.zip" -Force
Expand-Archive "$extractDir.zip" -DestinationPath $extractDir -Force
```

### 3. Patch the manifest's MinVersion

The manifest declares `MinVersion="10.0.18362.0"` (Windows 10 1903). Lower it
to your actual build so Windows accepts the package. The example below uses
`10.0.17134.0` (Windows 10 1803); change it if your build is different.

```powershell
$manifest = "$extractDir\AppxManifest.xml"
(Get-Content $manifest) -replace 'MinVersion="10\.0\.18362\.0"', 'MinVersion="10.0.17134.0"' | Set-Content $manifest
```

### 4. Register the package

```powershell
Add-AppxPackage -Register "$extractDir\AppxManifest.xml"
```

The app will appear in the Start menu. Launch it from there.

Do **not** run the original `Claude Setup.exe` again — it will always fail at
the signature check on older Windows builds. The setup binary is only useful
for triggering a fresh MSIX download, which the steps above do directly.

## Why this works

The official installer fails on older Windows builds for two reasons:

1. **Version gate.** It checks for build 19041+ and refuses to proceed.
2. **MSIX signature verification.** It calls `WinVerifyTrust` on the downloaded
   package, which fails with `TRUST_E_PROVIDER_UNKNOWN` (`0x800B0003`) because
   older Windows builds don't have the trust providers needed to validate
   Anthropic's MSIX signing format.

The PowerShell route bypasses both. `Invoke-WebRequest` fetches the MSIX
without going through the installer. `Add-AppxPackage -Register` from an
extracted package layout uses the native sideloading path, which doesn't
invoke `WinVerifyTrust`. Editing the manifest's `MinVersion` is what lets
Windows accept the package given the OS doesn't actually meet the package's
declared minimum.

## Files in this repo

- `Claude Setup.exe` — the original installer with a patched version-check
  error string (the comparison is unchanged; only the message was edited).
  Not needed for the install steps above.
- `Claude Launcher.exe` — wrapper that runs `Claude Setup.exe` with
  `ntdll!RtlGetVersion` patched in the child process to report build 19041.
  Not needed for the install steps above; useful if you want to inspect what
  the official installer downloads.
- `launcher/` — Go source for the launcher. Cross-compile for Windows with:
  ```
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "../Claude Launcher.exe" .
  ```
