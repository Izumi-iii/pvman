$ErrorActionPreference = "Stop"

$GoVersion = if ($env:PV_MAN_GO_VERSION) { $env:PV_MAN_GO_VERSION } else { "1.26.1" }
$GoRoot = Join-Path $env:LOCALAPPDATA "pvman\go$GoVersion"
$GoBin = Join-Path $env:USERPROFILE "go\bin"
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("pvman-install-" + [Guid]::NewGuid())

New-Item -ItemType Directory -Path $TempDir -Force | Out-Null
try {
    $Architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    switch ($Architecture) {
        "X64" { $GoArch = "amd64" }
        "Arm64" { $GoArch = "arm64" }
        default { throw "Unsupported Windows architecture: $Architecture" }
    }

    $Archive = "go$GoVersion.windows-$GoArch.zip"
    $Url = "https://go.dev/dl/$Archive"
    $ArchivePath = Join-Path $TempDir $Archive
    $ExtractPath = Join-Path $TempDir "extracted"

    Write-Host "Downloading Go $GoVersion for windows-$GoArch..."
    Invoke-WebRequest -Uri $Url -OutFile $ArchivePath
    Expand-Archive -Path $ArchivePath -DestinationPath $ExtractPath -Force

    if (Test-Path $GoRoot) {
        Remove-Item -Path $GoRoot -Recurse -Force
    }
    New-Item -ItemType Directory -Path (Split-Path $GoRoot) -Force | Out-Null
    Move-Item -Path (Join-Path $ExtractPath "go") -Destination $GoRoot

    New-Item -ItemType Directory -Path $GoBin -Force | Out-Null

    # Only add %USERPROFILE%\go\bin (for pvman and the versioned wrapper),
    # not the SDK's bin directory, so the user's default `go` is untouched.
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $PathEntries = @($UserPath -split ";" | Where-Object { $_ })
    if ($PathEntries -notcontains $GoBin) {
        $PathEntries += $GoBin
        [Environment]::SetEnvironmentVariable("Path", ($PathEntries -join ";"), "User")
    }

    # Versioned wrapper: lets users run `go1.26.1 ...` without replacing `go`.
    $WrapperPath = Join-Path $GoBin "go$GoVersion.cmd"
    $WrapperContent = "@echo off`r`n`"$GoRoot\bin\go.exe`" %*"
    Set-Content -Path $WrapperPath -Value $WrapperContent -Encoding ASCII

    $env:GOBIN = $GoBin
    $env:Path = "$GoBin;$env:Path"

    & (Join-Path $GoRoot "bin\go.exe") version
    & $WrapperPath install github.com/tkzzzzzz6/pvman@latest

    Write-Host "pvman installed successfully."
    Write-Host "Open a new PowerShell window, then run: pvman"
}
finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force
    }
}
