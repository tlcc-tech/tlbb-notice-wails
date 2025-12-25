# Requires: Go 1.22+ (or project go.mod requirement), Node.js 18+, Wails CLI v2
# Run this in PowerShell from the project root.

$ErrorActionPreference = "Stop"

$Version = "dev"
$match = Select-String -Path "wails.json" -Pattern '"productVersion"\s*:\s*"([^"]+)"' | Select-Object -First 1
if ($match -and $match.Matches.Count -gt 0) {
	$Version = $match.Matches[0].Groups[1].Value
}
wails build -platform windows/amd64 -clean -ldflags "-X main.AppVersion=$Version"
Write-Host "Build output is under build/bin/"
