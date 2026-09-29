@echo off
title Covenant Standard Wallet Bootstrapper v2.1.1
color 0E
echo ==================================================================
echo 💎 INITIALIZING COVENANT STANDARD WALLET DEPLOYMENT INSTANCE 💎
echo ==================================================================
echo.
echo [1/3] Securing execution directory path targeting...
cd /d "%~dp0"

echo [2/3] Configuring native hardware acceleration parameters...
set CGO_ENABLED=1

echo [3/3] Compiling and rendering graphical interface layout tiers...
echo.
go run . --wallet

if %errorlevel% neq 0 (
    echo.
    echo ⚠️ ERROR: The wallet compiler suite hit an obstacle. 
    echo Please ensure your background mining node has released file locks.
    pause
