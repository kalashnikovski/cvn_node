@echo off
SETLOCAL EnableDelayedExpansion
title Covenant Standard Node Miner Launcher

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE LAUNCHER
echo ====================================================================
echo.

set "CONFIG_FILE=miner_config.json"
set "CHOSEN_ADDRESS="

:: 1. If an identity file is missing, invoke the profile creator
if not exist "%CONFIG_FILE%" (
    echo 📡 NO PROFILE DETECTED: Running automated wizard...
    cvn_node.exe --generate-profile
    echo.
    echo ✅ miner_config.json profile built successfully!
    echo --------------------------------------------------------------------
)

:: 2. Provide runtime routing configuration selections
if exist "%CONFIG_FILE%" (
    echo 🔑 Pre-existing wallet identity configuration profile detected.
    echo --------------------------------------------------------------------
    echo * Press [ENTER] directly to keep mining on your pre-loaded profile.
    echo * Type NEW to clear this profile and register a different address.
    echo --------------------------------------------------------------------
    set /p "USER_CHOICE=Select your path: "
    
    if defined USER_CHOICE (
        set "USER_CHOICE=!USER_CHOICE: =!"
        if /i "!USER_CHOICE!"=="NEW" (
            echo.
            echo 🧹 Clearing local profile configurations...
            del "%CONFIG_FILE%" >nul 2>&1
            echo Profile cleared. Run LaunchMiner.bat again to generate a new identity.
            pause
            exit /b
        ) else (
            set "CHOSEN_ADDRESS=!USER_CHOICE!"
        )
    )
)

echo.
echo [1/2] Activating hardware compilation acceleration paths...
echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.

:: 3. Execute the node binary safely matching input choices with explicit cloud seed connections
if not "!CHOSEN_ADDRESS!"=="" (
    echo 📡 Attempting connection to Primary Singapore Cloud Hub...
    cvn_node.exe --miner-address !CHOSEN_ADDRESS! --connect 207.148.67.11:8080
    
    :: Catch Connection Error and Trigger Fallback to Melbourne Rig
    if %ERRORLEVEL% NEQ 0 (
        color 0E
        echo.
        echo ⚠️  WARNING: Primary Singapore Cloud Node unavailable or timed out.
        echo 🔄 Triggering automated redundancy fallback path...
        echo 🇦🇺 Connecting to Secondary Melbourne Anchor Rig Gateway...
        echo.
        timeout /t 3 >nul
        color 0B
        cvn_node.exe --miner-address !CHOSEN_ADDRESS! --connect 202.137.175.220:8080
    )
) else (
    echo 📡 Attempting connection to Primary Singapore Cloud Hub...
    cvn_node.exe --connect 207.148.67.11:8080
    
    :: Catch Connection Error and Trigger Fallback to Melbourne Rig
    if %ERRORLEVEL% NEQ 0 (
        color 0E
        echo.
        echo ⚠️  WARNING: Primary Singapore Cloud Node unavailable or timed out.
        echo 🔄 Triggering automated redundancy fallback path...
        echo 🇦🇺 Connecting to Secondary Melbourne Anchor Rig Gateway...
        echo.
        timeout /t 3 >nul
        color 0B
        cvn_node.exe --connect 202.137.175.220:8080
    )
)

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo 🚨 CRITICAL ERROR: Both Primary and Secondary bootstrap entry gateways are unreachable.
    pause
)