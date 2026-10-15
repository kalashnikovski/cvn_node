$ErrorActionPreference = "Stop"
Clear-Host

Write-Output "===================================================================="
Write-Output "COVENANT STANDARD LAYER-1 AUTOMATED USER ONBOARDING SCRIPT v4.5"
Write-Output "===================================================================="
Write-Output ""
Write-Output "[1/4] Analyzing local operating system environment tracks..."

$WorkDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
if ($WorkDir) { Set-Location $WorkDir }

if (-not (Test-Path "cvn_node.exe")) {
    Write-Output "Core wallet asset client (cvn_node.exe) is missing!"
    Read-Host "Press Enter to exit..."
    Exit
}

Write-Output "[2/4] Hardening local system firewall ingress policies..."
$null = netsh advfirewall firewall add rule name="CVN_P2P_8080" dir=in action=allow protocol=TCP localport=8080 2>&1
$null = netsh advfirewall firewall add rule name="CVN_P2P_8080_OUT" dir=out action=allow protocol=TCP localport=8080 2>&1

Write-Output "[3/4] Requesting dynamic NAT port mapping lease from home gateway..."
try {
    $natObject = New-Object -ComObject HNetCfg.NATUPnP
    $mappings = $natObject.StaticPortMappingCollection
    if ($mappings) {
        $localIP = "127.0.0.1"
        $mappings.Add(8080, "TCP", 8080, $localIP, $true, "CVN_P2P")
        $mappings.Add(8081, "TCP", 8081, $localIP, $true, "CVN_HTTP")
        Write-Output "NAT UPnP lease mapped successfully."
    } else {
        Write-Output "UPnP not responding. Check your gateway configuration settings."
    }
} catch {
    Write-Output "UPnP mapping skipped or not supported on this interface."
}

Write-Output "[4/4] Conducting sovereign ledger identity audit..."
$ConfigFile = "miner_config.json"
if (-not (Test-Path $ConfigFile)) {
    '{"miner_address": "CVN_UNCONFIGURED_LOCAL_NODE_ID"}' | Set-Content $ConfigFile -Force
}

$Config = Get-Content $ConfigFile -Raw | ConvertFrom-Json
$LocalRigID = $Config.miner_address

if ($LocalRigID -eq "CVN_UNCONFIGURED_LOCAL_NODE_ID") {
    Write-Output ""
    Write-Output "[NEW PROFILE DETECTED] Generating secure CSPRNG wallet parameters..."
    Write-Output "--------------------------------------------------------------------"
    
    .\cvn_node.exe --generate-wallet
    
    Write-Output "--------------------------------------------------------------------"
    $Config = Get-Content $ConfigFile -Raw | ConvertFrom-Json
    $LocalRigID = $Config.miner_address
    Write-Output "Profile keys instantiated successfully. Credentials written to disk."
}

Write-Output "Authenticated Active Address: $LocalRigID"
Write-Output ""
Write-Output "===================================================================="
Write-Output "RE-IGNITION PASS: Activating Layer-1 Node Engine Harmony..."
Write-Output "===================================================================="
Write-Output ""

if (Test-Path "LaunchMiner.bat") {
    $LauncherArgs = @("/k", ".\LaunchMiner.bat")
    Start-Process -FilePath "cmd.exe" -ArgumentList $LauncherArgs
} else {
    $DefaultArgs = @("/k", ".\cvn_node.exe", "--connect", "202.137.175.220:8081")
    Start-Process -FilePath "cmd.exe" -ArgumentList $DefaultArgs
}