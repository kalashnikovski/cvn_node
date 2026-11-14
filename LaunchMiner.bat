@echo off
rem Starts the CVN miner. Anything you add after the file name is passed on to
rem the miner, for example:   LaunchMiner.bat --threads 8
title Covenant Standard (CVN) Miner
cd /d "%~dp0"

set "MINER=%~dp0cvn_miner.exe"
set "CONFIRMED=miner_address_confirmed.txt"
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

rem Once you have confirmed the saved address is yours, you are not asked again.
if exist "%CONFIRMED%" goto START_MINER

if not exist "miner_config.json" goto NO_WALLET
findstr /c:"CVN_" "miner_config.json" >nul
if errorlevel 1 goto NO_WALLET

rem A mining address is already saved here. It may have come with the
rem download and belong to someone else, so ask before mining to it.
cls
echo ====================================================================
echo  COVENANT STANDARD (CVN) - CHECK YOUR MINING ADDRESS
echo ====================================================================
echo.
echo  This folder already has a mining address saved in miner_config.json:
echo.
findstr /c:"CVN_" "miner_config.json"
echo.
echo  Everything you mine is paid to that address. Only mine to it if it
echo  is YOUR wallet and you have its private key written down.
echo.
echo  [1] Yes, that is my wallet. Mine to it.
echo  [2] No. Create a new wallet for me.
echo  [3] Exit
echo.
choice /c 123 /n /m " Choose 1, 2 or 3: "
if errorlevel 3 exit /b 0
if errorlevel 2 goto CREATE_WALLET
goto CONFIRM

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

:CREATE_WALLET
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

:CONFIRM
echo The mining address in miner_config.json was confirmed on this machine.> "%CONFIRMED%"

:START_MINER
"%MINER%" %*
if errorlevel 1 pause
