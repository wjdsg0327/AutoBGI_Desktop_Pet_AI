# 使用项目专用工具链和锁文件构建，绝不修改 AutoBGI 主项目的 Go 配置。
param([switch]$Package, [switch]$SkipInstall, [string]$OutputDirectory = 'bin')
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
# 可将新阶段输出放入独立子目录，保留已经审计并冻结的旧版可执行文件与暂存包。
$outputRoot = [IO.Path]::GetFullPath((Join-Path $root $OutputDirectory))
if (-not $outputRoot.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw '构建输出必须位于桌宠项目的子目录'
}
Push-Location -LiteralPath $root
$oldToolchain = $env:GOTOOLCHAIN
$oldGoRoot = $env:GOROOT
$oldBin = $env:GOBIN
$oldArch = $env:GOARCH
$oldOS = $env:GOOS
try {
    $env:GOTOOLCHAIN = 'go1.26.10'
    # 清除外部 Go 安装的根目录，再由指定工具链解析，避免编译器与标准库版本混用。
    $env:GOROOT = $null
    $resolvedGoRoot = (& go env GOROOT | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $resolvedGoRoot) { throw '无法解析桌宠专用 Go 工具链' }
    $env:GOROOT = $resolvedGoRoot
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:GOBIN = Join-Path $root '.tools'
    $wails = Join-Path $env:GOBIN 'wails3.exe'
    # CLI 将版本写到标准错误流，合并捕获后再比较，避免每次构建都重新安装。
    $wailsVersion = ''
    if (Test-Path -LiteralPath $wails) { $wailsVersion = (& $wails version 2>&1 | Out-String).Trim() }
    # 将稳定版 CLI 安装在当前项目，避免覆盖用户已安装的其他版本。
    if ($wailsVersion -ne 'v3.0.0') {
        go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
        if ($LASTEXITCODE -ne 0) { throw 'Wails CLI 安装失败' }
    }
    # 内嵌芙宁娜主题图标、当前用户权限和 PerMonitorV2 清单。
    & $wails generate syso -arch amd64 -icon assets/furina-icon.ico -manifest build/windows/app.manifest -info build/windows/info.json -out autobgi_pet_windows_amd64.syso
    if ($LASTEXITCODE -ne 0) { throw 'Windows 资源生成失败' }
    # 首次绑定扫描要求 embed 目录存在，此占位必定被后续 Vite 发布构建替换。
    New-Item -ItemType Directory -Path 'frontend/dist' -Force | Out-Null
    if (-not (Test-Path -LiteralPath 'frontend/dist/index.html')) { Set-Content -LiteralPath 'frontend/dist/index.html' -Value '<!-- 构建占位 -->' }
    & $wails generate bindings -ts -i
    if ($LASTEXITCODE -ne 0) { throw '绑定生成失败' }
    Push-Location -LiteralPath 'frontend'
    try {
        # 已安装锁定依赖时允许跳过重装，普通构建仍使用 npm ci 保证依赖一致。
        if (-not $SkipInstall) {
            npm ci --no-audit --no-fund
            if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败' }
        }
        npm run build
        if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
    } finally { Pop-Location }
    New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
    $executable = Join-Path $outputRoot 'AutoBGI-Pet.exe'
    go build -tags production -trimpath -ldflags '-s -w -H windowsgui' -o $executable .
    if ($LASTEXITCODE -ne 0) { throw 'Windows 程序构建失败' }
    # 同时核对字符串版本，避免只有固定数值版本而在 Windows 属性中显示为空。
    $windowsInfo = Get-Content -LiteralPath 'build/windows/info.json' -Raw | ConvertFrom-Json
    $versionInfo = [System.Diagnostics.FileVersionInfo]::GetVersionInfo($executable)
    if ($versionInfo.FileVersion -ne $windowsInfo.fixed.file_version -or $versionInfo.ProductVersion -ne $windowsInfo.info.'0804'.ProductVersion) { throw 'Windows 程序版本资源与构建元数据不一致' }
    # 便携包无需管理员安装；程序配置和凭据不会进入压缩包。
    if ($Package) {
        # 每次创建全新暂存目录，保留旧发布证据，并避免重复复制产生嵌套许可目录。
        $staging = Join-Path $outputRoot ('portable-' + $versionInfo.ProductVersion + '-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
        # 在同一 PowerShell 内核对绝对目标，暂存目录必须始终位于本次输出目录内。
        $resolvedStaging = [System.IO.Path]::GetFullPath($staging)
        if (-not $resolvedStaging.StartsWith($outputRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) { throw '便携包暂存路径超出本次输出目录' }
        New-Item -ItemType Directory -Path $staging -Force | Out-Null
        Copy-Item -LiteralPath $executable, 'README.md', 'THIRD_PARTY_NOTICES.md' -Destination $staging -Force
        # 便携包只携带本版本的使用说明和角色素材声明，不附带旧功能的采集脚本。
        New-Item -ItemType Directory -Path (Join-Path $staging 'docs') -Force | Out-Null
        Copy-Item -LiteralPath 'docs/芙宁娜桌宠使用说明.md' -Destination (Join-Path $staging 'docs') -Force
        New-Item -ItemType Directory -Path (Join-Path $staging 'assets/pets/furina') -Force | Out-Null
        Copy-Item -LiteralPath 'frontend/public/pets/furina/LICENSE.txt' -Destination (Join-Path $staging 'assets/pets/furina') -Force
        $packageItems = @('AutoBGI-Pet.exe', 'README.md', 'THIRD_PARTY_NOTICES.md', 'docs', 'assets') | ForEach-Object { Join-Path $staging $_ }
        # 发布包版本直接取已核对的 Windows 元数据，避免改版后包名仍沿用旧版本。
        $packageName = 'AutoBGI-Pet-' + $versionInfo.ProductVersion + '-windows-amd64.zip'
        $packagePath = Join-Path $outputRoot $packageName
        Compress-Archive -LiteralPath $packageItems -DestinationPath $packagePath -Force
        # 输出校验摘要方便分发后核对，摘要不包含任何本地连接资料。
        $hash = Get-FileHash -LiteralPath $packagePath -Algorithm SHA256
        Set-Content -LiteralPath ($packagePath + '.sha256') -Value ($hash.Hash.ToLowerInvariant() + '  ' + $packageName) -Encoding ascii
    }
    Write-Host ('构建完成：' + $executable)
} finally {
    $env:GOTOOLCHAIN = $oldToolchain
    $env:GOROOT = $oldGoRoot
    $env:GOBIN = $oldBin
    $env:GOARCH = $oldArch
    $env:GOOS = $oldOS
    Pop-Location
}
