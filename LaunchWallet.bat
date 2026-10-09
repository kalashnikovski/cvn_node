@echo off
title Covenant Standard Wallet GUI Launcher
color 0B
cls

echo 📡 [1/2] Releasing background network adapters and flushing sockets...
taskkill /F /IM cvn_node.exe /T >nul 2>nul
powershell -Command "Stop-Process -Name cvn_node -Force -ErrorAction SilentlyContinue" >nul 2>nul

echo 🎨 [2/2] Launching Cryptographic UTXO Wallet Interface matrix...
echo --------------------------------------------------------------------

rem ✅ FIXED: Points directly to the authentic Wails binary output directory path!
if exist ".\build\bin\cvn_node.exe" (
    ".\build\bin\cvn_node.exe" --wallet
) else if exist ".\cvn_node.exe" (
    ".\cvn_node.exe" --wallet
) else (
    echo 🚨 CRITICAL ERROR: cvn_node.exe was not found inside .\build\bin\ or the root workspace folder!
    echo Please make sure to run 'wails build' to bake your production executable asset.
)

if %ERRORLEVEL% NEQ 0 ( 
    echo.
    echo 🚨 Alert: Interface closed unexpectedly or required binary elements are out of sync.
    pause 
)