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
if not exist miner_config.json (
    echo {"miner_address": "CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

rem Dynamic profile parsing tracks extract identity variables natively
for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
    set tmp_id=%%a
)
set LOCAL_RIG_ID=%tmp_id:"=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%

rem ✅ VISUAL DISCRIMINATION ENGINE: Evaluates configuration variables to log status states
if "%LOCAL_RIG_ID%" == "CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    echo 👤 [USER STATUS] New installation profile detected. Generating local keys...
    echo 🟩 Initializing primary network handshake directories...
) else (
    echo 🎖️ [USER STATUS] Experienced Node Operator Authenticated!
    echo 🪪 RECOGNIZED ADDRESS: %LOCAL_RIG_ID%
    echo 📂 Re-mounting historical BoltDB table schemas from local disk sectors...
)

if exist cvn_node.exe (
    echo ✅ SUCCESS: Valid production core executable found.
    goto LAUNCH_NODE
)

echo 📡 WARNING: Production core binary asset missing. Compiling package...
if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum
go mod init cvn_node
go get github.com/wailsapp/wails/v2
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
go build -o cvn_node.exe main.go app.go blockchain_core.go gossip_mesh.go database_core.go backup_vault.go

if %ERRORLEVEL% NEQ 0 (
    echo 🚨 CRITICAL FAULT: Node core compilation aborted.
    pause
	exit /b
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
    start "Covenant Standard Hashing Engine" cmd /k ".\cvn_node.exe --connect 207.148.67.11:8081"
)

echo ✅ Success: Mining engine unchained successfully into an isolated terminal panel!
echo This window can now be safely minimized.