@echo off
title Covenant Standard Node Miner Rig v3.0.1
color 0B
echo ==================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE 💎
echo ==================================================================
echo.
echo [1/3] Navigating to system repository environment...
cd /d "%~dp0"

echo [2/3] Activating hardware compilation acceleration paths...
set CGO_ENABLED=1

echo [3/3] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.
go run .

if %errorlevel% neq 0 (
    echo.
    echo 🚨 ERROR: The blockchain core engine encountered a launch obstacle.
    echo Please verify that your local Go toolchain is properly installed.
)
echo.
echo ⏸️ Terminal held open for diagnostic inspection.
pause