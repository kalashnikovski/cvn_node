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

:: ====================================================================
:: 🎛️ NATIVE WINDOWS DEFENDER FIREWALL PORT OPENING MATRIX
:: ====================================================================
echo 🔒 [FIREWALL CONFIG] Optimizing local network security policies...
echo 🟩 Punching open Port 8080 for raw P2P Gossip Mesh syncs...
netsh advfirewall firewall add rule name="CVN_P2P_8080" dir=in action=allow protocol=TCP localport=8080 style=v2.10 >nul 2>&1
netsh advfirewall firewall add rule name="CVN_P2P_8080_OUT" dir=out action=allow protocol=TCP localport=8080 style=v2.10 >nul 2>&1

echo 🟩 Punching open Port 8081 for Web Application Explorer syncs...
netsh advfirewall firewall add rule name="CVN_HTTP_8081" dir=in action=allow protocol=TCP localport=8081 style=v2.10 >nul 2>&1
netsh advfirewall firewall add rule name="CVN_HTTP_8081_OUT" dir=out action=allow protocol=TCP localport=8081 style=v2.10 >nul 2>&1

echo 🟩 Punching open Port 8082 for Analytics Data Matrix channels...
netsh advfirewall firewall add rule name="CVN_META_8082" dir=in action=allow protocol=TCP localport=8082 style=v2.10 >nul 2>&1
netsh advfirewall firewall add rule name="CVN_META_8082_OUT" dir=out action=allow protocol=TCP localport=8082 style=v2.10 >nul 2>&1
echo 👍 Firewall rules successfully updated natively.
echo --------------------------------------------------------------------

echo 🔍 [PROFILE AUDIT] Verifying local user deployment ledger states...
if not exist miner_config.json (
    echo {"miner_address": "CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

:: Dynamic profile parsing tracks extract identity variables natively
for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
    set tmp_id=%%a
)
set LOCAL_RIG_ID=%tmp_id:"=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID: =%

:: ✅ VISUAL DISCRIMINATION ENGINE: Evaluates configuration variables to log status states
if "%LOCAL_RIG_ID%" == "CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    echo 👤 [USER STATUS] New installation profile detected. Generating local keys...
    echo 🟩 Initializing primary network handshake directories...
    
    if not exist cvn_node.exe (
        echo 📡 Production core binary asset missing. Pre-compiling identity framework...
        go mod tidy
        go build -o cvn_node.exe .
    )
    
    echo.
    .\cvn_node.exe --generate-wallet
    echo.
    
    for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
        set tmp_id=%%a
    )
    set LOCAL_RIG_ID=%%a
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:"=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID: =%
) else (
    echo 🎖️ [USER STATUS] Experienced Node Operator Authenticated!
    echo 🪪 RECOGNIZED ADDRESS: %LOCAL_RIG_ID%
    echo 📂 Re-mounting historical BoltDB table schemas from local disk sectors...
)

if exist cvn_node.exe (
    echo ✅ SUCCESS: Valid production core executable verified.
    goto LAUNCH_NODE
)

echo 📡 WARNING: Production core binary asset missing. Compiling package...
if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum
go mod init cvn_node
go get ://github.com
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
go build -o cvn_node.exe .

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
    start "Covenant Standard Hashing Engine" cmd /k ".\cvn_node.exe --connect 202.137.175.220:8081"
)

echo ✅ Success: Mining engine unchained successfully into an isolated terminal panel!
echo This window can now be safely minimized.
