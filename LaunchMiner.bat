@echo off
rem Starts the CVN miner. Anything you add after the file name is passed on to
rem the miner, for example:   LaunchMiner.bat --threads 8
title Covenant Standard (CVN) Miner
cd /d "%~dp0"

set "MINER=cvn_miner.exe"
if not exist "%MINER%" (
    echo.
    echo  cvn_miner.exe was not found in this folder.
    echo  Run build_clean.bat first, or download a release package.
    echo.
    pause
    exit /b 1
)

rem A mining address given on the command line needs no saved wallet.
echo %* | findstr /i /c:"--miner-address" >nul
if not errorlevel 1 goto START_MINER

if exist "miner_config.json" (
    findstr /c:"CVN_" "miner_config.json" >nul
    if not errorlevel 1 goto START_MINER
)

:NO_WALLET
cls
echo ====================================================================
echo  COVENANT STANDARD (CVN) - THIS MACHINE HAS NO MINING ADDRESS YET
echo ====================================================================
echo.
echo  [1] Create a new wallet and mine to it
echo  [2] Exit
echo.
echo  To mine to a wallet you already have, close this window and run:
echo      LaunchMiner.bat --miner-address CVN_your_address
echo.
choice /c 12 /n /m " Choose 1 or 2: "
if errorlevel 2 exit /b 0

echo.
"%MINER%" --generate-wallet
if errorlevel 1 (
    pause
    exit /b 1
)
echo.
echo  WRITE THE PRIVATE KEY DOWN NOW. It is not saved anywhere, and without
echo  it the coins you mine can never be spent.
echo.
pause

:START_MINER
"%MINER%" %*
if errorlevel 1 pause
