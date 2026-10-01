@echo off
title Covenant Standard Client Wallet UI
cls
echo =================================================================
echo 🪙 INITIALIZING GRAPHICAL CLIENT WALLET APPLICATION MATRIX...
echo =================================================================
echo.
cd /d "C:\ollama\cvn_node"

:: Isolated build pass: main.go is stripped to ensure wallet GUI compilation succeeds flat
go run wallet.go structures.go crypto_auth.go read_ledger.go security_harness.go backup_vault.go --wallet

if %errorlevel% neq 0 (
    echo.
    echo ⚠️ ERROR: The graphical window closed or hit an environment exception.
    echo Verify your Fyne GUI dependencies are mapped correctly.
    echo.
    pause
)
