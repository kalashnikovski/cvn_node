@echo off
title Covenant Standard Node Package Dependency Installer (CVN)
color 0B
cls

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) - FRONTEND DESKTOP DEPENDENCY MATRIX
echo ====================================================================
echo.
echo 🔍 [ENVIRONMENT CHECK] Verifying node runtime environments on disk...

:: Adapt execution scope dynamically to whatever folder path holds this script
cd /d "%~dp0"

:: Use a string-safe inline conditional trap to avoid multi-line path parenthesis parsing crashes
where node >nul 2>nul || (
    echo 🚨 CRITICAL ERROR: Node.js was not detected on this machine!
    echo.
    echo 📥 Please install Node.js v26.11.1 or later before proceeding.
    echo 👉 Download Link: https://nodejs.org
    echo.
    pause
    exit /b
)

:: Secure variables strictly into isolated execution lines
for /f "delims=" %%i in ('node -v 2^>nul') do set "NODE_VER=%%i"
for /f "delims=" %%i in ('npm -v 2^>nul') do set "NPM_VER=%%i"

echo ✅ Found Node.js Runtime Engine: %NODE_VER%
echo ✅ Found Node Package Manager (NPM): v%NPM_VER%
echo --------------------------------------------------------------------

if not exist ".\frontend" (
    echo 🚨 FOLDER MISMATCH: Core ".\frontend" directory structure was not found!
    echo Ensure this script remains situated inside your \cvn_node project workspace.
    pause
    exit /b
)

echo 📂 Descending straight into local front-end layout folder tracks...
cd .\frontend

echo ⚡ [NPM PASS] Pulling missing node package wheels down over the wire...
echo This process registers Svelte/Vite UI assets. Please wait a brief moment...
echo --------------------------------------------------------------------

:: Call using a safe legacy peer dependency flag directly to clear out frontend package tree conflicts
call npm install --no-audit --no-fund --legacy-peer-deps

if %ERRORLEVEL% NEQ 0 (
	echo.
	echo 🚨 BUILD BREAKDOWN: NPM package asset download pass failed or dropped out!
	echo Check your internet connection configuration parameters and try again.
	cd /d "%~dp0"
	pause
	exit /b
)

echo.
echo ====================================================================
echo ✨ [P2P SUITE READY] Frontend Node dependencies successfully locked on disk!
echo ====================================================================
echo.
echo You can now safely close this window and run the build pass.
echo.
cd /d "%~dp0"
pause