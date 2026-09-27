# kshell 编译脚本
# 用法：
#   .\build.ps1              # vet + 编译到 dist\kshell.exe
#   .\build.ps1 -Test        # 先跑全量测试再编译
#   .\build.ps1 -Clean       # 先清空 dist 再编译
param(
    [switch]$Test,
    [switch]$Clean
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

Write-Host '==> go build' -ForegroundColor Cyan
go build -o dist/kshell.exe ./cmd/kshell
if ($LASTEXITCODE -ne 0) { exit 1 }

$out = Get-Item dist\kshell.exe
Write-Host ("==> 完成: {0} ({1:N0} KB, {2:yyyy-MM-dd HH:mm:ss})" -f $out.FullName, ($out.Length / 1KB), $out.LastWriteTime) -ForegroundColor Green
