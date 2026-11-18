# 把 GitHub Release 上的 Windows 安装包原样补传到 GitCode。
#
# 设计原则：
# - Windows 包只由 GitHub Actions 构建一次；本脚本只下载、校验、上传，绝不重新打包。
# - 上传使用 GITCODE_TOKEN（环境变量，进程级优先，其次用户级），不写入仓库。
# - 同名附件先按 GitCode API 删除再传，保证重跑幂等（GitCode 不会覆盖同名附件）。
#
# 用法：
#   $env:GITCODE_TOKEN = '...'
#   .\tools\release\upload-windows-to-gitcode.ps1 -Tag v0.1.33
#   .\tools\release\upload-windows-to-gitcode.ps1 -Tag v0.1.33 -DryRun
#   .\tools\release\upload-windows-to-gitcode.ps1 -Tag v0.1.33 -ProxyPrefix https://ghfast.top
#
# 退出码：0 成功；非 0 失败（下载/哈希/上传/校验任一失败）。

param(
    [Parameter(Mandatory = $true)]
    [string]$Tag,

    [string]$Owner = 'abraveheart2023',
    [string]$Repo = 'kshell',
    [string]$GitHubOwner = 'kaiys202212',
    [string]$GitHubRepo = 'kshell',
    [string]$TokenEnv = 'GITCODE_TOKEN',

    # 可选 GitHub 代理前缀，如 https://ghfast.top（脚本会拼成 <proxy>/<github url>）
    [string]$ProxyPrefix = '',

    # 只下载并校验哈希，不删除/上传任何附件
    [switch]$DryRun,

    # 上传后再从 GitCode 下载一次并复核 sha256（多耗一次 16MB 下载）
    [switch]$VerifyDownload
)

$ErrorActionPreference = 'Stop'

$ZipName = 'kshell-desktop-windows-amd64.zip'
$SumsName = 'SHA256SUMS'
$ApiBase = 'https://api.gitcode.com/api/v5'
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path

function Write-Step([string]$msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Fail([string]$msg) { Write-Host "[失败] $msg" -ForegroundColor Red; exit 1 }

function Get-Token {
    $v = [Environment]::GetEnvironmentVariable($TokenEnv, 'Process')
    if ([string]::IsNullOrWhiteSpace($v)) {
        $v = [Environment]::GetEnvironmentVariable($TokenEnv, 'User')
    }
    if ([string]::IsNullOrWhiteSpace($v)) {
        Fail "未找到环境变量 $TokenEnv（进程/用户级均为空）"
    }
    return $v.Trim()
}

function Get-ReleaseBase {
    return "https://github.com/$GitHubOwner/$GitHubRepo/releases/download/$Tag"
}

function Get-DownloadUrls([string]$fileName) {
    $base = (Get-ReleaseBase) + '/' + $fileName
    $urls = @()
    if (-not [string]::IsNullOrWhiteSpace($ProxyPrefix)) {
        $urls += ($ProxyPrefix.TrimEnd('/') + '/' + $base)
    }
    $urls += $base
    return $urls
}

function Invoke-Download([string]$fileName, [string]$dest) {
    $lastErr = ''
    foreach ($url in (Get-DownloadUrls $fileName)) {
        for ($i = 1; $i -le 3; $i++) {
            try {
                if (Test-Path -LiteralPath $dest) { Remove-Item -LiteralPath $dest -Force }
                # curl 带重试/断点更稳；不存在则回退 Invoke-WebRequest
                $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
                if ($curl) {
                    # 经 cmd 合并 stderr：PS 5.1 在 EAP=Stop 下会把 curl 进度输出当终止错误。
                    $cmdLine = 'curl.exe -fsSL --retry 3 --retry-delay 2 --connect-timeout 30 -o "' + $dest + '" "' + $url + '" 2>&1'
                    & cmd /c $cmdLine | Out-Null
                    if ($LASTEXITCODE -ne 0) { throw "curl exit $LASTEXITCODE" }
                } else {
                    Invoke-WebRequest -Uri $url -OutFile $dest -UseBasicParsing -TimeoutSec 600
                }
                if (-not (Test-Path -LiteralPath $dest) -or (Get-Item -LiteralPath $dest).Length -le 0) {
                    throw '下载结果为空'
                }
                return
            } catch {
                $lastErr = $_.Exception.Message
                Start-Sleep -Seconds (2 * $i)
            }
        }
    }
    Fail "下载 $fileName 失败：$lastErr"
}

function Get-WindowsHash([string]$sumsText) {
    $pattern = '(?im)^([0-9a-fA-F]{64})\s+\*?' + [regex]::Escape($ZipName) + '\s*$'
    $m = [regex]::Match($sumsText, $pattern)
    if (-not $m.Success) { Fail "$SumsName 中没有 $ZipName 的校验行" }
    return $m.Groups[1].Value.ToLower()
}

function Invoke-GitCode([string]$method, [string]$path, [string]$token) {
    $headers = @{ 'Authorization' = "Bearer $token"; 'User-Agent' = 'kshell-release' }
    try {
        $r = Invoke-WebRequest -Uri ($ApiBase + $path) -Method $method -Headers $headers -UseBasicParsing -TimeoutSec 60
        return @{ ok = $true; status = [int]$r.StatusCode; body = $r.Content }
    } catch {
        $status = $null
        if ($_.Exception.Response) { $status = [int]$_.Exception.Response.StatusCode }
        return @{ ok = $false; status = $status; err = $_.Exception.Message }
    }
}

if ($Tag -notmatch '^v\d+\.\d+\.\d+') { Fail "Tag 不合法：$Tag" }

Write-Step "校验本地 tag $Tag"
Push-Location $RepoRoot
try {
    $commit = (& git rev-parse "$Tag^{commit}" 2>$null)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($commit)) {
        Fail "本地仓库找不到 tag $Tag"
    }
    Write-Host "tag $Tag -> commit $commit"
} finally {
    Pop-Location
}

