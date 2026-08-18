@echo off
setlocal EnableExtensions DisableDelayedExpansion
rem 65001 = UTF-8。本文件以 UTF-8 保存，不切码页中文会显示成乱码。
chcp 65001 >nul 2>nul
title DNF90CN 启动器

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
echo 正在更新本机运行时，请稍候...
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
echo   [无法启动] 这个目录里的服务端是旧版本，不能用来测试。
echo.
echo   原因：runtime\bin 里的程序比当前版本旧，本机又没有装 Go，无法重新编译。
echo         强行启动会重现已经修好的 BUG，所以这里直接停下。
echo.
echo   怎么办：请找发包的人要一份新的完整发布包（一个几百 MB 的 ZIP），
echo           解压到一个全新的文件夹，再双击里面的 LOGIN.bat。
echo.
echo   注意：从 GitHub 下载的源码 ZIP（文件夹名带 -main）不能直接玩，
echo         里面不含数据库和游戏资源，必须用完整发布包。
pause
exit /b 10

:runtime_incomplete
echo.
echo   [无法启动] 这个文件夹不是完整的游戏包。
echo.
echo   缺少 runtime\bin 里的四个程序：
echo     DNF90Control.exe  DNF90Doctor.exe  DNF90Launcher.exe  DNF90Server.exe
echo.
echo   最常见的原因：你是从 GitHub 点 "Download ZIP" 下载的源码
echo   （解压出来的文件夹名字带 -main）。那个包里没有游戏程序、
echo   没有数据库、也没有游戏资源，**不管怎么操作都跑不起来**。
echo.
echo   怎么办：请找发包的人要一份完整发布包（一个几百 MB 的 ZIP），
echo           解压到一个全新的文件夹，再双击里面的 LOGIN.bat。
pause
exit /b 12

:version_source_failed
echo.
echo   [无法启动] 缺少 deploy\windows\runtime.version 文件。
echo   这个文件夹不是完整的游戏包，请重新解压一份完整发布包。
pause
exit /b 11

:stop_failed
echo.
echo   [无法启动] 上一次的服务端还没有停干净，不能覆盖正在使用的程序。
echo   请先重启电脑，或者双击 STOP.bat 之后再试一次。
pause
exit /b %DNF90_STOP_EXIT%

:build_failed
echo.
echo   [无法启动] 编译登录程序失败。
echo   请把上面的报错内容截图发给开发者。
pause
exit /b %DNF90_BUILD_EXIT%

:launch
if not exist "%DNF90_LAUNCHER%" goto runtime_incomplete
start "" "%DNF90_LAUNCHER%"
exit /b 0
