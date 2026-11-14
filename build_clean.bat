@echo off
rem Builds both CVN programs from source:
rem   cvn_miner.exe   the node and miner (also the text wallet: --wallet)
rem   cvn_node.exe    the desktop wallet
rem Stop the miner before running this; Windows cannot replace a running program.
title Covenant Standard (CVN) Build
cd /d "%~dp0"

if exist "cvn_node.exe" del /f /q "cvn_node.exe"
if exist "cvn_miner.exe" del /f /q "cvn_miner.exe"

echo [1/2] Building the node and miner (cvn_miner.exe)...
go build -tags server -o cvn_miner.exe .
set "MINER_BUILD_RESULT=%ERRORLEVEL%"

echo.
echo [2/2] Building the desktop wallet (cvn_node.exe)...
rem The wallet window needs the "desktop,production" tags; without them the
rem program is built with no window. "-H windowsgui" stops an empty console
rem window from opening behind the wallet.
go build -tags desktop,production -ldflags "-H windowsgui" -o cvn_node.exe .
set "WALLET_BUILD_RESULT=%ERRORLEVEL%"

echo.
if "%MINER_BUILD_RESULT%%WALLET_BUILD_RESULT%"=="00" (
    echo Build finished. Start mining with LaunchMiner.bat.
    timeout /t 2 >nul
) else (
    echo The build failed. Read the messages above for the cause.
    pause
)
