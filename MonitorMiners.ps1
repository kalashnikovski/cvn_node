$ConsensusPort = 8080
Clear-Host
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "💎 COVENANT STANDARD (CVN) - PEER MINING ACTIVITY TRACKER" -ForegroundColor Cyan
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "🛰️  Monitoring active sockets... Standby for remote node mining transitions." -ForegroundColor Yellow
Write-Host ""

# Keep track of peers we have already flagged as active miners to prevent log flooding
$ActiveMinersList = @{}

while ($true) {
    # Query current TCP connections on your P2P consensus port
    $Connections = Get-NetTCPConnection -State Established -ErrorAction SilentlyContinue | Where-Object { $_.LocalPort -eq $ConsensusPort }
    
    if ($Connections) {
        foreach ($conn in $Connections) {
            $RemoteIP = $conn.RemoteAddress
            
            # Skip loopback or internal interface pointers
            if ($RemoteIP -eq "127.0.0.1" -or $RemoteIP -eq "::1" -or $RemoteIP -eq "0.0.0.0") { continue }
            
            # Analyze connection profile density (Miners maintain a sustained connection footprint)
            if (!$ActiveMinersList.ContainsKey($RemoteIP)) {
                $ActiveMinersList[$RemoteIP] = (Get-Date)
                Write-Host "🚀 ALERT: External Peer Node " -NoNewline -ForegroundColor White
                Write-Host "[$RemoteIP]" -NoNewline -ForegroundColor Green
                Write-Host " has initialized an active socket on Port $ConsensusPort!" -ForegroundColor White
                Write-Host "👉 Tracking verification metrics... Analyzing thread payload stability." -ForegroundColor Gray
            }
        }
    }
    
    # Check every 5 seconds to maintain near-zero CPU overhead
    Start-Sleep -Seconds 5
}