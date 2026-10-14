@echo off
title Covenant Standard Source Code Bundler
cls
echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) RECURSIVE SOURCE CODE BUNDLER
echo ====================================================================
echo.

cd /d "%~dp0"
set "DUMP_FILE=cvn_source_audit_dump.txt"

if exist "%DUMP_FILE%" del /f /q "%DUMP_FILE%"

echo 🔍 Scanning directory layers for active code sheets...

for /r %%F in (*.go *.html *.json *.bat) do (
    echo %%F | findstr /i /v "build node_modules" >nul
    if not errorlevel 1 (
        echo 📄 Appending: %%~nxF
        echo ==================================================================== >> "%DUMP_FILE%"
        echo FILE PATH: %%F >> "%DUMP_FILE%"
        echo ==================================================================== >> "%DUMP_FILE%"
        echo. >> "%DUMP_FILE%"
        type "%%F" >> "%DUMP_FILE%"
        echo. >> "%DUMP_FILE%"
        echo. >> "%DUMP_FILE%"
    )
)

echo.
echo ✅ SUCCESS: Complete codebase compiled into '%DUMP_FILE%'!
pause
