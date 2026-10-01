@echo off
title Covenant Standard Wallet GUI Launcher
color 0B
echo ??? Initializing Outbound Cryptographic UTXO Wallet Interface...
cvn_node.exe --wallet
if %ERRORLEVEL% NEQ 0 ( pause )
