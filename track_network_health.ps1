# Covenant Standard Layer-1 Node Diagnostics Watchdog
$endpoint = "http://localhost:8081/req_chain"
$logFile = "C:\ollama\cvn_node\network_health_summary.log"

Clear-Host
Write-Host "====================================================" -ForegroundColor Cyan
Write-Host "COVENANT STANDARD (CVN) HARDWARE MONITOR ENGAGED" -ForegroundColor Cyan
Write-Host "====================================================`n" -ForegroundColor Cyan
Write-Host "Automating background network tracking logs. Polling port :8081 loop..." -ForegroundColor Yellow
Write-Host "[Live Metric Stream Active]" -ForegroundColor Magenta

$lastKnownHeight = -1

while ($true) {
    try {
        $response = Invoke-RestMethod -Uri $endpoint -Method Get -TimeoutSec 3
        $blocks = $response.blocks
        $circulatingSupply = $response.circulating_supply
        $currentHeight = 0
        
        if ($blocks -and $blocks.Count -gt 0) {
            $currentHeight = ($blocks | Measure-Object -Property index -Maximum).Maximum
        }
        
        $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
        
        if ($currentHeight -gt $lastKnownHeight) {
            if ($lastKnownHeight -ne -1) {
                Write-Host "BLOCK VALIDATED BY NETWORK MATRIX!" -ForegroundColor Cyan
            }
            $lastKnownHeight = $currentHeight
            
            $logEntry = "[$timestamp] BLOCK HEIGHT: #$currentHeight | ACTIVE SUPPLY: $circulatingSupply CVN | RIG STATUS: ONLINE"
            Write-Host $logEntry -ForegroundColor Green
            $logEntry | Out-File -FilePath $logFile -Append
        } else {
            Write-Host "Monitoring consensus loop at block #$currentHeight... No new blocks solved yet." -ForegroundColor Gray
        }
    }
    catch {
        $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
        $errorEntry = "[$timestamp] WARNING: Connection to local node backplane timed out. Port :8081 initializing..."
        Write-Host $errorEntry -ForegroundColor Yellow
        $errorEntry | Out-File -FilePath $logFile -Append
    }
    Start-Sleep -Seconds 5
}