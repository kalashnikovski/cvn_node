@echo off
title Administrator: Covenant Standard Layer-1 Node Miner Launcher
color 0A
cls

echo ====================================================================
echo 💎 COVENANT STANDARD CVN LEDGER SYNCHRONIZATION MATRIX CORE
echo ====================================================================
echo.

echo 📡 [1/2] Preparing network socket channels safely...
rem ✅ DETOXIFIED: Removed taskkill commands to prevent breaking active onboarding states mid-launch!

echo ⚙️ [2/3] Extracting local identity token variables dynamically...
if not exist miner_config.json (
    echo {"miner_address":"CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

set LOCAL_RIG_ID=
for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
    set tmp_id=%%a
)
set LOCAL_RIG_ID=%tmp_id:"=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%

if "%LOCAL_RIG_ID%"=="" (
    set LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID
)

echo 🚀 [3/3] Igniting un-throttled Proof-of-Diligence (PoD) hashing engine...
echo --------------------------------------------------------------------
echo ACTIVE RIG IDENTITY: %LOCAL_RIG_ID%
echo.

rem ✅ FIXED: Executes the compiled binary directly from your root project space to unchain the screen logger instantly!
if exist ".\cvn_node.exe" (
    .\cvn_node.exe --miner-address %LOCAL_RIG_ID%
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe --miner-address %LOCAL_RIG_ID%
) else (
    echo 🚨 CRITICAL ERROR: cvn_node.exe core binary was not found inside your workspace directory!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    pause
)