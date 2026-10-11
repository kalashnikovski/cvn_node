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
:: 👑 THE FIXED: Hard-locks variable expansion inside an isolated environment block!
setlocal enabledelayedexpansion
set LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID

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

echo 🚀 [3/3] Igniting un-throttled Proof-of-Diligence (PoD) hashing engine...
echo --------------------------------------------------------------------
echo ACTIVE RIG IDENTITY: !LOCAL_RIG_ID!
echo --------------------------------------------------------------------
echo.

if exist ".\cvn_miner.exe" (
    .\cvn_miner.exe --miner-address !LOCAL_RIG_ID!
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe --miner-address !LOCAL_RIG_ID!
) else (
    color 0C
    echo 🚨 CRITICAL ERROR: cvn_miner.exe core engine binary was not found!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    pause
)
endlocal