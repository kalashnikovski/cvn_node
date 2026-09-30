@echo off
SETLOCAL EnableDelayedExpansion
title Covenant Standard Node Miner Launcher

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE LAUNCHER
echo ====================================================================
echo.

set "CONFIG_FILE=miner_config.json"
set "CHOSEN_ADDRESS="

:: 1. If an identity file exists, alert the operator and provide options
if exist "%CONFIG_FILE%" (
    echo 🔑 Pre-existing wallet identity configuration profile detected.
    echo --------------------------------------------------------------------
    echo * Press [ENTER] directly to keep mining on your pre-loaded profile.
    echo * Type "NEW" to clear this profile and register a different address.
    echo --------------------------------------------------------------------
    set /p "USER_CHOICE=Select your path: "
    
    set "USER_CHOICE=!USER_CHOICE: =!"
    if /i "!USER_CHOICE!"=="NEW" (
        echo.
        echo 🧹 Clearing local profile configurations...
        del "%CONFIG_FILE%" >nul 2>&1
    ) else if not "!USER_CHOICE!"=="" (
        :: If they typed or pasted a specific address directly into the prompt
        set "CHOSEN_ADDRESS=!USER_CHOICE!"
    )
)

echo.
echo [1/2] Activating hardware compilation acceleration paths...
echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.

:: 2. Fire execution parameters safely based on the operator's choice
if not "!CHOSEN_ADDRESS!"=="" (
    cvn_node.exe --miner-address !CHOSEN_ADDRESS!
) else (
    :: If they hit Enter, let Go handles reading or auto-generating profiles natively!
    cvn_node.exe
)

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo ⚡ ERROR: The blockchain core engine encountered a launch obstacle.
    pause
)