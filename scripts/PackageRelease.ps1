# COVENANT STANDARD (CVN) - HIGH-PERFORMANCE RELEASE PACKAGER
$ErrorActionPreference = "Stop"
Clear-Host

Write-Output "=== COVENANT STANDARD PROTOCOL - AUTOMATED BUILD PACKAGING ==="

$TargetZip = "C:\ollama\cvn_node_windows.zip"
$StagingDir = "C:\ollama\cvn_release_staging"

# 1. Force a fresh production-grade local compilation pass
Write-Output "Step 1 of 4: Compiling fresh, hardened binary engine executable..."
go build -o cvn_node.exe .

# 2. Re-create a clean sandbox staging directory
if (Test-Path $StagingDir) { rm -Recurse -Force $StagingDir }
mkdir $StagingDir > $null

# 3. Clean out any old zip archive sitting on disk
if (Test-Path $TargetZip) { rm -Force $TargetZip }

# 4. Copy ONLY the production-critical operational assets (No raw .go source code)
Write-Output "Step 2 of 4: Sweeping critical deployment assets into staging matrix..."
cp "cvn_node.exe" $StagingDir\
cp "AutoOnboard_And_Mine.bat" $StagingDir\
cp "LaunchMiner.bat" $StagingDir\

# 5. Compress the sandboxed files into a pristine deployment archive
Write-Output "Step 3 of 4: Executing compression loops..."
Compress-Archive -Path "$StagingDir\*" -DestinationPath $TargetZip

# 6. Clean up temporary directories to keep your C: drive pristine
rm -Recurse -Force $StagingDir

Write-Output "Step 4 of 4: Production package compiled cleanly!"
Write-Output "LOCATION: $TargetZip"