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
where go.exe >nul 2>nul
if errorlevel 1 (
  echo [ERROR] DNF90Control.exe is missing and Go was not found.
  echo         Install Go or restore runtime\bin\DNF90Control.exe.
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
go build -buildvcs=false -mod=readonly -trimpath -o "%DNF90_CONTROL_NEW%" .\cmd\server\control
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
