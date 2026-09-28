# kshell 编译脚本
# 用法：
#   .\build.ps1              # vet + 编译 TUI 到 dist\kshell.exe
#   .\build.ps1 -Test        # 先跑全量测试再编译
#   .\build.ps1 -Clean       # 先清空 dist 再编译
#   .\build.ps1 -Desktop     # wails build 桌面版（自动构建前端），产物拷到 dist\kshell-desktop.exe
param(
    [switch]$Test,
    [switch]$Clean,
    [switch]$Desktop
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

# Go 可能不在系统 PATH（本机装在 D:\software\go），自动探测补齐
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    foreach ($candidate in @('D:\software\go\bin', "$env:ProgramFiles\Go\bin", "$env:LOCALAPPDATA\Programs\Go\bin")) {
        if (Test-Path (Join-Path $candidate 'go.exe')) {
            $env:Path = "$candidate;$env:Path"
            break
        }
    }
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host '[错误] 未找到 go，请确认安装位置' -ForegroundColor Red
    exit 1
}

# -Desktop 需要 wails CLI，与 go 同样做自动探测补齐
if ($Desktop -and -not (Get-Command wails -ErrorAction SilentlyContinue)) {
    foreach ($candidate in @('D:\software\gopath\bin', "$env:USERPROFILE\go\bin")) {
        if (Test-Path (Join-Path $candidate 'wails.exe')) {
            $env:Path = "$candidate;$env:Path"
            break
        }
    }
}
if ($Desktop -and -not (Get-Command wails -ErrorAction SilentlyContinue)) {
    Write-Host '[错误] 未找到 wails CLI，请先 go install github.com/wailsapp/wails/v2/cmd/wails@latest' -ForegroundColor Red
    exit 1
}

if ($Clean -and (Test-Path dist)) {
    Remove-Item dist -Recurse -Force
}

Write-Host '==> go vet' -ForegroundColor Cyan
go vet ./...
if ($LASTEXITCODE -ne 0) { exit 1 }

if ($Test) {
    Write-Host '==> go test ./... -count=1' -ForegroundColor Cyan
    go test ./... -count=1
    if ($LASTEXITCODE -ne 0) { exit 1 }
}

if ($Desktop) {
    # wails build 会按 wails.json 自动执行 frontend 的 npm install / build，再绑定打包。
    # 经 cmd /c 间接执行：wails 把进度日志（KnownStructs 等）写到 stderr，
    # PowerShell 5.1 在 $ErrorActionPreference='Stop' 下会把它们误判为 terminating error。
    Write-Host '==> wails build' -ForegroundColor Cyan
    # 2>&1 在 cmd 层把 stderr 并入 stdout：PowerShell 5.1 在 EAP=Stop 下会把
    # 子进程 stderr 行升级成 NativeCommandError 终止脚本（wails 的 KnownStructs
    # 进度日志必走 stderr）；成功与否由 $LASTEXITCODE 判定。
    cmd /c "wails build 2>&1"
    if ($LASTEXITCODE -ne 0) { exit 1 }
    New-Item dist -ItemType Directory -Force | Out-Null
    try {
        Copy-Item build\bin\kshell.exe dist\kshell-desktop.exe -Force
    } catch [System.IO.IOException] {
        Write-Host '[错误] dist\kshell-desktop.exe 被占用——桌面版应用正在运行，请先从托盘退出再构建' -ForegroundColor Red
        exit 1
    }
    $out = Get-Item dist\kshell-desktop.exe
    $size = $out.Length / 1MB
    Write-Host ("==> 完成: {0} ({1:N1} MB, {2:yyyy-MM-dd HH:mm:ss})" -f $out.FullName, $size, $out.LastWriteTime) -ForegroundColor Green
    exit 0
}

Write-Host '==> go build' -ForegroundColor Cyan
go build -o dist/kshell.exe ./cmd/kshell
if ($LASTEXITCODE -ne 0) { exit 1 }

$out = Get-Item dist\kshell.exe
Write-Host ("==> 完成: {0} ({1:N0} KB, {2:yyyy-MM-dd HH:mm:ss})" -f $out.FullName, ($out.Length / 1KB), $out.LastWriteTime) -ForegroundColor Green
