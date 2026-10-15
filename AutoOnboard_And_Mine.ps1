Write-Output "Authenticated Active Address: $LocalRigID"
Write-Output ""
Write-Output "===================================================================="
Write-Output "RE-IGNITION PASS: Activating Layer-1 Node Engine Harmony..."
Write-Output "===================================================================="
Write-Output ""

# 👑 THE IN-BOUND CONNECTIONS REPAIR:
# Ensure that every single newly spawned node client explicitly inherits your 
# Melbourne Master network sync gateway coordinate arrays upon initial boot!
$MasterSeedCoordinate = "202.137.175.220:8081"

if (Test-Path "LaunchMiner.bat") {
    # Dynamically inject the master connection flag directly into the command shell session wrapper
    $LauncherArgs = @("/k", ".\LaunchMiner.bat", "--connect", $MasterSeedCoordinate)
    Start-Process -FilePath "cmd.exe" -ArgumentList $LauncherArgs
} else {
    $DefaultArgs = @("/k", ".\cvn_node.exe", "--miner-address", $LocalRigID, "--connect", $MasterSeedCoordinate)
    Start-Process -FilePath "cmd.exe" -ArgumentList $DefaultArgs
}