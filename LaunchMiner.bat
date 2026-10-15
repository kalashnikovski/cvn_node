@echo off
title Covenant Standard Hashing Engine
color 0A
cls

echo ====================================================================
echo 💎 COVENANT STANDARD CVN LEDGER SYNCHRONIZATION MATRIX CORE
echo ====================================================================
echo.

echo 📡 [1/2] Preparing network socket channels safely...
cd /d "%~dp0"

echo ⚙️ [2/3] Extracting local identity token variables dynamically...
setlocal enabledelayedexpansion
set LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID

:: If the profile configuration doesn't exist at all, skip the parsing layout
if not exist "miner_config.json" (
    goto PROFILE_MISMATCH_TRIGGER
)

for /f "usebackq tokens=*" %%A in ("miner_config.json") do (
    set "line=%%A"
    set "line=!line: =!"
    set "line=!line:"=!"
    set "line=!line:{=!"
    set "line=!line:}=!"
    for /f "tokens=2 delims=:" %%B in ("!line!") do (
        set "LOCAL_RIG_ID=%%B"
    )
)

if "!LOCAL_RIG_ID!"=="CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    goto PROFILE_MISMATCH_TRIGGER
)

echo 🚀 [3/3] Igniting un-throttled Proof-of-Diligence (PoD) hashing engine...
echo --------------------------------------------------------------------
echo ACTIVE RIG IDENTITY: !LOCAL_RIG_ID!
echo --------------------------------------------------------------------
echo.

:: Establish fallback mainnet routing coordinates if no specific seed args were provided by PowerShell
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

:PROFILE_MISMATCH_TRIGGER
color 0B
echo 👤 [USER STATUS] Pristine installation path detected. 
echo ⚡ Automatically shifting execution over to your Onboarding Autopilot...
echo --------------------------------------------------------------------
timeout /t 2 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File ".\AutoOnboard_And_Mine.ps1"
goto END_LAUNCHPAD

:END_LAUNCHPAD
endlocal