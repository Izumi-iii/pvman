$ErrorActionPreference = "Stop"

$GoVersion = if ($env:PV_MAN_GO_VERSION) { $env:PV_MAN_GO_VERSION } else { "1.26.1" }
$GoRoot = Join-Path $env:LOCALAPPDATA "pvman\go"
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

    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $PathEntries = @($UserPath -split ";" | Where-Object { $_ })
    foreach ($PathEntry in @((Join-Path $GoRoot "bin"), $GoBin)) {
        if ($PathEntries -notcontains $PathEntry) {
            $PathEntries += $PathEntry
        }
    }
    [Environment]::SetEnvironmentVariable("Path", ($PathEntries -join ";"), "User")

    $env:Path = "$(Join-Path $GoRoot 'bin');$GoBin;$env:Path"
    & (Join-Path $GoRoot "bin\go.exe") version
    & (Join-Path $GoRoot "bin\go.exe") install github.com/tkzzzzzz6/pvman@latest

    Write-Host "pvman installed successfully."
    Write-Host "Open a new PowerShell window, then run: pvman"
}
finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force
    }
}
