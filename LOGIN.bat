@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "DNF90_ROOT=%~dp0"
set "DNF90_LAUNCHER=%DNF90_ROOT%runtime\bin\DNF90Launcher.exe"
set "DNF90_CONTROL=%DNF90_ROOT%runtime\bin\DNF90Control.exe"
set "DNF90_DOCTOR=%DNF90_ROOT%runtime\bin\DNF90Doctor.exe"
set "DNF90_SERVER=%DNF90_ROOT%runtime\bin\DNF90Server.exe"
set "DNF90_VERSION_SOURCE=%DNF90_ROOT%deploy\windows\runtime.version"
set "DNF90_VERSION_INSTALLED=%DNF90_ROOT%runtime\bin\DNF90Build.version"
set "DNF90_REFRESH="
set "DNF90_RUNTIME_INCOMPLETE="
set "DNF90_GO_EXE="

if not exist "%DNF90_VERSION_SOURCE%" goto version_source_failed

rem A release package always ships the four runtime executables together with a
rem DNF90Build.version equal to deploy\windows\runtime.version. The ordinary
rem player therefore falls straight through to the launcher window below and
rem never sees a console message.
if not exist "%DNF90_LAUNCHER%" set "DNF90_RUNTIME_INCOMPLETE=1"
if not exist "%DNF90_CONTROL%" set "DNF90_RUNTIME_INCOMPLETE=1"
if not exist "%DNF90_DOCTOR%" set "DNF90_RUNTIME_INCOMPLETE=1"
if not exist "%DNF90_SERVER%" set "DNF90_RUNTIME_INCOMPLETE=1"
if defined DNF90_RUNTIME_INCOMPLETE set "DNF90_REFRESH=1"

rem DNF90Build.version is written only by a controller build of the current
rem source tree. A missing marker therefore proves the installed executables
rem are older than the marker itself and cannot contain the current server
rem fixes; a differing marker proves the source moved on. Both cases need a
rem real rebuild. This script must never stamp the marker onto executables it
rem did not build: that would report a stale runtime as current and silently
rem keep the old server behaviour forever.
if not exist "%DNF90_VERSION_INSTALLED%" set "DNF90_REFRESH=1"
if exist "%DNF90_VERSION_INSTALLED%" call :compare_installed_version
if not defined DNF90_REFRESH goto launch

:bootstrap
echo Updating DNF90 local runtime...
call :find_go
if errorlevel 1 goto go_required

rem Stop through the controller that is already installed. Rebuilding the
rem controller before this point is what used to turn "cannot update" into
rem "cannot start" on a machine without Go, because the existing controller was
rem perfectly able to stop its own service.
if not exist "%DNF90_CONTROL%" goto build_runtime
call "%DNF90_ROOT%deploy\windows\control.bat" stop
set "DNF90_STOP_EXIT=%ERRORLEVEL%"
if not "%DNF90_STOP_EXIT%"=="0" goto stop_failed

:build_runtime
rem control.bat replaces DNF90Control.exe itself, then the fresh controller
rem builds DNF90Server/Doctor/Launcher and writes DNF90Build.version. A running
rem controller cannot overwrite its own image, so the force flag has to be set
rem here, after the stop above, or the controller would never be refreshed.
set "DNF90_FORCE_CONTROL_BUILD=1"
call "%DNF90_ROOT%deploy\windows\control.bat" build --force=true
set "DNF90_BUILD_EXIT=%ERRORLEVEL%"
set "DNF90_FORCE_CONTROL_BUILD="
if not "%DNF90_BUILD_EXIT%"=="0" goto build_failed
goto launch

:compare_installed_version
fc /b "%DNF90_VERSION_SOURCE%" "%DNF90_VERSION_INSTALLED%" >nul 2>nul
if errorlevel 1 set "DNF90_REFRESH=1"
exit /b 0

:find_go
set "DNF90_GO_EXE="
if defined DNF90_GO_EXECUTABLE if exist "%DNF90_GO_EXECUTABLE%" set "DNF90_GO_EXE=%DNF90_GO_EXECUTABLE%"
if defined DNF90_GO_EXE goto found_go
where go.exe >nul 2>nul
if not errorlevel 1 set "DNF90_GO_EXE=go.exe"
if defined DNF90_GO_EXE goto found_go
rem Each candidate is probed through a call, never inside a parenthesised
rem block: the expansion of %ProgramFiles(x86)% carries its own parentheses.
call :probe_go "%ProgramFiles%\Go\bin\go.exe"
call :probe_go "%ProgramW6432%\Go\bin\go.exe"
call :probe_go "%ProgramFiles(x86)%\Go\bin\go.exe"
call :probe_go "%LOCALAPPDATA%\Programs\Go\bin\go.exe"
call :probe_go "%SystemDrive%\Go\bin\go.exe"
if not defined DNF90_GO_EXE exit /b 1

:found_go
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

:go_required
echo.
if defined DNF90_RUNTIME_INCOMPLETE goto runtime_incomplete
echo [ERROR] The installed DNF90 runtime does not match this source tree, and
echo         Go was not found, so it cannot be rebuilt here.
echo         runtime\bin still holds executables older than the current server
echo         fixes. Starting them would reproduce the bugs this build repairs,
echo         so LOGIN stops instead of running them.
echo.
echo         Choose one:
echo           - run a release package produced by PACKAGE_RELEASE.bat, or
echo           - install the Go version from go-server\go.mod on this machine
echo             and run REBUILD.bat.
echo         If Go is installed outside PATH, set DNF90_GO_EXECUTABLE to the
echo         full path of go.exe and start LOGIN.bat again.
pause
exit /b 10

:runtime_incomplete
echo.
echo [ERROR] runtime\bin does not contain the four DNF90 executables
echo         (DNF90Control.exe, DNF90Doctor.exe, DNF90Launcher.exe,
echo         DNF90Server.exe), and Go was not found to build them.
echo         A plain source checkout can never contain them, because runtime\bin
echo         is ignored by Git.
echo.
echo         Choose one:
echo           - run a release package produced by PACKAGE_RELEASE.bat, or
echo           - install the Go version from go-server\go.mod on this machine
echo             and run REBUILD.bat.
echo         If Go is installed outside PATH, set DNF90_GO_EXECUTABLE to the
echo         full path of go.exe and start LOGIN.bat again.
pause
exit /b 12

:version_source_failed
echo.
echo [ERROR] Missing deploy\windows\runtime.version.
echo         This directory is not a complete DNF90 release or source tree.
pause
exit /b 11

:stop_failed
echo.
echo [ERROR] Cannot stop the previous DNF90 runtime safely, so its executables
echo         must not be replaced while they are still in use.
echo         Run STATUS.bat and check runtime\logs before retrying.
pause
exit /b %DNF90_STOP_EXIT%

:build_failed
echo.
echo [ERROR] Cannot prepare the DNF90 login tools.
pause
exit /b %DNF90_BUILD_EXIT%

:launch
if not exist "%DNF90_LAUNCHER%" goto runtime_incomplete
start "" "%DNF90_LAUNCHER%"
exit /b 0
