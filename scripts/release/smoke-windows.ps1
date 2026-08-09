param(
    [Parameter(Mandatory = $true)][string]$Installer,
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$RepoRoot
)

$ErrorActionPreference = "Stop"
$stage = Join-Path $env:RUNNER_TEMP ("atlas-windows-smoke-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $stage | Out-Null
$app = $null

try {
    $install = Start-Process -FilePath $Installer -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/CURRENTUSER' -Wait -PassThru
    if ($install.ExitCode -ne 0) { throw "installer exited $($install.ExitCode)" }

    $association = (Get-Item "HKCU:\Software\Classes\.atlas").GetValue("")
    if ($association -ne "Atlas.Volume") { throw "unexpected .atlas association: $association" }
    $contentType = (Get-Item "HKCU:\Software\Classes\.atlas").GetValue("Content Type")
    if ($contentType -ne "application/vnd.felinestatemachine.atlas") { throw "unexpected content type: $contentType" }
    $command = (Get-Item "HKCU:\Software\Classes\Atlas.Volume\shell\open\command").GetValue("")
    if ($command -notmatch '^"([^"]+Atlas\.exe)" "%1"$') { throw "unexpected open command: $command" }
    $executable = $Matches[1]
    if (-not (Test-Path $executable -PathType Leaf)) { throw "installed executable missing: $executable" }

    $cache = Join-Path $stage "cache"
    $source = Join-Path $stage "source"
    & go run "$RepoRoot/cmd/atlas" build -cache $cache -bundles $source "$RepoRoot/examples/sample-region.atlas-project"
    if ($LASTEXITCODE -ne 0) { throw "Sample Region fixture build exited $LASTEXITCODE" }
    $artifact = Get-ChildItem $source -Filter "sample-region-*.atlas" | Select-Object -First 1
    if ($null -eq $artifact) { throw "Sample Region fixture was not built" }

    $library = Join-Path $stage "library"
    $data = Join-Path $stage "data"
    New-Item -ItemType Directory -Path $library, $data | Out-Null
    $env:ATLAS_BUNDLES_DIR = $library
    $env:ATLAS_DATA_DIR = $data
    $app = Start-Process -FilePath $executable -ArgumentList $artifact.FullName -PassThru
    $installed = $null
    foreach ($attempt in 1..60) {
        $installed = Get-ChildItem $library -Filter "sample-region-*.atlas" -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($null -ne $installed) { break }
        if ($app.HasExited) { throw "installed Atlas exited before file intake with $($app.ExitCode)" }
        Start-Sleep -Seconds 1
    }
    if ($null -eq $installed) { throw "installed Atlas did not intake Sample Region" }
    if ((Get-FileHash $artifact.FullName -Algorithm SHA256).Hash -ne (Get-FileHash $installed.FullName -Algorithm SHA256).Hash) {
        throw "installed Atlas changed the handed-off artifact"
    }
}
finally {
    if ($null -ne $app -and -not $app.HasExited) { Stop-Process -Id $app.Id -Force }
    if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
}
