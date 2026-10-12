@echo off
title Covenant Standard Node Onboarding Daemon (CVN)
cls

:: FORCE WORKING DIRECTORY CONTEXT LOCK
cd /d "%~dp0"

echo ====================================================================
echo Covenant Standard Layer-1 Onboarding Matrix
echo ====================================================================
echo.
echo [SYSTEM STATUS] Analyzing system runtime environment tracks...

where go >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo Error: The Go Programming Language runtime engine was not found!
    pause
    exit /b
)

echo [FIREWALL CONFIG] Optimizing local network security policies...
netsh advfirewall firewall add rule name="CVN_P2P_8080" dir=in action=allow protocol=TCP localport=8080 >nul 2>&1
netsh advfirewall firewall add rule name="CVN_P2P_8080_OUT" dir=out action=allow protocol=TCP localport=8080 >nul 2>&1

echo [NAT TRAVERSAL] Requesting dynamic port map lease from Eero gateway via UPnP...
powershell -Command "$u=New-Object -ComObject HNetCfg.NATUPnP; $m=$u.StaticPortMappingCollection; if($m){$m.Add(8080,'TCP',8080,'127.0.0.1',$true,'CVN_P2P'); $m.Add(8081,'TCP',8081,'127.0.0.1',$true,'CVN_HTTP')}else{Write-Host 'UPnP not responding. Ensure UPnP is enabled inside your Eero app settings.'}"

echo [PROFILE AUDIT] Verifying local user deployment ledger states...
if not exist miner_config.json (
    echo {"miner_address": "CVN_UNCONFIGURED_LOCAL_NODE_ID"} > miner_config.json
)

for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
    set tmp_id=%%a
)
set LOCAL_RIG_ID=%tmp_id:"=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%
set LOCAL_RIG_ID=%LOCAL_RIG_ID: =%

if "%LOCAL_RIG_ID%" == "CVN_UNCONFIGURED_LOCAL_NODE_ID" (
    echo New installation profile detected. Generating local keys...
    
    if not exist cvn_node.exe (
        if exist go.mod del /f /q go.mod
        if exist go.sum del /f /q go.sum
        go mod init cvn_node
        go get github.com/wailsapp/wails/v2
        go get go.etcd.io/bbolt@v1.5.0
        go mod tidy
        go build -o cvn_node.exe .
    )
    
    echo.
    .\cvn_node.exe --generate-wallet
    echo.
    
    for /f "tokens=2 delims=:, " %%a in ('findstr "miner_address" miner_config.json') do (
        set tmp_id=%%a
    )
    set LOCAL_RIG_ID=%%a
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:"=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:}=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID:]=%
    set LOCAL_RIG_ID=%LOCAL_RIG_ID: =%
) else (
    echo Authenticated Address: %LOCAL_RIG_ID%
)

if exist cvn_node.exe goto LAUNCH_NODE

if exist go.mod del /f /q go.mod
if exist go.sum del /f /q go.sum
go mod init cvn_node
go get github.com/wailsapp/wails/v2
go get go.etcd.io/bbolt@v1.5.0
go mod tidy
go build -o cvn_node.exe .

if %ERRORLEVEL% NEQ 0 (
    echo Critical fault: Node core compilation aborted.
    pause
    exit /b
)

:LAUNCH_NODE
echo.
echo Launching Covenant Standard Blockchain Consensus Miner Engine...
echo.

if exist LaunchMiner.bat (
    start "Covenant Standard Hashing Engine" cmd /k ".\LaunchMiner.bat"
) else (
    start "Covenant Standard Hashing Engine" cmd /k ".\cvn_node.exe --connect 202.137.175.220:8081"
)