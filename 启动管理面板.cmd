@echo off
powershell.exe -NoProfile -ExecutionPolicy RemoteSigned -File "%~dp0scripts\start-panel.ps1"
if errorlevel 1 (
    pause
    exit /b 1
)
exit /b 0
