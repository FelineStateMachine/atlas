param([Parameter(Mandatory = $true)][string]$Path)

$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CERTIFICATE_BASE64)) {
    Write-Host "WINDOWS_CERTIFICATE_BASE64 is empty; leaving $Path unsigned"
    exit 0
}
if ([string]::IsNullOrWhiteSpace($env:WINDOWS_CERTIFICATE_PASSWORD)) {
    throw "WINDOWS_CERTIFICATE_PASSWORD is required when a signing certificate is provided"
}

$certificate = Join-Path $env:RUNNER_TEMP "atlas-release.pfx"
$signTool = Get-ChildItem "${env:ProgramFiles(x86)}\Windows Kits\10\bin\*\x64\signtool.exe" |
    Sort-Object FullName -Descending |
    Select-Object -First 1
if ($null -eq $signTool) {
    throw "signtool.exe was not found in the Windows 10 SDK"
}
[IO.File]::WriteAllBytes($certificate, [Convert]::FromBase64String($env:WINDOWS_CERTIFICATE_BASE64))
try {
    & $signTool.FullName sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 /f $certificate /p $env:WINDOWS_CERTIFICATE_PASSWORD $Path
    if ($LASTEXITCODE -ne 0) { throw "signtool failed with exit code $LASTEXITCODE" }
    & $signTool.FullName verify /pa /v $Path
    if ($LASTEXITCODE -ne 0) { throw "signature verification failed with exit code $LASTEXITCODE" }
} finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $certificate
}
