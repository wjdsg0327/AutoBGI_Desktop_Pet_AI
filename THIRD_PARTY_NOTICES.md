# 第三方依赖与素材说明

## 设计参考，不等于复制代码或素材

本项目参考规格所列 Morrow 的窗口拆分、气泡与低帧率动画思路，参考 VPet 的动作状态分层与 BongoCat 的托盘恢复交互。
本次实现不复制这些项目的源文件、角色图片、动画或字体，不包含 Live2D SDK、模型文件或原神角色素材。

参考来源：

- [Morrow 指定快照](https://github.com/Benny66/morrow/tree/6c70831be2ed39a54440d8653d0ca18332214478)，代码 MIT。
- [VPet 指定快照](https://github.com/LorisYounger/VPet/tree/78384d1dd21b026c09462bc34900db41a7b53280)，代码 Apache-2.0。
- [BongoCat 指定快照](https://github.com/ayangweb/BongoCat/tree/a8d68d63079e302375cce48fab82f15fae75d44d)，代码 Apache-2.0。

代码许可证不自动覆盖这些项目的角色素材。本项目的实际内置精灵及授权以 `assets/pets/default/` 的声明为准。
内置「叶芽 / ABGI 小助手」由 `cmd/assetgen` 原创程序绘制，按 CC0 1.0 Universal 贡献，详见该目录的 `LICENSE.txt`；不复用任何参考项目角色。
若后续引入外部皮肤、音效或字体，必须先单独核实权利并在此补充来源与许可。

## 直接依赖

依赖的确切版本以本项目 `go.mod/go.sum` 与 `frontend/package.json/package-lock.json` 为准。
完整发行应同时保留所用版本的上游许可文件；不得将下表当作对所有传递依赖的完整许可证扫描。

| 依赖 | 许可证 | 来源与归属 |
| --- | --- | --- |
| Wails v3.0.0 | MIT | [wailsapp/wails](https://github.com/wailsapp/wails)，Copyright (c) 2018-Present Lea Anthony |
| golang.org/x/sys v0.48.0 | BSD-3-Clause | [golang/sys](https://github.com/golang/sys)，Copyright 2009 The Go Authors |
| @wailsio/runtime 3.0.0 | MIT | [wailsapp/wails](https://github.com/wailsapp/wails)，使用 Wails 仓库许可 |
| Vue 3.5.24 | MIT | [vuejs/core](https://github.com/vuejs/core)，Copyright (c) 2018-present, Yuxi (Evan) You |
| Pinia 3.0.4 | MIT | [vuejs/pinia](https://github.com/vuejs/pinia)，Eduardo San Martin Morote |
| Vite 7.2.2 / @vitejs/plugin-vue 6.0.2 | MIT | [vitejs/vite](https://github.com/vitejs/vite)，Vite 贡献者 |
| TypeScript 5.9.3 | Apache-2.0 | [microsoft/TypeScript](https://github.com/microsoft/TypeScript)，Microsoft Corporation |
| vue-tsc 3.1.4 / Vue Language Tools | MIT | [vuejs/language-tools](https://github.com/vuejs/language-tools)，Copyright (c) 2021-present Johnson Chu |
| Vitest 4.0.8（开发测试） | MIT | [vitest-dev/vitest](https://github.com/vitest-dev/vitest)，Copyright (c) 2021-Present VoidZero Inc. and Vitest contributors |

直接依赖的上游许可文件随项目保存在 `docs/licenses/`，便携 ZIP 中对应根目录 `licenses/`。`@wailsio/runtime` 的 npm 包元数据声明 MIT，包自身未包含独立 LICENSE 文件；它属于相同 Wails 仓库，适用下方及 `docs/licenses/Wails-LICENSE.txt` 所保留的 Wails 许可。
该目录同时保留实际 Windows production 编译使用的 xdg、go-ole、Go runtime 许可，调试使用的 go-colorable/go-isatty 许可，以及 npm production 锁定依赖的 LICENSE、NOTICE 和 TypeScript 第三方声明；保守纳入被树摇移除的依赖许可。版本及文件对应见 `docs/licenses/INVENTORY.md`，不将依赖元数据的单一许可名称当成原始许可正文。
Vite 的许可文件包含其分发包内的第三方归属，原样保留；原创角色的 CC0 法律文本保存在 `docs/licenses/CC0-1.0.txt`。这些记录不构成对未来依赖变更、所有开发工具或所有框架内嵌组件的法律结论，公开发行前仍需审核锁文件和最终二进制。

Windows WebView2 运行时由 Microsoft 按其许可分发，不是本项目原创资源；使用安装程序时应保留其许可与安装提示。
Windows 系统字体仅作为已安装的系统资源引用，不打包复制字体文件。

## Wails MIT 许可

MIT License

Copyright (c) 2018-Present Lea Anthony

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
