function Write-DaasArtifactChecksums {
    param(
        [Parameter(Mandatory)][string]$OutDir,
        [Parameter(Mandatory)][string]$Platform,
        [Parameter(Mandatory)][string[]]$ArtifactNames
    )

    $Root = (Resolve-Path -LiteralPath $OutDir).Path
    $Lines = foreach ($Name in $ArtifactNames) {
        if ([string]::IsNullOrWhiteSpace($Name) -or [IO.Path]::IsPathRooted($Name) -or $Name.IndexOfAny([char[]]@('\', '/')) -ge 0) {
            throw "Artifact name must be a plain file name: $Name"
        }
        $Path = Join-Path $Root $Name
        if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
            throw "Cannot checksum missing artifact: $Name"
        }
        $Hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
        "$Hash  $Name"
    }
    [IO.File]::WriteAllLines(
        (Join-Path $Root "SHA256SUMS-$Platform.txt"),
        [string[]]$Lines,
        [Text.Encoding]::ASCII
    )
}
