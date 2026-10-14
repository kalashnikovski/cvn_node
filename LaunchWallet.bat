@echo off
title Covenant Standard Wallet GUI Launcher
color 0B
cls

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) DESKTOP WALLET INTERFACE LAUNCHER
echo ====================================================================
echo.
echo 📡 [1/2] Preparing network interface socket lines safely...

cd /d "%~dp0"

echo 🎨 [2/2] Launching Cryptographic UTXO Wallet Interface matrix...
echo --------------------------------------------------------------------

if exist ".\cvn_node.exe" (
    :: 👑 THE FIX: Launches your desktop app in its own completely isolated thread window!
    start "Covenant Standard Wallet" ".\cvn_node.exe"
    echo ✨ Success: Wallet interface process detached cleanly from local console tracks.
    timeout /t 2 >nul
) else if exist ".\build\bin\cvn_node.exe" (
    start "Covenant Standard Wallet" ".\build\bin\cvn_node.exe"
    echo ✨ Success: Build wallet interface process detached cleanly from local console tracks.
    timeout /t 2 >nul
) else (
    color 0C
    echo 🚨 CRITICAL ERROR: cvn_node.exe core binary was not found inside your workspace directory!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    echo.
    pause
)