$tmp = Join-Path $env:TEMP ("kshell-gitcode-" + [DateTimeOffset]::UtcNow.ToUnixTimeSeconds())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    Write-Step "下载 $SumsName"
    $sumsPath = Join-Path $tmp $SumsName
    Invoke-Download $SumsName $sumsPath
    $sumsText = Get-Content -LiteralPath $sumsPath -Raw
    $want = Get-WindowsHash $sumsText
    Write-Host "GitHub sums 中 windows 哈希: $want"

    Write-Step "下载 $ZipName（原样，不重新打包）"
    $zipPath = Join-Path $tmp $ZipName
    Invoke-Download $ZipName $zipPath
    $got = (Get-FileHash -LiteralPath $zipPath -Algorithm SHA256).Hash.ToLower()
    if ($got -ne $want) { Fail "本地下载的 Windows 包哈希不匹配：got=$got want=$want" }
    Write-Host "sha256 校验通过: $got"

    if ($DryRun) {
        Write-Host "[DryRun] 将删除 GitCode 上同名附件（若有）后上传 $ZipName" -ForegroundColor Yellow
        Write-Host "[DryRun] 未调用任何写接口" -ForegroundColor Yellow
        exit 0
    }

    $token = Get-Token

    Write-Step "查询 GitCode Release $Tag"
    $rel = Invoke-GitCode 'GET' "/repos/$Owner/$Repo/releases/tags/$Tag" $token
    if ($rel.ok) {
        $relJson = $rel.body | ConvertFrom-Json
        $existing = @($relJson.assets | Where-Object { $_.type -eq 'attach' -and $_.name -eq $ZipName })
        foreach ($a in $existing) {
            Write-Step "删除同名旧附件 $($a.name) (id=$($a.id))"
            $del = Invoke-GitCode 'DELETE' "/repos/$Owner/$Repo/releases/$Tag/attach_files/$($a.id)" $token
            if (-not $del.ok) { Fail "删除旧附件失败：status=$($del.status) $($del.err)" }
        }
    } elseif ($rel.status -eq 404) {
        Write-Host 'GitCode Release 尚不存在，将由 syncgitcode 创建'
    } else {
        Fail "查询 GitCode Release 失败：status=$($rel.status) $($rel.err)"
    }

    Write-Step "上传 $ZipName 到 GitCode"
    $env:GITCODE_TOKEN = $token
    $env:GITCODE_OWNER = $Owner
    $env:GITCODE_REPO = $Repo
    Push-Location $RepoRoot
    try {
        $goLine = 'go run ./cmd/syncgitcode -dir "' + $tmp + '" -tag "' + $Tag + '" 2>&1'
        & cmd /c $goLine
        if ($LASTEXITCODE -ne 0) { Fail "syncgitcode 失败（exit $LASTEXITCODE）" }
    } finally {
        Pop-Location
    }

    Write-Step "校验 GitCode 附件"
    $rel2 = Invoke-GitCode 'GET' "/repos/$Owner/$Repo/releases/tags/$Tag" $token
    if (-not $rel2.ok) { Fail "上传后查询 Release 失败：status=$($rel2.status) $($rel2.err)" }
    $asset = @(($rel2.body | ConvertFrom-Json).assets | Where-Object { $_.type -eq 'attach' -and $_.name -eq $ZipName })
    if ($asset.Count -eq 0) { Fail "GitCode 上找不到上传后的 $ZipName" }
    $digest = [string]$asset[0].digest
    if ($digest -ne "sha256:$got") {
        Fail "GitCode digest 不一致：got=$digest want=sha256:$got"
    }
    Write-Host "GitCode 附件校验通过: id=$($asset[0].id) digest=$digest"

    if ($VerifyDownload) {
        Write-Step "从 GitCode 回下载并复核 sha256"
        $back = Join-Path $tmp ("back-" + $ZipName)
        Invoke-WebRequest -Uri ([string]$asset[0].browser_download_url) -OutFile $back -UseBasicParsing -TimeoutSec 600
        $backHash = (Get-FileHash -LiteralPath $back -Algorithm SHA256).Hash.ToLower()
        if ($backHash -ne $got) { Fail "GitCode 回下载哈希不一致：got=$backHash want=$got" }
        Write-Host "GitCode 回下载校验通过: $backHash"
    }

    Write-Host ''
    Write-Host "完成：$Tag / $ZipName" -ForegroundColor Green
    Write-Host "  sha256 = $got"
    Write-Host "  GitCode asset id = $($asset[0].id)"
} finally {
    if (Test-Path -LiteralPath $tmp) { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue }
}
