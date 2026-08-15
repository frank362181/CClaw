# 确保调试前端口 5500 已有静态服务器在监听。
# - 若端口已被占用（如 Live Server 正在运行）则直接复用，不做任何事。
# - 否则从工作区根目录(src)用 `python -m http.server` 启动一个后台静态服务器。
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File serve.ps1

param(
    [int]$Port = 5500,
    [string]$Directory = ""
)

$ErrorActionPreference = 'Stop'

if (-not $Directory) {
    # 脚本位于 src\.vscode\scripts\serve.ps1，向上两级即工作区根(src)
    $Directory = (Resolve-Path "$PSScriptRoot\..\..").Path
}

# 1) 端口已被占用 → 直接复用现有服务器（通常是 Live Server）
$listening = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
if ($listening) {
    Write-Host "[cclawchat] port $Port already in use - reusing existing server (pid $($listening.OwningProcess))"
    exit 0
}

# 2) 找不到 python → 提示先手动启动 Live Server
$py = Get-Command python -ErrorAction SilentlyContinue
if (-not $py) {
    Write-Error "[cclawchat] python not found - cannot start static server. Start Live Server first (right-click index.html -> Open with Live Server)."
    exit 1
}

# 3) 后台启动静态服务器（与 VS Code 生命周期解耦，调试结束后仍可复用）
Start-Process -FilePath "python" -ArgumentList @(
    "-m", "http.server", "$Port",
    "--bind", "127.0.0.1",
    "--directory", "`"$Directory`""
) -WindowStyle Hidden | Out-Null

# 4) 等待端口就绪（最多约 8 秒）
for ($i = 0; $i -lt 80; $i++) {
    Start-Sleep -Milliseconds 100
    if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
        Write-Host "[cclawchat] static server ready on http://localhost:$Port/cclawchat/index.html"
        exit 0
    }
}

Write-Error "[cclawchat] failed to start server on port $Port"
exit 1
