@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "DNF90_LAUNCHER=%~dp0runtime\bin\DNF90Launcher.exe"
if not exist "%DNF90_LAUNCHER%" (
  echo [ERROR] DNF90Launcher.exe is missing.
  echo         Run REBUILD.bat or restore the release runtime\bin directory.
  pause
  exit /b 10
)

start "" "%DNF90_LAUNCHER%"
exit /b %ERRORLEVEL%
