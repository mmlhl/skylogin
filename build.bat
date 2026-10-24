@echo off
setlocal
chcp 65001 >nul 2>&1
cd /d "%~dp0"
echo 正在构建 skylogin.exe …
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H=windowsgui" -o skylogin.exe ./cmd/skylogin
if errorlevel 1 (
  echo.
  echo 构建失败。
  pause
  exit /b 1
)
for %%A in (skylogin.exe) do echo 构建完成: %%~fA  (%%~zA 字节)
echo.
echo 双击 skylogin.exe 即可使用（会打开一个独立窗口）。
pause
