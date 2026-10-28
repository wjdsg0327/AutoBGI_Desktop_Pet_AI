# 调试也运行真实原生宿主；纯浏览器 npm dev 只用于布局预览，不伪造后端。
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
& (Join-Path $PSScriptRoot 'build.ps1')
if ($LASTEXITCODE -ne 0) { throw '开发构建失败' }
Push-Location -LiteralPath $root
$oldToolchain = $env:GOTOOLCHAIN
$oldGoRoot = $env:GOROOT
try {
    $env:GOTOOLCHAIN = 'go1.26.10'
    # 调试同样使用桌宠工具链的标准库，不能继承主项目其他 Go 版本的 GOROOT。
    $env:GOROOT = $null
    $resolvedGoRoot = (& go env GOROOT | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $resolvedGoRoot) { throw '无法解析桌宠专用 Go 工具链' }
    $env:GOROOT = $resolvedGoRoot
    go run .
    if ($LASTEXITCODE -ne 0) { throw '桌宠调试运行失败' }
} finally { $env:GOTOOLCHAIN = $oldToolchain; $env:GOROOT = $oldGoRoot; Pop-Location }
