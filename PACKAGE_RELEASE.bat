@echo off
setlocal EnableExtensions DisableDelayedExpansion

call :resolve_go
if errorlevel 1 (
  echo [ERROR] Go was not found.
  echo         Install the version declared in go-server\go.mod, add it to PATH,
  echo         or set DNF90_GO_EXECUTABLE to its full path before packaging.
  pause
  exit /b 10
)

pushd "%~dp0go-server"
if errorlevel 1 (
  echo [ERROR] Cannot enter go-server.
  pause
  exit /b 11
)

"%DNF90_GO_EXE%" run -buildvcs=false .\cmd\server\release -root "%~dp0." %*
set "EXIT_CODE=%ERRORLEVEL%"
popd

echo.
if not "%EXIT_CODE%"=="0" pause
exit /b %EXIT_CODE%

:resolve_go
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
for %%G in ("%DNF90_GO_EXE%") do set "DNF90_GO_DIR=%%~dpG"
if defined DNF90_GO_DIR set "PATH=%DNF90_GO_DIR%;%PATH%"
exit /b 0

:probe_go
if defined DNF90_GO_EXE exit /b 0
if "%~1"=="" exit /b 0
if exist "%~1" set "DNF90_GO_EXE=%~1"
exit /b 0
