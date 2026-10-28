@echo off
setlocal EnableExtensions
REM ============================================================
REM  Build script: compile agentService into agentService(.exe)
REM
REM  Source : %~dp0agentService   (module my.favorite/cclaw)
REM  Output : %~dp0bin\agentService.exe
REM  Config : copied to %~dp0bin\config\ (exe loads it from there)
REM  Usage  : double-click, or run "build_agentService.bat"
REM
REM  Note   : -trimpath strips local absolute paths from the
REM           binary; -ldflags "-s -w" strips symbols to reduce
REM           size. Remove them if you need a debuggable binary.
REM ============================================================

REM Treat the directory of this script as the SRC root.
set "SRC_ROOT=%~dp0"

cd /d "%SRC_ROOT%" || (
    echo [ERROR] Cannot enter directory: %SRC_ROOT%
    exit /b 1
)

REM Check that go is available on PATH.
where go >nul 2>nul
if errorlevel 1 (
    echo [ERROR] "go" was not found. Install Go and add it to PATH.
    exit /b 1
)

REM Output directory.
set "OUT_DIR=%SRC_ROOT%bin"
if not exist "%OUT_DIR%" mkdir "%OUT_DIR%"

REM Pure-Go build, no C toolchain required.
set "CGO_ENABLED=0"

echo [1/3] Building agentService ...
pushd "%SRC_ROOT%agentService"
go build -trimpath -ldflags "-s -w" -o "%OUT_DIR%\agentService.exe" .
set "BUILD_EXIT=%ERRORLEVEL%"
popd

if not "%BUILD_EXIT%"=="0" (
    echo [ERROR] Build failed with exit code %BUILD_EXIT%
    exit /b %BUILD_EXIT%
)

REM Copy config files into bin\config so the exe can load them.
echo [2/3] Copying config ...
if not exist "%OUT_DIR%\config" mkdir "%OUT_DIR%\config"
copy /y "%SRC_ROOT%agentService\config\*.ini" "%OUT_DIR%\config\" >nul

echo [3/3] Done: %OUT_DIR%\agentService.exe
exit /b 0
