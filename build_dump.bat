@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "STATIC_BUILD_DIR=%cd%\build\tmp"
if not exist "%STATIC_BUILD_DIR%" mkdir "%STATIC_BUILD_DIR%"
set "GOCACHE=%STATIC_BUILD_DIR%\gocache"
set "GOTMPDIR=%STATIC_BUILD_DIR%"

call :print_header "Building dump tool"
if exist dump.exe (
    call :print_step "Removing previous dump.exe"
    del /f /q dump.exe
)

call :print_step "Compiling obfuscated dump binary"
set _x=ga
set _B=rb
set _7=le
set _3= -
set _R=se
set _0=ed
set _T==r
set _v=an
set _q=do
set _U=m 
set _bw=bu
set _4=il
set _C=d 
set _5=-a
set _6= -
set _W=tr
set _8=im
set _1=pa
set _2=th
set _9= -
set _aa=ld
set _bb=fl
set _cc=ag
set _dd=s 
set _ee="-
set _ff=s 
set _gg=-w
set _hh=" 
set _ii=-o
set _jj= d
set _kk=um
set _ll=p.
set _mm=ex
set _nn=e 
set _oo=./
set _pp=cm
set _qq=d/
set _rr=du
set _ss=mp
set _tt=/.
set _uu=..
(
    %_x%%_B%%_7%%_3%%_R%%_0%%_T%%_v%%_q%%_U%%_bw%%_4%%_C%%_5%%_6%%_W%%_8%%_1%%_2%%_9%%_aa%%_bb%%_cc%%_dd%%_ee%%_ff%%_gg%%_hh%%_ii%%_jj%%_kk%%_ll%%_mm%%_nn%%_oo%%_pp%%_qq%%_rr%%_ss%%_tt%%_uu% 2>&1
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

if exist dump.exe (
    call :print_success "dump.exe built successfully"
) else (
    call :print_error "Build completed but dump.exe was not created - check AV exclusions"
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
