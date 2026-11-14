@echo off
rem Opens the CVN wallet on the CVN Testnet. It is LaunchWallet.bat with the
rem network chosen for you.
call "%~dp0LaunchWallet.bat" --network cvn_testnet %*
