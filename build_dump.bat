@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "STATIC_BUILD_DIR=%cd%\build\tmp"
if not exist "%STATIC_BUILD_DIR%" mkdir "%STATIC_BUILD_DIR%"
set "GOCACHE=%STATIC_BUILD_DIR%\gocache"
set "GOTMPDIR=%STATIC_BUILD_DIR%"

:: Generate a unique name for the dump binary
for /f "delims=" %%a in ('powershell "[guid]::NewGuid().ToString()"') do set "DUMP_ID=%%a"
set "OUTPUT_DUMP=%DUMP_ID%.exe"

call :print_header "Building dump tool"

call :print_step "Compiling obfuscated dump binary"
(
    garble -seed=random build -a -trimpath -ldflags "-s -w" -o "%OUTPUT_DUMP%" ./cmd/dump/... 2>&1
) > garble_dump.log
set "BUILD_EXIT=!errorlevel!"

if !BUILD_EXIT! neq 0 (
    call :print_error "Build failed. Log output:"
    for /f "usebackq delims=" %%l in (`type garble_dump.log`) do call :print_error "%%l"
    del garble_dump.log
    if exist "%STATIC_BUILD_DIR%" rmdir /s /q "%STATIC_BUILD_DIR%"
    pause & exit /b 1
)
del garble_dump.log

if exist "%STATIC_BUILD_DIR%" (
    call :print_step "Cleaning up temporary build folder"
    rmdir /s /q "%STATIC_BUILD_DIR%"
)

if exist "%OUTPUT_DUMP%" (
    call :print_success "Dump tool built successfully as: %OUTPUT_DUMP%"
) else (
    call :print_error "Build completed but dump binary was not created - check AV exclusions"
    pause & exit /b 1
)
exit /b 0

:print_header
powershell -Command "Write-Host '=== %~1 ===' -ForegroundColor Magenta"
goto :eof

:print_step
powershell -Command "Write-Host '  - %~1' -ForegroundColor Cyan"
goto :eof

:print_success
powershell -Command "Write-Host '  SUCCESS: %~1' -ForegroundColor Green"
goto :eof

:print_error
powershell -Command "Write-Host '  ERROR: %~1' -ForegroundColor Red"
goto :eof

:print_info
powershell -Command "Write-Host '  INFO: %~1' -ForegroundColor Yellow"
goto :eof
