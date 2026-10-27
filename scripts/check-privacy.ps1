# 推送前隐私检查：扫描「将要推送的内容」里有没有敏感信息。
#
# 用法（也可以由 .git/hooks/pre-push 自动调用）：
#   pwsh -File scripts/check-privacy.ps1
#
# 只认退出码：0 = 干净，1 = 命中（push 会被拦下）。
# 这里的规则是"高信号"规则，宁可漏报也不误报；新增规则请先确认
# 现有仓库能过，否则每次推送都会被自己拦住。

$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

# 演示/文档里合法出现的占位值
$allow = @(
    '13800138000',
    '1100000000000001',
    'aedemo0000000000',
    'amdemo0000000000-d',
    '0123456789abcdef',
    'abc***@163.com',
    'C:\Users\you',
    'aecfs5n2y4aaaakd',      # 游戏长 id（协议参数，不是个人信息）
    'aecglf6ee4aaaarz',      # 同上
    'aebdemo0000000000',
    'aedemo0000000000'
)

# 规则：名字 -> 正则
$rules = [ordered]@{
    '手机号'     = '\b1[3-9][0-9]{9}\b'
    '真实邮箱'   = '[A-Za-z0-9._%+-]{4,}@(163|126|yeah|qq|gmail|outlook)\.(com|net)'
    'MPay票据'   = '1-eyJ[A-Za-z0-9+/=_-]{40,}'
    '网易用户id' = '\bae[bc][a-z0-9]{10,}\b'
    '本机路径'   = 'C:\\Users\\[A-Za-z0-9._-]+'
    '开发路径'   = 'E:\\devtools-local'
}

$failed = $false
foreach ($name in $rules.Keys) {
    $pattern = $rules[$name]
    $lines = git grep -n -I -E -- $pattern HEAD -- 2>$null
    if (-not $lines) { continue }

    $bad = @()
    foreach ($line in $lines) {
        $hit = $false
        foreach ($a in $allow) { if ($line.Contains($a)) { $hit = $true; break } }
        if (-not $hit) { $bad += $line }
    }
    if ($bad.Count -gt 0) {
        $failed = $true
        Write-Host "✗ $name：" -ForegroundColor Red
        $bad | Select-Object -First 8 | ForEach-Object { Write-Host "    $_" }
        if ($bad.Count -gt 8) { Write-Host ("    … 还有 " + ($bad.Count - 8) + " 行") }
    }
}

if ($failed) {
    Write-Host ""
    Write-Host "发现敏感信息，已阻止推送。确认要推送时请先清理（或临时 git push --no-verify）。" -ForegroundColor Yellow
    exit 1
}

Write-Host "隐私检查通过：将要推送的内容里没有匹配到敏感信息。" -ForegroundColor Green
exit 0
