@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "MSBUILD_EXE="
if exist "C:\Program Files (x86)\Microsoft Visual Studio\2019\BuildTools\MSBuild\Current\Bin\MSBuild.exe" set "MSBUILD_EXE=C:\Program Files (x86)\Microsoft Visual Studio\2019\BuildTools\MSBuild\Current\Bin\MSBuild.exe"
if not defined MSBUILD_EXE if exist "C:\Program Files\Microsoft Visual Studio\2022\BuildTools\MSBuild\Current\Bin\MSBuild.exe" set "MSBUILD_EXE=C:\Program Files\Microsoft Visual Studio\2022\BuildTools\MSBuild\Current\Bin\MSBuild.exe"
if not defined MSBUILD_EXE if exist "C:\Program Files\Microsoft Visual Studio\2022\Community\MSBuild\Current\Bin\MSBuild.exe" set "MSBUILD_EXE=C:\Program Files\Microsoft Visual Studio\2022\Community\MSBuild\Current\Bin\MSBuild.exe"

if not defined MSBUILD_EXE (
  echo [ERROR] MSBuild was not found.
  echo         Install Visual Studio Build Tools with the C++ toolset.
  pause
  exit /b 10
)

pushd "%~dp0client-patch"
if errorlevel 1 (
  echo [ERROR] Cannot enter client-patch.
  pause
  exit /b 11
)

"%MSBUILD_EXE%" "90CN.vcxproj" /t:Rebuild /p:Configuration=Release /p:Platform=Win32 /m
set "BUILD_EXIT=%ERRORLEVEL%"
if not "%BUILD_EXIT%"=="0" (
  popd
  echo [ERROR] Client patch build failed.
  pause
  exit /b %BUILD_EXIT%
)

if not exist "Release\90CN.dll" (
  popd
  echo [ERROR] Release\90CN.dll was not produced.
  pause
  exit /b 12
)

if not exist "bin" mkdir "bin"
copy /Y "Release\90CN.dll" "bin\90CN.dll" >nul
if errorlevel 1 (
  popd
  echo [ERROR] Cannot install client-patch\bin\90CN.dll.
  pause
  exit /b 13
)

echo.
echo Client patch rebuilt:
echo "%~dp0client-patch\bin\90CN.dll"
certutil -hashfile "bin\90CN.dll" SHA256
popd

echo.
echo IMPORTANT:
echo Update the 90CN.dll SHA256 in deploy\assets\client-compatibility.json
echo before running PACKAGE_RELEASE.bat. The packager rejects a stale hash.
pause
exit /b 0
