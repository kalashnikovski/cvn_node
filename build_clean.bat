@echo off
title Covenant Standard Native Core Compiler Pass

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) FIXED ARCHITECTURE COMPILER DAEMON
echo ====================================================================
echo.

:: 1. Clear out any residual, corrupted module metadata files
if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum

echo [1/4] Initializing clean module namespace registry path...
go mod init cvn_node

echo [2/4] Hard-locking uncensored Wails core framework bindings...
go get github.com/wailsapp/wails/v2

echo [3/4] Pulling BoltDB mainnet ledger database wheels...
go get go.etcd.io/bbolt@v1.5.0

echo [4/4] Aligning dependencies and verifying structural hashes...
go mod tidy

echo.
echo 🚀 ROLLING TELEMETRY COMPILE PASS IGNITED...
echo --------------------------------------------------------------------
go build -o cvn_node.exe main.go app.go blockchain_core.go gossip_mesh.go database_core.go backup_vault.go

if %ERRORLEVEL% EQU 0 (
    echo.
    echo ✅ SUCCESS: cvn_node.exe compiled with zero syntax errors!
) else (
    echo.
    echo 🚨 ERROR: Compilation pass aborted due to internal syntax boundaries.
)
pause