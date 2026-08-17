@echo off
setlocal EnableExtensions DisableDelayedExpansion
call "%~dp0deploy\windows\control.bat" start %*
set "EXIT_CODE=%ERRORLEVEL%"
echo.
if not "%EXIT_CODE%"=="0" pause
exit /b %EXIT_CODE%
