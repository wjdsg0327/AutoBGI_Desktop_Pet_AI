# AutoBGI 芙宁娜桌宠

这是独立运行的 Windows 桌宠，通过 AutoBGI 的 API 地址和密钥连接 BetterGI 实时日志。桌宠设置中可添加多条关键词规则，指定芙宁娜的动作、气泡台词和由用户决定是否执行的代办。

首次使用、连接说明、日志限制和构建方式见 [芙宁娜桌宠使用说明](docs/芙宁娜桌宠使用说明.md)。

本版本仅开放连接、日志与关键词动作相关功能。旧版叶芽桌宠的说明保存在开发项目的 `docs/README-叶芽旧版.md`，其中描述的其他功能不属于本版本。

## Windows 打包

在 Windows PowerShell 中进入项目根目录，先关闭正在运行的桌宠，然后执行：

```powershell
.\scripts\build.ps1 -Package
```

脚本会自动完成前端构建和 Go 编译，并在 `bin` 目录生成 `AutoBGI-Pet.exe`、便携 ZIP 以及对应的 `.sha256` 校验文件。首次构建会安装锁定的 npm 依赖和本项目使用的 Wails CLI。

依赖已经准备好时，可以跳过 npm 依赖安装：

```powershell
.\scripts\build.ps1 -Package -SkipInstall
```

需要将产物写入其他目录时：

```powershell
.\scripts\build.ps1 -Package -OutputDirectory release
```

便携包不包含本机连接资料、API 密钥、规则配置和历史记录。完整的环境要求、输出说明和故障处理见 [芙宁娜桌宠使用说明](docs/芙宁娜桌宠使用说明.md)。
