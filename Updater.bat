@echo off
setlocal enabledelayedexpansion

:: Set current directory to script location
cd /d "%~dp0"

echo -----------------------------------
echo [0/2] Checking Requirements...
echo -----------------------------------

where git >nul 2>nul
if errorlevel 1 (
    echo ERROR: Git is not installed.
    pause
    exit /b
)
echo Git is installed.

echo.
echo -----------------------------------
echo [1/2] Updating Repository...
echo -----------------------------------

:: Ensure upstream remote points to Diobyte/koolo
git remote get-url upstream >nul 2>nul
if errorlevel 1 (
    echo Adding upstream remote for Diobyte/koolo...
    git remote add upstream https://github.com/Diobyte/koolo.git
) else (
    for /f "delims=" %%u in ('git remote get-url upstream') do set "UPSTREAM_URL=%%u"
    echo !UPSTREAM_URL! | findstr /i "Diobyte/koolo" >nul
    if errorlevel 1 (
        echo Updating upstream remote to Diobyte/koolo...
        git remote set-url upstream https://github.com/Diobyte/koolo.git
    )
)

echo Fetching latest from Diobyte/koolo...
git fetch upstream main

if errorlevel 1 (
    echo.
    echo ERROR: Git fetch from upstream failed.
    pause
    exit /b
)

echo Cleaning untracked/generated files before merge...
git clean -fd

echo Merging upstream/main...
git merge upstream/main --no-edit

if errorlevel 1 (
    echo.
    echo WARNING: Merge failed. Resetting to upstream/main...
    git merge --abort 2>nul
    git clean -fd
    git reset --hard upstream/main
    git clean -fd
)

echo.
echo -----------------------------------
echo Managing Old Executables...
echo -----------------------------------

:: Define paths
set "BUILD_DIR=%~dp0build"
set "OLD_DIR=%BUILD_DIR%\old_versions"

if exist "%BUILD_DIR%\*.exe" (
    if not exist "%OLD_DIR%" (
        echo Creating directory: %OLD_DIR%
        mkdir "%OLD_DIR%"
    )
    
    echo Moving old .exe to old_versions folder...
    move /y "%BUILD_DIR%\*.exe" "%OLD_DIR%\"
) else (
    echo No old .exe found in build folder. Skipping move.
)

echo.
echo -----------------------------------
echo [2/2] Starting better_build.bat...
echo (Auto-answering "n" to config prompt)
echo -----------------------------------

if not exist "better_build.bat" (
    echo ERROR: better_build.bat not found!
    pause
    exit /b
)

(echo n) | call better_build.bat

echo.
echo -----------------------------------
echo Update, Move, and Build process complete!
echo.
pause