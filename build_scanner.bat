@echo off
setlocal enabledelayedexpansion

:: ── Scanner Build Script ─────────────────────────────────────────────
:: Follows the same conventions as better_build.bat:
::   - garble obfuscation with random seed
::   - trimpath, static tags, stripped symbols
::   - per-build noise generation
::   - static build folder (AV-friendly)
::   - GUID-named output (or custom name)

set GOGARBLE=*

:: Required versions (same as better_build.bat)
set REQUIRED_GO_VERSION=1.24
set REQUIRED_GARBLE_VERSION=0.14.2

cd /d "%~dp0"

set "STATIC_BUILD_DIR=%cd%\build\tmp"
if not exist "%STATIC_BUILD_DIR%" mkdir "%STATIC_BUILD_DIR%"
set "GOCACHE=%STATIC_BUILD_DIR%\gocache"
set "GOTMPDIR=%STATIC_BUILD_DIR%"

call :print_header "D2R Pattern Scanner — Build"

:: ── Pre-flight checks ────────────────────────────────────────────────
where go >nul 2>&1
if %errorlevel% neq 0 (
    call :print_error "Go is not installed or not in PATH"
    call :pause_and_exit 1
)
where garble >nul 2>&1
if %errorlevel% neq 0 (
    call :print_error "Garble is not installed. Run: go install mvdan.cc/garble@v%REQUIRED_GARBLE_VERSION%"
    call :pause_and_exit 1
)
call :print_success "Go and Garble found"

:: ── Generate build noise ─────────────────────────────────────────────
call :print_step "Generating per-build noise..."
powershell -ExecutionPolicy Bypass -File "%~dp0generate_noise.ps1"

:: ── Build identifiers ────────────────────────────────────────────────
for /f "delims=" %%a in ('powershell -Command "[guid]::NewGuid().ToString()"') do set "BUILD_ID=%%a"
for /f "delims=" %%b in ('powershell -Command "Get-Date -Format 'o'"') do set "BUILD_TIME=%%b"

if "%1"=="" (
    set "OUTPUT_EXE=build\scanner-%BUILD_ID%.exe"
) else (
    set "OUTPUT_EXE=build\%1.exe"
)

:: ── Compile ──────────────────────────────────────────────────────────
call :print_step "Compiling obfuscated scanner..."
call :print_info "Output: %OUTPUT_EXE%"

(
    garble -seed=random build -a -trimpath -tags static --ldflags "-s -w" -o "%OUTPUT_EXE%" ./cmd/scanner 2>&1
) > garble_scanner.log
set "GARBLE_EXIT_CODE=!errorlevel!"

if !GARBLE_EXIT_CODE! neq 0 (
    call :print_error "Build failed. Garble log:"
    for /f "usebackq delims=" %%l in (`type garble_scanner.log`) do (
        call :print_error "%%l"
    )
    del garble_scanner.log
    call :pause_and_exit 1
)
del garble_scanner.log

:: Clean up temp build folder
if exist "%STATIC_BUILD_DIR%" (
    call :print_step "Cleaning up temporary build folder"
    rmdir /s /q "%STATIC_BUILD_DIR%"
)

:: ── Verify output ────────────────────────────────────────────────────
if exist "%OUTPUT_EXE%" (
    call :print_success "Built: %OUTPUT_EXE%"
) else (
    call :print_error "Executable was not created"
    call :print_info "Check your AV exclusion list for this folder"
    call :pause_and_exit 1
)

call :print_header "Build Complete"
call :print_info "Usage: %OUTPUT_EXE%"
call :print_info "  -core       Scan only core d2go offsets"
call :print_info "  -json       Output as JSON"
call :print_info "  -verify     Validate resolved addresses"
call :print_info "  -pid N      Attach to a specific PID"

echo.
powershell -Command "Write-Host 'Press any key to exit...' -ForegroundColor Yellow"
pause > nul
exit /b 0

:: ── Helpers (same style as better_build.bat) ─────────────────────────
:print_header
echo.
powershell -Command "Write-Host '=== %~1 ===' -ForegroundColor Magenta"
echo.
goto :eof

:print_step
powershell -Command "Write-Host '  - %~1' -ForegroundColor Cyan"
goto :eof

:print_success
powershell -Command "Write-Host '    SUCCESS: %~1' -ForegroundColor Green"
goto :eof

:print_error
powershell -Command "Write-Host '    ERROR: %~1' -ForegroundColor Red"
goto :eof

:print_info
powershell -Command "Write-Host '    INFO: %~1' -ForegroundColor Yellow"
goto :eof

:pause_and_exit
echo.
powershell -Command "Write-Host 'Press any key to exit...' -ForegroundColor Yellow"
pause > nul
exit %1
