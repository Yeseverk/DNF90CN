@echo off
setlocal EnableExtensions DisableDelayedExpansion
call "%~dp0deploy\windows\control.bat" launch-client %*
set "EXIT_CODE=%ERRORLEVEL%"
echo.
if not "%EXIT_CODE%"=="0" pause
exit /b %EXIT_CODE%
