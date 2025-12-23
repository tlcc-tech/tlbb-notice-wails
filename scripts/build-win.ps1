# Requires: Go 1.22+ (or project go.mod requirement), Node.js 18+, Wails CLI v2
# Run this in PowerShell from the project root.

$ErrorActionPreference = "Stop"

wails build -platform windows/amd64 -clean
Write-Host "Build output is under build/bin/"