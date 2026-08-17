@echo off
setlocal EnableExtensions DisableDelayedExpansion

for %%I in ("%~dp0..\..") do set "DNF90_PROJECT_ROOT=%%~fI"
set "DNF90_CONTROL_EXE=%DNF90_PROJECT_ROOT%\runtime\bin\DNF90Control.exe"
set "DNF90_CONTROL_NEW=%DNF90_PROJECT_ROOT%\runtime\bin\DNF90Control.new.%RANDOM%.%RANDOM%.exe"

if /I "%DNF90_FORCE_CONTROL_BUILD%"=="1" call :build_controller
if errorlevel 1 exit /b %ERRORLEVEL%

if not exist "%DNF90_CONTROL_EXE%" call :build_controller
if errorlevel 1 exit /b %ERRORLEVEL%

"%DNF90_CONTROL_EXE%" %*
exit /b %ERRORLEVEL%

:build_controller
call :resolve_go
if errorlevel 1 (
  if exist "%DNF90_CONTROL_EXE%" (
    echo [ERROR] Go was not found; the existing DNF90Control.exe cannot be rebuilt.
    echo         Add Go to PATH or set DNF90_GO_EXECUTABLE to its full path.
  ) else (
    echo [ERROR] DNF90Control.exe is missing and Go was not found.
    echo         Install Go, add it to PATH, or restore runtime\bin\DNF90Control.exe.
  )
  exit /b 10
)

if not exist "%DNF90_PROJECT_ROOT%\runtime\bin" (
  mkdir "%DNF90_PROJECT_ROOT%\runtime\bin"
  if errorlevel 1 (
    echo [ERROR] Cannot create runtime\bin.
    exit /b 11
  )
)

if exist "%DNF90_CONTROL_NEW%" del /f /q "%DNF90_CONTROL_NEW%" >nul 2>nul
pushd "%DNF90_PROJECT_ROOT%\go-server"
if errorlevel 1 (
  echo [ERROR] Cannot enter go-server.
  exit /b 12
)

echo Building DNF90 native controller...
"%DNF90_GO_EXE%" build -buildvcs=false -mod=readonly -trimpath -o "%DNF90_CONTROL_NEW%" .\cmd\server\control
set "DNF90_BUILD_EXIT=%ERRORLEVEL%"
popd

if not "%DNF90_BUILD_EXIT%"=="0" (
  if exist "%DNF90_CONTROL_NEW%" del /f /q "%DNF90_CONTROL_NEW%" >nul 2>nul
  echo [ERROR] DNF90 controller build failed.
  exit /b %DNF90_BUILD_EXIT%
)

move /y "%DNF90_CONTROL_NEW%" "%DNF90_CONTROL_EXE%" >nul
if errorlevel 1 (
  echo [ERROR] Cannot install DNF90Control.exe.
  exit /b 13
)
echo Controller ready: "%DNF90_CONTROL_EXE%"
exit /b 0

:resolve_go
rem LOGIN.bat may already have discovered a standard Windows Go installation
rem that is not on PATH and exported it as DNF90_GO_EXE; honour that first so
rem the wrapper and the controller always agree on one toolchain.
if defined DNF90_GO_EXE goto resolved_go
if defined DNF90_GO_EXECUTABLE if exist "%DNF90_GO_EXECUTABLE%" set "DNF90_GO_EXE=%DNF90_GO_EXECUTABLE%"
if defined DNF90_GO_EXE goto resolved_go
where go.exe >nul 2>nul
if not errorlevel 1 set "DNF90_GO_EXE=go.exe"
if defined DNF90_GO_EXE goto resolved_go
rem Each candidate is probed through a call, never inside a parenthesised
rem block: the expansion of %ProgramFiles(x86)% carries its own parentheses.
call :probe_go "%ProgramFiles%\Go\bin\go.exe"
call :probe_go "%ProgramW6432%\Go\bin\go.exe"
call :probe_go "%ProgramFiles(x86)%\Go\bin\go.exe"
call :probe_go "%LOCALAPPDATA%\Programs\Go\bin\go.exe"
call :probe_go "%SystemDrive%\Go\bin\go.exe"
if not defined DNF90_GO_EXE exit /b 1

:resolved_go
call :prepend_go_path
exit /b 0

:probe_go
if defined DNF90_GO_EXE exit /b 0
if "%~1"=="" exit /b 0
if exist "%~1" set "DNF90_GO_EXE=%~1"
exit /b 0

:prepend_go_path
if not defined DNF90_GO_EXE exit /b 0
for %%G in ("%DNF90_GO_EXE%") do set "DNF90_GO_DIR=%%~dpG"
if defined DNF90_GO_DIR set "PATH=%DNF90_GO_DIR%;%PATH%"
exit /b 0
