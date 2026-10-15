$ScriptRoot = $PSScriptRoot
if ([string]::IsNullOrEmpty($ScriptRoot)) { $ScriptRoot = Get-Location }

# Hard-lock target file paths and update the destination directory explicitly to your preference
$SourceDbFile = Join-Path $ScriptRoot "cvn_mainnet.db"
$SourceJsonFile = Join-Path $ScriptRoot "ledger_vault_backup.json"
$BackupDir = "E:\cvn_node\backups"

# Ensure the destination backups directory folder exists cleanly on disk
if (!(Test-Path $BackupDir)) { 
    New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null 
}

Clear-Host
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "💎 COVENANT STANDARD (CVN) 24-HOUR ARCHIVE VAULT DAEMON             " -ForegroundColor Cyan
Write-Host "====================================================================" -ForegroundColor Cyan
Write-Host "📡 Status: Monitoring Thornbury Node mainnet storage partitions..." -ForegroundColor Green
Write-Host "📦 Backup Destination Directory: $BackupDir" -ForegroundColor Yellow
Write-Host "⏰ Interval: Executing exactly ONE master snapshot pass every 24 hours." -ForegroundColor Gray
Write-Host "--------------------------------------------------------------------" -ForegroundColor Cyan

# Track the calendar date of the last successful backup pass
$LastBackupDate = ""

while ($true) {
    $CurrentDate = Get-Date -Format "yyyyMMdd"
    
    if (Test-Path $SourceDbFile) {
        # Check if we have already compiled a backup pass for the current calendar date
        if ($LastBackupDate -ne $CurrentDate) {
            $Timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
            $DestDb = Join-Path $BackupDir "cvn_mainnet_$Timestamp.db"
            $DestJson = Join-Path $BackupDir "ledger_vault_snapshot_$Timestamp.json"
            
            try {
                # Replicate database sectors safely down onto the disk partition
                Copy-Item -Path $SourceDbFile -Destination $DestDb -Force -ErrorAction Stop
                
                if (Test-Path $SourceJsonFile) {
                    Copy-Item -Path $SourceJsonFile -Destination $DestJson -Force -ErrorAction SilentlyContinue
                }
                
                Write-Host "💾 [SUCCESS] Master mainnet snapshot cloned cleanly at $(Get-Date -Format 'HH:mm:ss')" -ForegroundColor Green
                Write-Host "   🎯 Target: cvn_mainnet_$Timestamp.db" -ForegroundColor Gray
                
                # Lock in the date flag so the engine stands down until tomorrow
                $LastBackupDate = $CurrentDate
            } 
            catch {
                Write-Host "⏳ [NOTICE] Database currently locked by active mining thread. Re-trying copy pass in 60 seconds..." -ForegroundColor Yellow
                Start-Sleep -Seconds 60
                continue
            }
        }
    } else {
        Write-Host "⚠️  [WARNING] Active cvn_mainnet.db file not found yet. Awaiting genesis boot..." -ForegroundColor Red
    }
    
    # Check the state boundary loop every 10 minutes to maintain ultra-low CPU background idle profiles
    Start-Sleep -Seconds 600
}
