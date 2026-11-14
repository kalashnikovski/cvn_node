@echo off
rem Starts the CVN miner on the CVN Testnet. It is LaunchMiner.bat with the
rem network chosen for you: started with a double-click, LaunchMiner.bat
rem itself runs on the first test network (testnet-1).
call "%~dp0LaunchMiner.bat" --network cvn_testnet %*
