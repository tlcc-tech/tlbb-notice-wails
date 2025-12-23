# Requires: Go 1.22+ (or project go.mod requirement), Node.js 18+, Wails CLI v2
# Run this in PowerShell from the project root.

$ErrorActionPreference = "Stop"

$Version = "1.0.9"
wails build -platform windows/amd64 -clean -ldflags "-X main.AppVersion=$Version"
Write-Host "Build output is under build/bin/"
