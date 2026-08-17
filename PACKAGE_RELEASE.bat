@echo off
setlocal EnableExtensions DisableDelayedExpansion

where go.exe >nul 2>nul
if errorlevel 1 (
  echo [ERROR] Go was not found.
  echo         Install the version declared in go-server\go.mod before packaging.
  pause
  exit /b 10
)

pushd "%~dp0go-server"
if errorlevel 1 (
  echo [ERROR] Cannot enter go-server.
  pause
  exit /b 11
)

go run -buildvcs=false .\cmd\server\release -root "%~dp0." %*
set "EXIT_CODE=%ERRORLEVEL%"
popd

echo.
if not "%EXIT_CODE%"=="0" pause
exit /b %EXIT_CODE%
