@echo off
rem For developers only. You do NOT need this to run the miner or the wallet:
rem the wallet's screens are already built and stored in frontend\dist.
rem
rem Run this after changing anything in frontend\src. It installs the tools
rem the wallet's screens are built with and rebuilds frontend\dist. Then run
rem build_clean.bat to put the new screens into cvn_node.exe.
title Covenant Standard (CVN) - Rebuild the wallet screens
cd /d "%~dp0"

where node >nul 2>nul
if errorlevel 1 (
    echo.
    echo  Node.js was not found. Install it from https://nodejs.org and run this again.
    echo.
    pause
    exit /b 1
)

if not exist "frontend\package.json" (
    echo.
    echo  The frontend folder was not found. Run this from the cvn_node folder.
    echo.
    pause
    exit /b 1
)

cd frontend

echo [1/2] Installing the build tools...
call npm install --no-audit --no-fund --legacy-peer-deps
if errorlevel 1 goto FAILED

echo.
echo [2/2] Building the wallet screens...
call npm run build
if errorlevel 1 goto FAILED

cd /d "%~dp0"
echo.
echo Done. Run build_clean.bat to build the wallet with the new screens.
pause
exit /b 0

:FAILED
cd /d "%~dp0"
echo.
echo That step failed. Read the messages above for the cause.
pause
exit /b 1
