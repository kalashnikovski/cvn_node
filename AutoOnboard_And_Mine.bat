@echo off
title COVENANT STANDARD (CVN) PROTOCOL - CORE INITIALIZATION INTERFACE
color 0B
cls

echo ====================================================================
echo 💎  COVENANT STANDARD (CVN) NETWORK - ONE-CLICK NODE LAUNCHER
echo ====================================================================
echo.

:: Check if the compiled binary is present in the workspace directory
if not exist "cvn_node.exe" (
    color 0C
    echo 🚨 CRITICAL ERROR: cvn_node.exe binary was not found in this folder!
    echo Please ensure the pre-compiled executable is placed in this directory.
    echo.
    pause
    exit /b
)

:: Read configuration profile from miner_config.json if it exists
if exist "miner_config.json" (
    echo 📂 Found local profile layout on disk. Launching node engine automatically...
    echo 📡 Connecting to neutral Singapore Cloud Seed Node matrix...
    echo.
    timeout /t 2 >nul
    :: 🚀 FIXED: Route existing profiles straight to your cloud anchor seed node
    cvn_node.exe --connect 207.148.67.11:8080
    goto end
)

:: If no config profile exists, prompt for initialization
echo 🛰️  Welcome, Sovereign Peer! No local miner configuration profile detected.
echo.
echo [1] Start Core Node and generate a brand-new mining wallet address automatically.
echo [2] Start Core Node using an existing public CVN address.
echo.
set /p userchoice="Select initialization vector [1-2]: "

if "%userchoice%"=="1" (
    echo.
    echo 🔑 Initializing cryptographic key-generation sequence...
    cvn_node.exe --generate-profile
    echo.
    echo ✨ Profile config written successfully! Launching core node mining threads...
    timeout /t 3 >nul
    :: 🚀 FIXED: Route brand-new miners directly into your horizontal mesh
    cvn_node.exe --connect 207.148.67.11:8080
    goto end
)

if "%userchoice%"=="2" (
    echo.
    set /p inputaddr="Paste your public wallet address (Format: CVN_...): "
    if "%inputaddr%"=="" goto invalid
    
    echo.
    echo 💾 Locking target address and booting mesh node...
    :: 🚀 FIXED: Bind their pasted address and synchronize their ledger height from the seed node instantly
    cvn_node.exe --miner-address %inputaddr% --connect 207.148.67.11:8080
    goto end
)

:invalid
color 0C
echo 🚨 Error: Invalid or null address entered. Boot sequence aborted.
pause
exit /b

:end
pause