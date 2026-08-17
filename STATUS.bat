@echo off
setlocal EnableExtensions DisableDelayedExpansion
call "%~dp0deploy\windows\control.bat" status %*
set "EXIT_CODE=%ERRORLEVEL%"
echo.
pause
exit /b %EXIT_CODE%
