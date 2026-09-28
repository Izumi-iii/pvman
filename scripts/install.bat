@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "& { $script = (Invoke-WebRequest -UseBasicParsing 'https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.ps1').Content; Invoke-Expression $script }"
if errorlevel 1 (
    echo pvman installation failed.
    exit /b 1
)
echo pvman installation completed. Open a new terminal and run pvman.
endlocal
