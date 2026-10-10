# ✅ PORTABLE BOUNDARY: Automatically compute the script's native execution path directory
$ScriptRoot = $PSScriptRoot
if ([string]::IsNullOrEmpty($ScriptRoot)) { $ScriptRoot = Get-Location }

# Target your actual production binary data files and dynamic backup structures
$SourceDbFile = Join-Path $ScriptRoot "cvn_mainnet.db"
$SourceJsonFile = Join-Path $ScriptRoot "ledger_vault_backup.json"
$BackupDir = Join-Path $ScriptRoot "cvn_node_vault_backups"

# Ensure the backup directory structure exists cleanly on the active drive partition
if (!(Test-Path $BackupDir)) { 
    New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null 
}

Clear-Host
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "💎 COVENANT STANDARD (CVN) CONTINUOUS ARCHIVE VAULT DAEMON          " -ForegroundColor Cyan
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "📡 Status: Monitoring E:\cvn_node local database partitions..." -ForegroundColor Green
Write-Host "📦 Backup Vault Destination: $BackupDir" -ForegroundColor Yellow
Write-Host "--------------------------------------------------------------------" -ForegroundColor Cyan

while ($true) {
    if (Test-Path $SourceDbFile) {
        $Timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
        
        # Define explicit, unique filenames for both your binary database and JSON telemetry layers
        $DestDb = Join-Path $BackupDir "cvn_mainnet_$Timestamp.db"
        $DestJson = Join-Path $BackupDir "ledger_vault_snapshot_$Timestamp.json"
        
        try {
            # Execute a clean, un-throttled forced copy pass to replicate database sectors safely
            Copy-Item -Path $SourceDbFile -Destination $DestDb -Force -ErrorAction Stop
            
            if (Test-Path $SourceJsonFile) {
                Copy-Item -Path $SourceJsonFile -Destination $DestJson -Force -ErrorAction SilentlyContinue
            }
            
            Write-Host "💾 [VAULT PASS] Master database partition cloned cleanly at $(Get-Date -Format 'HH:mm:ss')" -ForegroundColor Green
            Write-Host "   🎯 Archived: cvn_mainnet_$Timestamp.db" -ForegroundColor Gray
        } 
        catch {
            Write-Host "⏳ [VAULT NOTICE] Database currently executing active mining transaction. Retrying next cycle..." -ForegroundColor Yellow
        }
    } else {
        Write-Host "⚠️  [VAULT WARNING] Active cvn_mainnet.db file not found yet. Awaiting genesis initialization..." -ForegroundColor Red
    }
    
    # 5-Minute structural pacing sleep delay block
    Start-Sleep -Seconds 300
}
