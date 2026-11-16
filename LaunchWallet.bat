@echo off
rem Opens the CVN desktop wallet on the CVN Testnet. Anything you add after
rem the file name is passed on to the wallet. For another network:
rem     LaunchWallet.bat --network devnet
title Covenant Standard (CVN) Wallet
cd /d "%~dp0"

if exist "cvn_node.exe" (
    start "Covenant Standard Wallet" "cvn_node.exe" %*
) else (
    echo.
    echo  cvn_node.exe was not found in this folder.
    echo  Run build_clean.bat first, or download a release package.
    echo.
    echo  The text wallet needs no separate program:  cvn_miner.exe --wallet
    echo.
    pause
)
