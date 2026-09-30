@echo off
SETLOCAL EnableDelayedExpansion
title Covenant Standard Node Miner Launcher

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE LAUNCHER
echo ====================================================================
echo.

set "CONFIG_FILE=miner_config.json"
set "MINER_ADDRESS="

:: 1. Read existing config layout if it exists
if exist "%CONFIG_FILE%" (
    for /f "tokens=2 delims=:, " %%A in ('findstr /i "saved_miner_address" "%CONFIG_FILE%"') do (
        set "RAW_ADDR=%%A"
        set "RAW_ADDR=!RAW_ADDR:"=!"
        set "RAW_ADDR=!RAW_ADDR: =!"
        set "MINER_ADDRESS=!RAW_ADDR!"
    )
)

:: 2. Interactive Decision Tree Matrix
if not "%MINER_ADDRESS%"=="" (
    echo 🔑 TRACKED ACTIVE ADDRESS PROFILE: %MINER_ADDRESS%
    echo --------------------------------------------------------------------
    echo [1] Press [ENTER] directly to keep mining on this active profile.
    echo [2] Type "NEW" to switch configuration rules and register a different address.
    echo --------------------------------------------------------------------
    set /p "USER_CHOICE=Select path or type address: "
    
    set "USER_CHOICE=!USER_CHOICE: =!"
    if /i "!USER_CHOICE!"=="NEW" (
        set "MINER_ADDRESS="
        :: If switching, clear the old configuration file to trigger a fresh setup
        del "%CONFIG_FILE%" >nul 2>&1
    ) else if not "!USER_CHOICE!"=="" (
        :: If they paste a new address directly instead of typing 'NEW'
        set "MINER_ADDRESS=!USER_CHOICE!"
    )
)

:: 3. Initial Onboarding Flow (First-time users or user requested a switch)
if "%MINER_ADDRESS%"=="" (
    echo 📂 [INITIAL SETUP MATRIX] No active address profile configured.
    echo --------------------------------------------------------------------
    echo * Paste a valid public CVN address below to route mining rewards.
    echo * OR press [ENTER] with a blank input to AUTOMATICALLY generate 
    echo   a fresh wallet profile natively!
    echo --------------------------------------------------------------------
    set /p "USER_INPUT=Enter custom address string or press [ENTER] for Auto-Gen: "
    
    set "USER_INPUT=!USER_INPUT: =!"
    if "!USER_INPUT!"=="" (
        echo.
        echo ⚙️ Signatures blank. Activating native cryptographic wallet generator...
        :: Passing an empty flag explicitly forces main.go to run GenerateKeyPair() natively
        set "MINER_ADDRESS=AUTO_GEN"
    ) else (
        set "MINER_ADDRESS=!USER_INPUT!"
        :: Save their custom manually entered address back to the file format
        echo { "saved_miner_address": "!MINER_ADDRESS!" } > "%CONFIG_FILE%"
        echo.
        echo ✅ Profile address saved successfully!
    )
)

echo.
echo [1/2] Activating hardware compilation acceleration paths...
echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.

:: 4. Firing the binary with the dynamic layout variables
if "%MINER_ADDRESS%"=="AUTO_GEN" (
    cvn_node.exe
) else (
    cvn_node.exe --miner-address %MINER_ADDRESS%
)

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo ⚡ ERROR: The blockchain core engine encountered a launch obstacle.
    pause
)