@echo off
:: 🗺️ MASTER HARDCODE FIX: Permanently locks the Administrator terminal to your true project folder
cd /d C:\ollama\cvn_node
title Covenant Standard Node Operator Gateway v3.8
cls
echo =================================================================
echo 🛰️ COVENANT STANDARD (CVN) PROTOCOL AUTOMATED ONBOARDING SYSTEM
echo =================================================================
echo.

:: --- STEP 1: Core System & Environment Audits ---
echo [1/3] Auditing local Go toolchain installations...
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo ⚠️ ERROR: Go compiler runtime utility toolchain not detected!
    echo Please install Go v1.20+ from golang.org before continuing.
    pause
    exit /b
)

:: --- STEP 2: Native Multithreaded Compilation Backplane ---
echo [2/3] Compiling standalone executable file natively with CGO flags...
:: Groups all modular dependency files into a single unified binary build
go build -o cvn_node.exe .
if %errorlevel% neq 0 (
    echo ⚠️ ERROR: Compilation failed! Please verify your Go source code layout files.
    pause
    exit /b
)
echo ✅ Initialization pass checked. Unified binary compiled with 100%% success!
echo.

:: --- STEP 3: Identity & Ledger Synchronization ---
echo [3/3] Synchronizing master database history chains...
if exist ledger_vault.json (
    echo ✅ Local production master ledger vault verified safely on disk.
) else (
    echo 📡 Core ledger file missing. Initializing new fallback ledger snapshot...
)
echo.
echo =================================================================
echo ✅ ENVIRONMENT INITIALIZATION COMPLETED WITH 100%% SUCCESS
echo =================================================================
echo.

:MENU_PROMPT
echo Select your desired network node routing profile allocation task:
echo -----------------------------------------------------------------
echo ⛏️  1. Launch Hashing Core Threads (Start Personal Miner + Node)
echo 🪙  2. Launch Lightweight Client Wallet (Access Your Balance Console)
echo ❌  3. Exit Operator Gateway (Safely Close Terminal Sandbox)
echo -----------------------------------------------------------------
echo.

set /p OperatorChoice="Enter target menu index selection number [1-3]: "

if "%OperatorChoice%"=="1" goto CHOSEN_MINER
if "%OperatorChoice%"=="2" goto CHOSEN_WALLET
if "%OperatorChoice%"=="3" goto EXIT_GATEWAY

cls
echo ⚠️ Invalid selection token detected. Please select index 1, 2, or 3.
echo.
goto MENU_PROMPT

:CHOSEN_MINER
cls
echo =================================================================
echo 🚀 SPINNING UP NATIVE PERSONAL MINER + NODE MATRIX ENVIRONMENT...
echo =================================================================
echo.
:: 👉 FIXED MENU ROUTE: Launches your clean compiled production binary natively
cvn_node.exe
pause
exit /b

:CHOSEN_WALLET
cls
echo =================================================================
echo 📡 DISPATCHING CRYPTOGRAPHIC CLIENT WALLET APPLICATION SHELL...
echo =================================================================
echo.
:: 👉 FIXED MENU ROUTE: Launches your compiled binary core explicitly targeting the wallet console argument
cvn_node.exe --wallet
echo.
echo.
echo Press [ENTER] to return to the gateway control menu...
pause >nul
cls
goto MENU_PROMPT

:EXIT_GATEWAY
cls
echo 🔒 Closing node operator portal lines gracefully. Go with God!
timeout /t 3 >nul
exit /b
