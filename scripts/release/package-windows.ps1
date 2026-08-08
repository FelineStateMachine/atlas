param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$Binary,
    [Parameter(Mandatory = $true)][string]$Dist
)

$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
$iscc = Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6/ISCC.exe"
if (-not (Test-Path $iscc)) {
    throw "Inno Setup 6 is required at $iscc"
}

New-Item -ItemType Directory -Force -Path $Dist | Out-Null
$outputBase = "Atlas-$Version-windows-amd64-setup"
& $iscc "/DMyAppVersion=$($Version.TrimStart('v'))" "/DMyAppExe=$Binary" "/DOutputDir=$Dist" "/DOutputBase=$outputBase" (Join-Path $root "packaging/windows/Atlas.iss")
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup failed with exit code $LASTEXITCODE"
}

$installer = Join-Path $Dist "$outputBase.exe"
if (-not (Test-Path $installer)) {
    throw "Inno Setup did not create $installer"
}
