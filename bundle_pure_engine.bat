@echo off
echo ====================================================================
echo 🧬 GENERATING PURE BLOCKCHAIN ENGINE SECURITY AUDIT DUMP...
echo ====================================================================

set OUTPUT_FILE=cvn_engine_pure_audit.txt
if exist %OUTPUT_FILE% del %OUTPUT_FILE%

echo === COVENANT STANDARD CORE LAYER-1 ARCHITECTURE SPECIFICATION === > %OUTPUT_FILE%
echo Generated on: %date% %time% >> %OUTPUT_FILE%
echo ==================================================================== >> %OUTPUT_FILE%

:: Explicitly loop through and dump every functional backend blockchain file in the workspace
for %%f in (main.go database_core.go blockchain_core.go gossip_mesh.go crypto_auth.go wallet.go security_harness.go app.go read_ledger.go backup_vault.go nat_traversal.go ansi_windows.go main_server.go) do (
    if exist %%f (
        echo 💾 Appending: %%f
        echo. >> %OUTPUT_FILE%
        echo ==================================================================== >> %OUTPUT_FILE%
        echo 📂 FILE: %%f >> %OUTPUT_FILE%
        echo ==================================================================== >> %OUTPUT_FILE%
        type %%f >> %OUTPUT_FILE%
    ) else (
        echo ⚠️ Missing skipped file: %%f
    )
)

echo.
echo ====================================================================
echo ✅ SUCCESS: Complete backend engine safely bundled into %OUTPUT_FILE%!
echo ====================================================================
pause