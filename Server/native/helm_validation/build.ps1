param([string]$OutDir = (Join-Path $PSScriptRoot 'dist'))
$ErrorActionPreference = 'Stop'
$Version = 'v1.0.0'
$HelmVersion = 'v3.22.1'
$Platform = "$(go env GOOS)-$(go env GOARCH)"
. (Join-Path $PSScriptRoot 'write_checksums.ps1')
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
& (Join-Path $PSScriptRoot 'build_checksum_test.ps1')
Push-Location $PSScriptRoot
try {
    go test -mod=readonly ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    $Binary = 'libdaas_helm_validation.dll'
    $env:CGO_ENABLED = '1'
    go build -mod=readonly -trimpath -buildmode=c-shared -ldflags '-extldflags=-static' -o (Join-Path $OutDir $Binary) .
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}
$Header = Join-Path $OutDir 'daas_helm_validation.h'
Copy-Item (Join-Path $OutDir 'libdaas_helm_validation.h') $Header -Force
$Digest = (Get-FileHash -Algorithm SHA256 (Join-Path $OutDir $Binary)).Hash.ToLowerInvariant()
@{
    version = $Version
    abi_version = 2
    helm_version = $HelmVersion
    platform = $Platform
    library = $Binary
    library_sha256 = $Digest
    header = 'daas_helm_validation.h'
} | ConvertTo-Json | ForEach-Object { [IO.File]::WriteAllText((Join-Path $OutDir 'manifest.json'), $_ + "`n", [Text.UTF8Encoding]::new($false)) }
python (Join-Path $PSScriptRoot 'smoke_ctypes.py') (Join-Path $OutDir $Binary)
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$Archive = Join-Path $OutDir "daas-helm-validation-$Version-$Platform.zip"
Compress-Archive -Path (Join-Path $OutDir $Binary), $Header, (Join-Path $OutDir 'manifest.json') -DestinationPath $Archive -Force
$ChecksumFiles = @($Binary, $Header, 'manifest.json', [IO.Path]::GetFileName($Archive))
Write-DaasArtifactChecksums -OutDir $OutDir -Platform $Platform -ArtifactNames $ChecksumFiles
