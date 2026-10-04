$CorePorts = @(8080, 8081)
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "💎 COVENANT STANDARD (CVN) MESH NETWORK - ACTIVE INBOUND PEERS AUDIT" -ForegroundColor Cyan
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host ""

$Connections = Get-NetTCPConnection -State Established -ErrorAction SilentlyContinue | Where-Object { $CorePorts -contains $_.LocalPort }

if ($Connections) {
    $Connections | ForEach-Object {
        $GeoData = "Unregistered Remote Peer Node"
        Write-Host "📡 Connect Vector: " -NoNewline -ForegroundColor White
        Write-Host "$($_.RemoteAddress):$($_.RemotePort)" -NoNewline -ForegroundColor Green
        Write-Host " -> Bound to Local Handle: " -NoNewline -ForegroundColor White
        Write-Host "Port $($_.LocalPort)" -NoNewline -ForegroundColor Yellow
        Write-Host " [$GeoData]" -ForegroundColor Gray
    }
    Write-Host ""
    Write-Host "📊 Total Active Network Interchanges: $($Connections.Count)" -ForegroundColor Cyan
} else {
    Write-Host "🛰️  Listening... Standing by for external peer handshake allocations on Ports 8080/8081." -ForegroundColor Yellow
}
Write-Host "====================================================================" -ForegroundColor Cyan