$SourceFile = "ledger_vault_backup.json"
$BackupDir = "C:\ollama\cvn_node_vault_backups"
if (!(Test-Path $BackupDir)) { New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null }
Write-Host "?? Covenant Standard Continuous Archive Vault Protection Loop Initialized." -ForegroundColor Cyan
while ($true) {
    if (Test-Path $SourceFile) {
        $Timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
        $Destination = Join-Path $BackupDir "ledger_vault_snapshot_$Timestamp.json"
        Copy-Item -Path $SourceFile -Destination $Destination -Force
        Write-Host "?? [Vault Backup Pass] Master ledger snapshot cloned safely! Archive: $Destination" -ForegroundColor Green
    } else {
        Write-Host "?? [Vault Warning] Active ledger file is temporarily locked. Retrying..." -ForegroundColor Yellow
    }
    Start-Sleep -Seconds 300
}
