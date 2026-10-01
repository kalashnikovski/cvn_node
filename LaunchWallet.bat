@echo off
title Covenant Standard Desktop Wallet Launcher

echo ====================================================================
echo 💎 COVENANT STANDARD (CVN) LAYER-1 GRAPHICAL WALLET MANAGEMENT UTILITY
echo ====================================================================
echo.

echo [1/2] Initializing secure runtime variables...
echo [2/2] Spanning Fyne GUI Cryptographic Client Application Interface...
echo.

:: Launch the binary using the explicit wallet flag parameter natively. 
:: Go will intercept this variable and run your RunWalletGUI() thread block cleanly.
cvn_node.exe --wallet

if %ERRORLEVEL% NEQ 0 (
    echo.
    echo ⚡ ERROR: The graphical window application encountered an unexpected boundary obstacle.
    echo 👉 Ensure your graphics hardware layout is active and your environment toolchains are built.
    pause
)