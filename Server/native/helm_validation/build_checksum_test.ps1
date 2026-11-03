$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'write_checksums.ps1')
$TempRoot = Join-Path ([IO.Path]::GetTempPath()) ("daas-checksum-test-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $TempRoot | Out-Null
try {
    [IO.File]::WriteAllText((Join-Path $TempRoot 'library.dll'), 'library')
    [IO.File]::WriteAllText((Join-Path $TempRoot 'daas_helm_validation.h'), 'header')
    [IO.File]::WriteAllText((Join-Path $TempRoot 'manifest.json'), '{}')
    [IO.File]::WriteAllText((Join-Path $TempRoot 'daas.zip'), 'archive')
    [IO.File]::WriteAllText((Join-Path $TempRoot 'SHA256SUMS-win-amd64.txt'), 'stale previous checksum')
    [IO.File]::WriteAllText((Join-Path $TempRoot 'SHA256SUMS-linux-amd64.txt'), 'stale platform checksum')
    $Header = Join-Path $TempRoot 'daas_helm_validation.h'
    $Archive = Join-Path $TempRoot 'daas.zip'
    $Names = @('library.dll', [IO.Path]::GetFileName($Header), 'manifest.json', [IO.Path]::GetFileName($Archive))

    $BuildScript = Get-Content -Raw (Join-Path $PSScriptRoot 'build.ps1')
    if ($BuildScript -notmatch '\$ChecksumFiles\s*=\s*@\(\$Binary,\s*\[IO\.Path\]::GetFileName\(\$Header\),\s*''manifest\.json'',\s*\[IO\.Path\]::GetFileName\(\$Archive\)\)') {
        throw 'Build script checksum list must pass only artifact file names'
    }

    foreach ($InvalidName in @($Header, 'nested\header.h', '../manifest.json')) {
        try {
            Write-DaasArtifactChecksums -OutDir $TempRoot -Platform 'invalid' -ArtifactNames @($InvalidName)
            throw "Checksum writer accepted a path instead of a plain artifact name: $InvalidName"
        } catch {
            if ($_.Exception.Message -like 'Checksum writer accepted*') { throw }
        }
    }

    Write-DaasArtifactChecksums -OutDir $TempRoot -Platform 'win-amd64' -ArtifactNames $Names
    $First = [IO.File]::ReadAllLines((Join-Path $TempRoot 'SHA256SUMS-win-amd64.txt'))
    Write-DaasArtifactChecksums -OutDir $TempRoot -Platform 'win-amd64' -ArtifactNames $Names
    $Second = [IO.File]::ReadAllLines((Join-Path $TempRoot 'SHA256SUMS-win-amd64.txt'))

    if ($First.Count -ne $Names.Count -or $Second.Count -ne $Names.Count) {
        throw 'Checksum output included stale checksum files or omitted a fresh artifact'
    }
    foreach ($Name in $Names) {
        $Hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $TempRoot $Name)).Hash.ToLowerInvariant()
        if (-not ($Second | Where-Object { $_ -eq "$Hash  $Name" })) {
            throw "Checksum list did not contain the current hash for $Name"
        }
    }
} finally {
    $ResolvedTempRoot = [IO.Path]::GetFullPath($TempRoot)
    $ResolvedTempBase = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    if (-not $ResolvedTempRoot.StartsWith($ResolvedTempBase, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Refusing to clean checksum test directory outside the system temp folder'
    }
    Remove-Item -LiteralPath $ResolvedTempRoot -Recurse -Force
}
