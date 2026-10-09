@echo off
title Covenant Standard Matrix Core Launcher
cls

// ✅ FIXED: Completely dynamic runtime alignment for external developers
cd /d "%~dp0"

echo ====================================================================
echo 🚀 IGNITING LAUNCHPAD: Triggering Hardened Phase 3 Sync Sentinel...
echo ====================================================================
echo.

if exist ".\cvn_node.exe" (
    .\cvn_node.exe
) else if exist ".\build\bin\cvn_node.exe" (
    .\build\bin\cvn_node.exe
) else (
    echo 🚨 CRITICAL ERROR: cvn_node.exe core binary was not found inside your workspace directory!
    echo Please run '.\build_clean.bat' to bake your production executable first.
    pause
)