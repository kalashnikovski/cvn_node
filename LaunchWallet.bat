@echo off
title Covenant Standard Wallet GUI Launcher
color 0B
cls

echo 📡 [1/2] Preparing network interface socket lines safely...

// ✅ FIXED: Switch runtime focus cleanly across drives using percent parameter operators
cd /d "%~dp0"

echo 🎨 [2/2] Launching Cryptographic UTXO Wallet Interface matrix...
echo --------------------------------------------------------------------

if exist ".\cvn_node.exe" (
    ".\cvn_node.exe"
) else if exist ".\build\bin\cvn_node.exe" (
    ".\build\bin\cvn_node.exe"
) else (
    echo 🚨 CRITICAL ERROR: cvn_node.exe core binary was not found inside your workspace directory!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    pause
)

if %ERRORLEVEL% NEQ 0 ( 
    echo.
    echo 🚨 Alert: Interface closed unexpectedly or required binary elements are out of sync.
    pause 
)