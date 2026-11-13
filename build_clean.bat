@echo off
title Covenant Standard Production Compiler Toolchain
color 0A
cls

echo ====================================================================
echo 🔨 BAKING COVENANT STANDARD LAYER-1 DUAL-ENGINE ECOSYSTEM...
echo ====================================================================
echo.

cd /d "%~dp0"

:: Clear out lingering assets
if exist ".\cvn_node.exe" del /f /q ".\cvn_node.exe"
if exist ".\cvn_miner.exe" del /f /q ".\cvn_miner.exe"

echo 📦 [1/2] Baking Headless Consensus Hashing Core (cvn_miner.exe)...
:: 👑 THE FIXED: Forcefully compiles the binary as a true Console sub-system utility so text streams roll alive!
go build -tags server -o cvn_miner.exe .
set "MINER_BUILD_RESULT=%ERRORLEVEL%"

echo.
echo 🎨 [2/2] Baking Graphical Desktop Wallet Client (cvn_node.exe)...
:: The wallet window needs the "desktop,production" tags. Without them the
:: program is built with no window at all. "-H windowsgui" stops an empty
:: console window from opening behind the wallet.
go build -tags desktop,production -ldflags "-H windowsgui" -o cvn_node.exe .
set "WALLET_BUILD_RESULT=%ERRORLEVEL%"

if "%MINER_BUILD_RESULT%%WALLET_BUILD_RESULT%"=="00" (
    echo.
    echo ====================================================================
    echo ✅ PRODUCTION MASTER PLATFORM SUCCESSFUL: Dual suites locked on disk!
    echo ====================================================================
    timeout /t 2 >nul
) else (
    color 0C
    echo.
    echo 🚨 ERROR: The build failed. Read the messages above for the cause.
    pause
)