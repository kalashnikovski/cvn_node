:: Scroll straight to the bottom execution block of LaunchMiner.bat and update to read:
if exist ".\cvn_miner.exe" (
    .\cvn_miner.exe --miner-address !LOCAL_RIG_ID!
) else (
    color 0C
    echo 🚨 CRITICAL ERROR: cvn_miner.exe core engine binary was not found!
    echo Please run '.\build_clean.bat' to build your console components.
    pause
)
endlocal