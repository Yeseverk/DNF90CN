@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "DNF90_LAUNCHER=%~dp0runtime\bin\DNF90Launcher.exe"
set "DNF90_CONTROL=%~dp0runtime\bin\DNF90Control.exe"
set "DNF90_VERSION_SOURCE=%~dp0deploy\windows\runtime.version"
set "DNF90_VERSION_INSTALLED=%~dp0runtime\bin\DNF90Build.version"
set "DNF90_REFRESH="

if not exist "%DNF90_LAUNCHER%" set "DNF90_REFRESH=1"
if not exist "%DNF90_CONTROL%" set "DNF90_REFRESH=1"
if exist "%DNF90_VERSION_SOURCE%" (
  if not exist "%DNF90_VERSION_INSTALLED%" set "DNF90_REFRESH=1"
  if exist "%DNF90_VERSION_INSTALLED%" (
    fc /b "%DNF90_VERSION_SOURCE%" "%DNF90_VERSION_INSTALLED%" >nul 2>nul
    if errorlevel 1 set "DNF90_REFRESH=1"
  )
)
if defined DNF90_REFRESH goto bootstrap
goto launch

:bootstrap
if not exist "%DNF90_CONTROL%" goto build_runtime
echo Updating DNF90 local runtime...
set "DNF90_FORCE_CONTROL_BUILD=1"
call "%~dp0deploy\windows\control.bat" stop
set "DNF90_STOP_EXIT=%ERRORLEVEL%"
set "DNF90_FORCE_CONTROL_BUILD="
if not "%DNF90_STOP_EXIT%"=="0" goto stop_failed

:build_runtime
call "%~dp0deploy\windows\control.bat" build --force=true
set "DNF90_BUILD_EXIT=%ERRORLEVEL%"
if not "%DNF90_BUILD_EXIT%"=="0" goto build_failed
goto launch

:stop_failed
echo.
echo [ERROR] Cannot stop the previous DNF90 runtime for update.
pause
exit /b %DNF90_STOP_EXIT%

:build_failed
echo.
echo [ERROR] Cannot prepare the DNF90 login tools.
pause
exit /b %DNF90_BUILD_EXIT%

:launch
start "" "%DNF90_LAUNCHER%"
exit /b %ERRORLEVEL%
