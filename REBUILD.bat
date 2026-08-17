@echo off
setlocal EnableExtensions DisableDelayedExpansion
set "DNF90_FORCE_CONTROL_BUILD=1"
call "%~dp0deploy\windows\control.bat" build --force=true %*
set "EXIT_CODE=%ERRORLEVEL%"
echo.
pause
exit /b %EXIT_CODE%
