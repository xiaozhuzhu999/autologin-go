@echo off
title Build AutoLoginPro (Go)

set "SCRIPT_DIR=%~dp0"
set "GO_PATH=C:\Program Files\Go\bin"

echo [INFO] Setting Go PATH...
set "PATH=%GO_PATH%;%PATH%"

cd /d "%SCRIPT_DIR%"

echo [INFO] Downloading dependencies...
go mod tidy
if errorlevel 1 (
    echo [ERROR] Failed to download dependencies.
    pause
    exit /b 1
)

echo [INFO] Building executable...
go build -ldflags "-H windowsgui -s -w" -o AutoLoginPro.exe ./cmd/autologin
if errorlevel 1 (
    echo [ERROR] Build failed.
    pause
    exit /b 1
)

echo.
echo [DONE] Build output:
echo %SCRIPT_DIR%AutoLoginPro.exe
echo.
echo [NOTE] Runtime requirements:
echo   - Chrome or Edge browser (for UI and browser automation)
echo   - lib\onnxruntime.dll (Go native OCR)
echo   - ddddocr_weights\ folder (OCR model files)
echo   - No Python dependency required
echo.
pause
