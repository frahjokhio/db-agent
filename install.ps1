# Install db-agent (Windows) without Node or any runtime.
#   irm https://raw.githubusercontent.com/frahjokhio/db-agent/main/install.ps1 | iex
# Options (env vars): $env:DB_AGENT_VERSION = "v0.1.0"; $env:DB_AGENT_INSTALL_DIR = "C:\tools\db-agent"
$ErrorActionPreference = "Stop"
$repo = "frahjokhio/db-agent"
$version = if ($env:DB_AGENT_VERSION) { $env:DB_AGENT_VERSION } else { "latest" }
$dir = if ($env:DB_AGENT_INSTALL_DIR) { $env:DB_AGENT_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "db-agent\bin" }

$arch = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "x64" }
$asset = "db-agent-win32-$arch.exe"
$url = if ($version -eq "latest") { "https://github.com/$repo/releases/latest/download/$asset" }
       else { "https://github.com/$repo/releases/download/$version/$asset" }

New-Item -ItemType Directory -Force -Path $dir | Out-Null
$exe = Join-Path $dir "db-agent.exe"
Write-Host "Downloading $url"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exe

# Add to the user's PATH (no admin needed).
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not ($userPath -split ";" | Where-Object { $_ -eq $dir })) {
    [Environment]::SetEnvironmentVariable("Path", ($userPath.TrimEnd(";") + ";" + $dir), "User")
    Write-Host "Added $dir to your PATH (open a new terminal to use it)."
}

Write-Host "OK: installed $exe ($(& $exe version))"
Write-Host "Next: cd your-project; db-agent init"
