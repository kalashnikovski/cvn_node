@echo off
title Covenant Standard Node Package Dependency Installer (CVN)
color 0B
cls

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) - FRONTEND DESKTOP DEPENDENCY MATRIX
echo ====================================================================
echo.
echo 🔍 [ENVIRONMENT CHECK] Verifying node runtime environments on disk...

rem 1. Check if Node.js runtime engine exists in the system variables environment path
where node >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo 🚨 CRITICAL ERROR: Node.js was not detected on this machine!
    echo.
    echo 📥 Please install Node.js v26.7.0 and NPM v11.19.0 before proceeding.
    echo 👉 Download Link: https://nodejs.org
    echo.
    pause
    exit /b
)

rem 2. Extract and print the exact version slices currently active on the host machine
for /f "tokens=*" %%i in ('node -v') do set NODE_VER=%%i
for /f "tokens=*" %%i in ('npm -v') do set NPM_VER=%%i

echo ✅ Found Node.js Runtime Engine: %NODE_VER%
echo ✅ Found Node Package Manager (NPM): v%NPM_VER%
echo --------------------------------------------------------------------

rem 3. Audit for the vital local frontend source directory architecture map
if not exist ".\frontend" (
    echo 🚨 FOLDER MISMATCH: Core ".\frontend" directory structure was not found!
    echo Ensure this script is sitting inside your root \cvn_node project workspace.
    pause
    exit /b
)

echo 📂 Descending straight into local front-end layout folder tracks...
cd .\frontend

echo ⚡ [NPM PASS] Pulling missing node package wheels down over the wire...
echo This process registers Svelte/Vite UI assets. Please wait a brief moment...
echo --------------------------------------------------------------------

rem 4. Execute the automated clean package install pass natively inside the target lane
call npm install --no-audit --no-fund

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo 🚨 BUILD BREAKDOWN: NPM package asset download pass failed or dropped out!
    echo Check your internet connection configuration parameters and try again.
    cd ..
    pause
    exit /b
)

echo.
echo ====================================================================
echo ✨ [P2P SUITE READY] Frontend Node dependencies successfully locked on disk!
echo ====================================================================
echo.
echo You can now safely close this window and launch: .\LaunchWallet.bat
echo.
cd ..
pause