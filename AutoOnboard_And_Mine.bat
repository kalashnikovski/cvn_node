@echo off
title Covenant Standard Node Onboarding Daemon (CVN)
cls
echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 AUTOMATED NODE ONBOARDING MATRIX
echo ====================================================================
echo.
echo [SYSTEM STATUS] Analyzing system runtime environment tracks...

where go >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo 🚨 ERROR: The Go Programming Language runtime engine was not found!
    pause
    exit /b
)

echo 🔍 [PROFILE AUDIT] Verifying local user deployment ledger states...

// ✅ FIXED: Explicitly target the script's local execution directory dynamically
cd /d "%~dp0"

if not exist miner_config.json (
    echo {"miner_address":"CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

set LOCAL_RIG_ID=CVN_UNCONFIGURED_LOCAL_NODE_ID
for /f "tokens=2 delims=:," %%A in (miner_config.json) do (
    set "val=%%A"
    set "val=!val: =!"
    set "val=!val:"=!"
    set "val=!val:{=!"
    set "val=!val:}=!"
    set "val=!val:[=!"
    set "val=!val:]=!"
)
set "LOCAL_RIG_ID=%val:"=%"
set "LOCAL_RIG_ID=%LOCAL_RIG_ID: =%"

if exist cvn_node.exe goto COMPILE_CHECK

echo 📡 WARNING: Production core binary asset missing. Compiling package...
if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum
go mod init cvn_node
go get ://github.com
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
go build -o cvn_node.exe main.go app.go blockchain_core.go gossip_mesh.go database_core.go backup_vault.go

if %ERRORLEVEL% NEQ 0 (
    echo 🚨 CRITICAL FAULT: Node core compilation aborted.
    pause
    exit /b
)

:COMPILE_CHECK
if "%LOCAL_RIG_ID%"=="CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    echo.
    echo 👤 [USER STATUS] New installation profile detected.
    echo 🟩 Initializing secure cryptographic identity generator...
    echo --------------------------------------------------------------------
    
    .\cvn_node.exe --generate-wallet
    
    if %ERRORLEVEL% NEQ 0 (
        echo 🚨 ERROR: Secure wallet initialization aborted.
        pause
        exit /b
    )
    
    echo.
    echo ⚠️  LOUD SECURITY WARNING: Your private keys have been exported to 'cvn_secret_backup.txt'.
    echo KEEP THIS FILE COMPLETELY SECRET. IF YOU LOSE IT, YOUR FUNDS ARE GONE FOREVER.
    echo --------------------------------------------------------------------
    echo ⏸️  Onboarding paused. Verify your credentials above before starting the node.
    pause
    
    set LOCAL_RIG_ID=
    for /f "tokens=2 delims=:," %%A in (miner_config.json) do (
        set "val=%%A"
        set "val=!val: =!"
        set "val=!val:"=!"
        set "val=!val:{=!"
        set "val=!val:}=!"
    )
    set "LOCAL_RIG_ID=%val:"=%"
    set "LOCAL_RIG_ID=%LOCAL_RIG_ID: =%"
) else (
    echo 🎖️ [USER STATUS] Experienced Node Operator Authenticated!
    echo 🪪 RECOGNIZED ADDRESS: %LOCAL_RIG_ID%
    echo 📂 Re-mounting historical BoltDB table schemas from local disk sectors...
)

:LAUNCH_NODE
echo.
echo ====================================================================
echo 🚀 UNCHAINING COVENANT STANDARD BLOCKCHAIN CONSENSUS MINER ENGINE...
echo ====================================================================
echo.

if exist LaunchMiner.bat (
    start "Covenant Standard Hashing Engine" cmd /k ".\LaunchMiner.bat"
) else (
    start "Covenant Standard Hashing Engine" cmd /k ".\cvn_node.exe --miner-address %LOCAL_RIG_ID%"
)

echo ✅ Success: Node network service and ledger verification engine ignited.
echo The active sync status progress bar is now updating live in your miner terminal.
echo This orchestrator window can now be safely closed.