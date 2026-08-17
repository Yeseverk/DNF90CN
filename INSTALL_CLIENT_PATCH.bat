@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "CLIENT_DIR=%~1"
if not defined CLIENT_DIR (
  echo Enter the full DNF client directory.
  echo Example: D:\Games\DNF
  set /p "CLIENT_DIR=Client directory: "
)

if not defined CLIENT_DIR (
  echo [ERROR] Client directory is required.
  pause
  exit /b 10
)

for %%I in ("%CLIENT_DIR%") do set "CLIENT_DIR=%%~fI"
if not exist "%CLIENT_DIR%\DNF.exe" if not exist "%CLIENT_DIR%\NoPack.exe" (
  echo [ERROR] DNF.exe or NoPack.exe was not found in:
  echo         "%CLIENT_DIR%"
  pause
  exit /b 11
)

tasklist /FI "IMAGENAME eq DNF.exe" 2>nul | find /I "DNF.exe" >nul
if not errorlevel 1 (
  echo [ERROR] Close DNF.exe before installing the compatibility DLL.
  pause
  exit /b 12
)

if not exist "%~dp0client-patch\bin\90CN.dll" (
  echo [ERROR] Packaged client-patch\bin\90CN.dll is missing.
  pause
  exit /b 13
)

copy /Y "%~dp0client-patch\bin\90CN.dll" "%CLIENT_DIR%\90CN.dll" >nul
if errorlevel 1 (
  echo [ERROR] Cannot install 90CN.dll into:
  echo         "%CLIENT_DIR%"
  pause
  exit /b 14
)

echo Client patch installed:
echo "%CLIENT_DIR%\90CN.dll"
echo.
echo Set client.directory in runtime\config\instance.json to this directory,
echo then run LAUNCH_CLIENT.bat.
pause
exit /b 0
