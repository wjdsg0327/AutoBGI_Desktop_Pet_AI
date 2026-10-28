param()

$ErrorActionPreference = 'Stop'
# 只读取随附脚本的六个执行输入，重新生成 AutoBGI 固定安装检查表，不安装或运行游戏脚本。
$petProject = Split-Path -Parent $PSScriptRoot
$repository = Split-Path -Parent $petProject
$source = Join-Path $petProject 'bgi\AutoBGIInventoryEvidence'
$output = Join-Path $repository 'menu\talent_inventory_sources.go'
$names = @(
    'manifest.json',
    'main.js',
    'src/modules/collector.js',
    'src/modules/contract.js',
    'src/modules/receipt.js',
    'src/utils/json.js'
)
$lines = [Collections.Generic.List[string]]::new()
$lines.Add('package menu')
$lines.Add('')
$lines.Add('// 此文件由桌宠 scripts/sync-inventory-collector-hashes.ps1 生成；脚本源码变更后必须同步摘要。')
$lines.Add('// 摘要只核对固定安装版本，不将本机可写文件或回执升级为自动推进的可信来源。')
$lines.Add('var talentInventoryCollectorSourceHashes = map[string]string{')
foreach ($name in $names) {
    # 文件名来自固定清单，拒绝缺失输入，避免生成不完整的可用标记。
    $path = Join-Path $source $name
    if (-not [IO.File]::Exists($path)) { throw "Missing collector input: $name" }
    $digest = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    $lines.Add('    "' + $name + '": "' + $digest + '",')
}
$lines.Add('}')
$lines.Add('')
[IO.File]::WriteAllText($output, ($lines -join "`n"), [Text.UTF8Encoding]::new($false))
# gofmt 仅格式化本次生成的 Go 文件，不扫描或修改其他工作区文件。
& gofmt -w $output
if ($LASTEXITCODE -ne 0) { throw 'gofmt failed for collector source hashes' }
Write-Output $output
