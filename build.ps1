# Windows build helper
$ErrorActionPreference = "Stop"
Set-Location -LiteralPath $PSScriptRoot
go mod tidy
go build -buildmode=c-shared -ldflags "-s -w" -o hi-on-json.dll .
Write-Host "Built: $PSScriptRoot\hi-on-json.dll"
