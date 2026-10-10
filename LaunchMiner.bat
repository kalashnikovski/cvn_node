@echo off
title Administrator: Covenant Standard Layer-1 Node Miner Launcher
color 0A
cls

echo ====================================================================
echo 💎 COVENANT STANDARD CVN LEDGER SYNCHRONIZATION MATRIX CORE
echo ====================================================================
echo.

echo 📡 [1/2] Preparing network socket channels safely...
cd /d "%~dp0"

echo ⚙️ [2/3] Extracting local identity token variables dynamically...
if not exist miner_config.json (
    echo {"miner_address":"CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

rem ✅ FIXED: Enabled delayed expansion to support exclamation point string scrubbing variables safely
setlocal enabledelayedexpansion
set "LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID"

for /f "tokens=2 delims=:," %%A in (miner_config.json) do (
    set "val=%%A"
    set "val=!val: =!"
    set "val=!val:"=!"
    set "val=!val:{=!"
    set "val=!val:}=!"
    set "val=!val:[=!"
    set "val=!val:]=!"
    set "LOCAL_RIG_ID=!val!"
)

if "!LOCAL_RIG_ID!"=="" (
    set "LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID"
)

echo 🚀 [3/3] Igniting un-throttled Proof-of-Diligence (PoD) hashing engine...
echo --------------------------------------------------------------------
echo ACTIVE RIG IDENTITY: !LOCAL_RIG_ID!
echo.

E:
cd \cvn_node

if exist ".\cvn_node.exe" (
    .\cvn_node.exe --miner-address !LOCAL_RIG_ID!
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe --miner-address !LOCAL_RIG_ID!
) else (
    echo 🚨 CRITICAL ERROR: cvn_node.exe core binary was not found inside your workspace directory!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    pause
)
endlocal