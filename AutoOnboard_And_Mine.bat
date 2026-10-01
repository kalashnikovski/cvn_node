@echo off
title Covenant Standard Onboarding Portal
color 0B
echo ====================================================================
echo COVENANT STANDARD (CVN) LAYER-1 AUTOMATED ONBOARDING SYSTEM
echo ====================================================================
echo.
echo [1/3] Checking Go Compiler runtime...
where go
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo ERROR: Go was not found. Download it from https://go.dev
    goto final_lock
)
echo.
echo [2/3] Compiling standalone executable file...
set CGO_ENABLED=1
go build .
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo ERROR: Compilation failed. Check your toolchain configuration.
    goto final_lock
)
echo.
echo [3/3] Launching Node Matrix Core and connecting to seed...
cvn_node.exe --connect 202.137.175.220:8080
:final_lock
echo.
echo ====================================================================
echo Script sequence complete.
echo ====================================================================
pause
