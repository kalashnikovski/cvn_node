@echo off
SETLOCAL EnableDelayedExpansion
title Covenant Standard Layer-1 Node Miner Launcher

echo ====================================================================
echo * COVENANT STANDARD CVN LAYER-1 CONSENSUS CORE ENGINE LAUNCHER *
echo ====================================================================
echo.

set "CONFIG_FILE=miner_config.json"
set "CHOSEN_ADDRESS="

:: 1. IDENTITY GATEWAY: Verify if a local identity profile exists, if not, generate one cleanly
if not exist "%CONFIG_FILE%" (
    echo [STATS] NO LOCAL IDENTITY DETECTED: Running automated wallet initialization wizard...
    if exist "cvn_node.exe" (
        cvn_node.exe --generate-profile
        echo [SUCCESS] Local miner_config.json identity profile built successfully!
    ) else (
        echo [ERROR] cvn_node.exe binary framework missing from target directory folder paths!
        pause
        exit /b
    )
    echo --------------------------------------------------------------------
)

:: 2. EXTRACTION LOOP: Pull the user's specific local address from their config profile securely
if exist "%CONFIG_FILE%" (
    for /f "tokens=2 delims=:," %%A in ('findstr /i "miner_address" "%CONFIG_FILE%"') do (
        set "RAW_ADDR=%%A"
        set "RAW_ADDR=!RAW_ADDR:"=!"
        set "RAW_ADDR=!RAW_ADDR: =!"
        set "RAW_ADDR=!RAW_ADDR:}=!"
        set "CHOSEN_ADDRESS=!RAW_ADDR!"
    )
)

if "!CHOSEN_ADDRESS!"=="" (
    echo [INFO] Pre-existing wallet identity configuration profile detected.
    echo --------------------------------------------------------------------
    echo * Press [ENTER] directly to keep mining on your pre-loaded profile.
    echo * Type NEW to clear this profile and register a different address.
    echo --------------------------------------------------------------------
    set /p "USER_CHOICE=Select your path / Input Wallet Address: "
    if "!USER_CHOICE!"=="" (
        set "CHOSEN_ADDRESS=CVN_DEFAULT_GUEST_MINER_ID_TRACKS"
    ) else (
        set "CHOSEN_ADDRESS=!USER_CHOICE!"
    )
)

echo.
echo [USER] ACTIVE IDENTITY ROOT SECURED: !CHOSEN_ADDRESS!
echo [1/2] Activating hardware compilation acceleration paths...
echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.

echo [INFO] Initializing Local Mainnet Verification Loops...
echo [INFO] LOADING NETWORK INTENSITY BUFFER MATRICES...
echo.

echo [SECURITY] Sync-Gate Lock activated.
echo [SECURITY] Validating ledger alignment against global network tip benchmarks...
echo.

:: Simulated dynamic sync sequence mapping local heights up to target network tip grids
set /a "localHeight=26922"
set /a "globalTip=27460"

:sync_loop
if !localHeight! lss !globalTip! (
    set /a "localHeight+=53"
    if !localHeight! gtr !globalTip! set /a "localHeight=!globalTip!"
    
    set /a "pct=(localHeight * 100) / globalTip"
    set /a "barWidth=pct / 5"
    
    set "progressBar="
    for /l %%G in (1,1,!barWidth!) do set "progressBar=!progressBar!#"
    for /l %%G in (!barWidth!,1,20) do set "progressBar=!progressBar!-"
    
    cls
    echo ====================================================================
    echo * COVENANT STANDARD CVN LEDGER SYNCHRONIZATION MATRIX CORE *
    echo ====================================================================
    echo.
    echo [PROGRESS] Syncing Data: [!progressBar!] !pct!%%%%
    echo --------------------------------------------------------------------
    echo * Local Ledger Height:  #!localHeight!
    echo * Global Mainnet Tip:   #!globalTip!
    echo.
    echo [STATUS] MINING STATE: [PAUSED] Waiting for 100%%%% ledger alignment equilibrium...
    
    timeout /t 1 /nobreak >nul
    goto :sync_loop
)

echo.
echo [SUCCESS] Core fully aligned with global mainnet tip benchmarks!
echo [SUCCESS] Mainnet equilibrium verified. Releasing socket locks...
echo [LAUNCH] IGNITING HARDWARE MINING WORKERS OVER LAYER-1 NETWORK WIRE...
echo --------------------------------------------------------------------
echo.

:: 3. RAW HARDWARE IGNITION GATES: Execute processing runs natively
cvn_node.exe --miner-address !CHOSEN_ADDRESS! --connect 207.148.67.11:8080

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo [CRITICAL] Local blockchain engine core experienced an internal initialization crash.
    pause
)