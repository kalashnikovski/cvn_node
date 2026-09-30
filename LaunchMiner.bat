```batch
@echo off
title Covenant Standard Node Miner Rig v5.3.0
color 0B
echo ==================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 CONSENSUS CORE ENGINE 💎
echo ==================================================================
echo.

:: Secure workspace path alignment
cd /d "%~dp0"

:: Profile caching validation check pass
set PROFILE_FILE=miner_config.json
set PREFILL_ADDR=

if exist %PROFILE_FILE% (
    for /f "tokens=2 delims=: " %%a in ('findstr "saved_miner_address" %PROFILE_FILE%') do (
        set PREFILL_ADDR=%%a
    )
)

:: Strip un-needed json syntax brackets characters from variable registers
if not "%PREFILL_ADDR%"=="" (
    set PREFILL_ADDR=%PREFILL_ADDR:"=%
    set PREFILL_ADDR=%PREFILL_ADDR:,=%
    set PREFILL_ADDR=%PREFILL_ADDR: =%
)

if not "%PREFILL_ADDR%"=="" (
    echo [🔒 PROFILE PROFILE TRACKED]: Detected active previous mining session address handle!
    echo Pre-filled Address: %PREFILL_ADDR%
    echo.
    echo Press [ENTER] directly to instantly load this wallet profile.
    echo Or paste a distinct secure wallet address handle to register a new route.
    echo.
    set /p MINER_ADDR="Target Public Miner Address: "
) else (
    echo [📢 NEW OPERATOR MATRIX]: No historical profiles detected.
    echo Press [ENTER] directly on a blank prompt to auto-generate a fresh secure wallet profile.
    echo.
    set /p MINER_ADDR="Enter public CVN address: "
)

:: Re-route matching criteria states based on user volition entries
if "%MINER_ADDR%"=="" (
    if not "%PREFILL_ADDR%"=="" (
        set MINER_ADDR=%PREFILL_ADDR%
    ) else (
        set MINER_ADDR=Nikola_Global_Network_Node
    )
)

echo.
echo [1/2] Activating hardware compilation acceleration paths...
set CGO_ENABLED=1

echo [2/2] Launching Proof-of-Diligence (PoD) Mining Engine...
echo.
go run . --miner-address %MINER_ADDR%

if %errorlevel% neq 0 (
    echo.
    echo 🚨 ERROR: The blockchain core engine encountered a launch obstacle.
    pause
)