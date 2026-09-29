```batch
@echo off
title Covenant Standard Node Miner Rig v4.0.0
color 0B
echo ==================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE 💎
echo ==================================================================
echo.

:: Secure navigation bounds path context
cd /d "%~dp0"

:: 💰 INTERACTIVE PROMPT LAYER
set /p MINER_ADDR="Enter the public CVN address to receive mining rewards: "

if "%MINER_ADDR%"=="" (
    echo.
    echo ⚠️ WARNING: No address provided. Defaulting to legacy system node...
    set MINER_ADDR=Nikola_Global_Network_Node
)

echo.
echo [1/2] Activating hardware compilation acceleration paths...
set CGO_ENABLED=1

echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.
go run . --miner-address %MINER_ADDR%

if %errorlevel% neq 0 (
    echo.
    echo 🚨 ERROR: The blockchain core engine encountered a launch obstacle.
    pause
)