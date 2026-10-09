@echo off
title Covenant Standard Node Onboarding Daemon (CVN)
cls
echo ====================================================================
echo ?? COVENANT STANDARD (CVN) LAYER-1 AUTOMATED NODE ONBOARDING MATRIX
echo ====================================================================
echo.
echo [SYSTEM STATUS] Analyzing system runtime environment tracks...
where go >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo ?? ERROR: The Go Programming Language runtime engine was not found!
    pause
    exit /b
)
if exist cvn_node.exe (
    echo ? SUCCESS: Valid cvn_node.exe found. Skipping compilation pass.
    goto LAUNCH_NODE
)
echo ?? WARNING: Production core binary asset missing. Compiling...
if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum
go mod init cvn_node
go get github.com/wailsapp/wails/v2
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
go build -o cvn_node.exe main.go app.go blockchain_core.go gossip_mesh.go database_core.go backup_vault.go
if %ERRORLEVEL% NEQ 0 (
    echo ?? CRITICAL FAULT: Node core compilation aborted.
    pause
    exit /b
)
:LAUNCH_NODE
echo.
echo ====================================================================
echo ?? STARTING COVENANT STANDARD BLOCKCHAIN CONSENSUS MINER ENGINE...
echo ====================================================================
echo.
if not exist miner_config.json (
    echo {"miner_address": "CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)
if exist LaunchMiner.bat (
    call .\LaunchMiner.bat
) else (
    .\cvn_node.exe --connect 207.148.67.11:8081
)
