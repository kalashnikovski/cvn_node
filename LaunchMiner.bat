@echo off
title Covenant Standard Hashing Engine
color 0A
cls

echo ====================================================================
echo 💎 COVENANT STANDARD CVN LEDGER SYNCHRONIZATION MATRIX CORE
echo ====================================================================
echo.

echo 📡 [1/3] Preparing network socket channels safely...
cd /d "%~dp0"

echo ⚙️  [2/3] Verifying environment profile configurations...
setlocal enabledelayedexpansion

:: If the configuration file or keys are missing, drop directly into the selection menu
if not exist "miner_config.json" (
    goto WALLET_INTERACTIVE_MENU
)
if not exist "cvn_secret_backup.txt" (
    goto WALLET_INTERACTIVE_MENU
)

:: 👑 HARDENED RAW VALUE STRING EXTRACTOR GATE:
set "LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID"
for /f "usebackq tokens=*" %%A in ("miner_config.json") do (
    set "line=%%A"
    set "line=!line:"=!"
    set "line=!line:{=!"
    set "line=!line:}=!"
    set "line=!line:,=!"
    
    for /f "tokens=1,2 delims=:" %%B in ("!line!") do (
        set "key=%%B"
        set "val=%%C"
        
        :: Clean the key spaces cleanly
        if not "!key!"=="" (
            for /f "tokens=*" %%X in ("!key!") do set "key=%%X"
        )
        
        if "!key!"=="miner_address" (
            :: 👑 INTENSE LEADING SPACE SHREDDER:
            :: Forces the batch pipeline to explicitly strip all leading json
            :: indentation spaces, matching your raw coin identity string down to the bit!
            for /f "tokens=*" %%Y in ("!val!") do set "LOCAL_RIG_ID=%%Y"
        )
    )
)

if "!LOCAL_RIG_ID!"=="CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    goto WALLET_INTERACTIVE_MENU
)
if "!LOCAL_RIG_ID!"=="" (
    goto WALLET_INTERACTIVE_MENU
)

goto IGNITE_MINER_CORE

:WALLET_INTERACTIVE_MENU
cls
color 0B
echo ====================================================================
echo 👤 COVENANT STANDARD ONBOARDING SELECTION GATEWAY
echo ====================================================================
echo.
echo [1] Create a Brand New Cryptographic Wallet Identity Profile
echo [2] Launch Automated Onboarding Autopilot Script
echo [3] Exit Launchpad
echo.
echo --------------------------------------------------------------------
choice /c 123 /n /m "👉 Select an onboarding option to establish rig identity [1-3]: "
set CHOICE_RESULT=%ERRORLEVEL%

if "%CHOICE_RESULT%"=="1" goto GENERATE_NEW_KEYS
if "%CHOICE_RESULT%"=="2" goto RUN_AUTOPILOT
if "%CHOICE_RESULT%"=="3" exit

:GENERATE_NEW_KEYS
cls
color 0A
echo ====================================================================
echo 🔑 GENERATING LAYER-1 CRYPTOGRAPHIC WALLET PARAMETERS
echo ====================================================================
echo.

if exist ".\cvn_miner.exe" (
    .\cvn_miner.exe --generate-wallet
) else if exist ".\cvn_node.exe" (
    .\cvn_node.exe --generate-wallet
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe --generate-wallet
) else (
    color 0C
    echo 🚨 CRITICAL ERROR: Core engine binary executable was not found!
    echo Cannot compile secure address keys. Run build_clean.bat first.
    pause
    goto WALLET_INTERACTIVE_MENU
)

echo.
echo --------------------------------------------------------------------
echo 🔐 ATTENTION: Your Private Keys have been saved to cvn_secret_backup.txt
echo 💾 Your Public Wallet Address has been configured inside miner_config.json
echo --------------------------------------------------------------------
echo.
echo Please copy your keys securely now before initializing mining worker threads.
echo.
pause
goto RELOAD_AND_LAUNCH

:RUN_AUTOPILOT
cls
color 0B
echo ⚡ Shifting execution over to your Onboarding Autopilot script...
timeout /t 2 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File ".\AutoOnboard_And_Mine.ps1"
goto END_LAUNCHPAD

:RELOAD_AND_LAUNCH
set "LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID"
for /f "usebackq tokens=*" %%A in ("miner_config.json") do (
    set "line=%%A"
    set "line=!line:"=!"
    set "line=!line:{=!"
    set "line=!line:}=!"
    set "line=!line:,=!"
    for /f "tokens=1,2 delims=:" %%B in ("!line!") do (
        set "key=%%B"
        set "val=%%C"
        if not "!key!"=="" (
            for /f "tokens=*" %%X in ("!key!") do set "key=%%X"
        )
        if "!key!"=="miner_address" (
            :: 👑 SANITIZE RELOAD STREAM: Strip trailing spaces from the freshly written file
            for /f "tokens=*" %%Y in ("!val!") do set "LOCAL_RIG_ID=%%Y"
        )
    )
)

:IGNITE_MINER_CORE
cls
color 0A
echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 NODE MINER MASTER DASHBOARD
echo ====================================================================
echo.
echo 🚀 [3/3] Igniting un-throttled Proof-of-Diligence (PoD) hashing engine...
echo --------------------------------------------------------------------
echo ACTIVE RIG IDENTITY: !LOCAL_RIG_ID!
echo --------------------------------------------------------------------
echo.

set "ROUTING_ARGS=%*"
if "!ROUTING_ARGS!"=="" (
    set "ROUTING_ARGS=--connect 202.137.175.220:8081"
)

if exist ".\cvn_miner.exe" (
    .\cvn_miner.exe --miner-address !LOCAL_RIG_ID! !ROUTING_ARGS!
) else if exist ".\cvn_node.exe" (
    .\cvn_node.exe --miner-address !LOCAL_RIG_ID! !ROUTING_ARGS!
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe --miner-address !LOCAL_RIG_ID! !ROUTING_ARGS!
) else (
    color 0C
    echo 🚨 CRITICAL ERROR: Core engine binary executable was not found!
    echo Please compile your project targets or run your onboarding script first.
    pause
)
goto END_LAUNCHPAD

:END_LAUNCHPAD
endlocal