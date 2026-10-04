# ====================================================================
# 💎 COVENANT STANDARD (CVN) LIVE IDENTITY HUD & PEER RESOLVER v1.2
# ====================================================================
Clear-Host
$LedgerPath = "C:\ollama\cvn_node\ledger_vault.json"

Write-Host "📡 Initializing Live Peer Connection Identity HUD..." -ForegroundColor Cyan
Write-Host "🔍 Monitoring Consensus (8080) & Explorer (8081) and indexing identities..." -ForegroundColor Yellow
Write-Host "---------------------------------------------------------------------"

$IdentityCache = @{}
$IdentityCache["202.137.175.220"] = "Master_Seed_Anchor_Rig"
$IdentityCache["127.0.0.1"]       = "Localhost_Creator_Console"

function Refresh-LedgerIdentities {
    if (Test-Path -Path $LedgerPath) {
        try {
            $RawData = Get-Content -Raw -Path $LedgerPath
            if ([string]::IsNullOrWhitespace($RawData)) { return }
            $Blockchain = ConvertFrom-Json $RawData -ErrorAction SilentlyContinue
            if (!$Blockchain) { return }
            foreach ($Block in $Blockchain) {
                if ($Block.transactions) {
                    foreach ($Tx in $Block.transactions) {
                        if ($Tx.witness -and $Tx.witness -ne "LOCAL_NODE_ENGINE" -and $Tx.outputs) {
                            foreach ($Output in $Tx.outputs) {
                                if ($Output.recipient -and !$IdentityCache.ContainsKey($Output.recipient)) {
                                    $IdentityCache[$Output.recipient] = $Tx.witness
                                }
                            }
                        }
                    }
                }
            }
        } catch {}
    }
}

while ($true) {
    Refresh-LedgerIdentities
    $ActiveConnections = Get-NetTCPConnection -LocalPort 8080, 8081 -State Established -ErrorAction SilentlyContinue | Where-Object { $_.RemoteAddress -ne "0.0.0.0" }
    if ($ActiveConnections) {
        $Timestamp = Get-Date -Format "HH:mm:ss"
        foreach ($Conn in $ActiveConnections) {
            $IP = $Conn.RemoteAddress
            $Port = $Conn.LocalPort
            $ResolvedTag = "Unregistered_External_Miner"
            if ($IdentityCache.ContainsKey($IP)) { $ResolvedTag = $IdentityCache[$IP] }
            
            $DisplayTag = "$ResolvedTag "
            while ($DisplayTag.Length -lt 30) { $DisplayTag += " " }
            
            if ($Port -eq 8080) {
                Write-Host "[$Timestamp] ⛏️  P2P CONSENSUS (:8080) | Node: " -NoNewline -ForegroundColor Green
                Write-Host $DisplayTag -NoNewline -ForegroundColor White
                Write-Host "Connected from $IP" -ForegroundColor Gray
            } else {
                Write-Host "[$Timestamp] 🌐 EXPLORER DASH (:8081) | Node: " -NoNewline -ForegroundColor Blue
                Write-Host $DisplayTag -NoNewline -ForegroundColor White
                Write-Host "Browsing from $IP" -ForegroundColor Gray
            }
        }
    }
    Start-Sleep -Seconds 3
}