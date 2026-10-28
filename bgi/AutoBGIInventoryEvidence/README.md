# AutoBGIInventoryEvidence

读取宿主准备的固定请求，先后观察游戏画面的 UID，复用 BetterGI 背包计数，输出本次材料数量观察。脚本不读取账号凭据、AutoBGI 数据库或配置，不访问网络，不自动安装到真实 BetterGI 目录。

## 运行条件与源码依据

这是 BetterGI 内嵌 V8 的纯 JavaScript ESM 脚本，不需要 Node.js、npm、浏览器或第三方包。`main.js` 是入口，版本为 `1.0.0`。没有用户设置表单。

实现核对了 BetterGI 的固定源码提交 `94c1de65fa5c525496a776ad63087162626dfbe7`：

- [Genshin.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Core/Script/Dependence/Genshin.cs)：`Uid()` 实际返回 `Task<int>`，必须等待 `await genshin.uid()`。
- [BvStatus.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/GameTask/Common/BgiVision/BvStatus.cs)：调用捕获接口，OCR 右下角 UID；识别失败返回零。
- [Dispatcher.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Core/Script/Dependence/Dispatcher.cs)：`runTask(new SoloTask("CountInventoryItem", ...))` 接收材料名称数组并返回名称到数量的对象。
- [CountInventoryItem.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/GameTask/Common/Job/CountInventoryItem.cs)：按物品图标和数量区域识别背包。失败数量为 `-2`，未找到的名称不写入返回对象。
- [LimitedFile.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Core/Script/Dependence/LimitedFile.cs)：文件接口限制在脚本目录内。`writeTextSync` 返回布尔值，失败可能不抛异常。
- [ScriptProject.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Core/Script/Project/ScriptProject.cs)：V8 启用大小写不敏感成员绑定与异步 Task/Promise 转换；项目依赖的 ClearScript 版本为 `7.4.5`。

这里没有填入未经验证的最低发布版本。运行环境必须实际具备 `genshin.returnMainUi`、异步 `genshin.uid`、`SoloTask`、`dispatcher.runTask` 的 `CountInventoryItem` 分支、`dispatcher.getLinkedCancellationToken`、`sleep` 和所需文件接口。缺少能力时本次采集失败，不能仅凭版本文字判定可用。

游戏窗口、截图器、物品识别模型与 OCR 必须能够正常工作。推荐 16:9、1920×1080；画面遮挡、滤镜、HDR、模型缺少材料、数量识别错误可能影响观察。采集期间请勿切换账号或手动操作游戏。

## 固定文件契约

输入仅为 `local/request.json`，输出仅为 `local/receipt.json`。两个文件都在脚本目录沙箱内，不从请求构造路径。`local/` 在脚本更新时保留；宿主每次都应替换请求并废弃上次回执。

请求的 UTF-8 正文最多 `512 KiB`，最多 `4096` 个材料。JSON 不允许重复字段、未知字段、孤立 Unicode 代理项、超过 16 层嵌套或无效时间。结构示意中的字符串说明必须由宿主替换为真实请求字段：

```json
{
  "schemaVersion": 1,
  "scriptVersion": "1.0.0",
  "requestId": "64位小写十六进制随机请求标识",
  "requestDigest": "宿主冻结请求的64位小写十六进制摘要",
  "createdAt": "UTC RFC3339 时间",
  "expiresAt": "晚于创建时间且未过期的 UTC RFC3339 时间",
  "role": {
    "uid": "不带前导零的十进制 UID 字符串",
    "region": "宿主绑定的精确区服字符串"
  },
  "context": {
    "scopeId": "64位小写十六进制执行范围摘要",
    "configName": "逻辑一条龙名称",
    "planRevision": "正 uint64 的十进制字符串",
    "planFingerprint": "64位小写十六进制计划指纹"
  },
  "materials": [
    { "materialId": 104301, "name": "「自由」的教导" }
  ]
}
```

请求时间只接受 `Z` 结尾的 UTC RFC3339，可以带 1 至 9 位小数秒。宿主与运行环境需要一致的正确时钟。脚本在主要步骤前后检查创建时间、过期时间和取消状态，重新读取文件不会刷新来源时间。

材料 `materialId` 必须是正安全整数，`name` 必须是宿主从官方资料取得的精确名称。任何材料 ID 重复、名称重复均拒绝，包括完全相同的重复条目。全局 ID 与名称的一一映射由宿主负责，脚本不归一化名称、不猜 ID、不下载目录。空材料数组允许完成身份观察，不调用不接受空目标的背包计数任务。

ClearScript `7.4.5` 会把背包计数返回的 .NET `ExpandoObject` 上的 `GetType`、`ToString`、`Equals`、`GetHashCode` 一起枚举。脚本仅将这四个精确名称且值类型为函数的条目视为宿主元数据；其余未请求名称、其余函数以及这四个名称下的任何非函数值均拒绝。鉴于 BetterGI 的成员绑定不区分大小写，材料请求不得使用这四个名称或其大小写变体，以免材料数量与宿主方法发生歧义。

`requestDigest` 由宿主确定摘要算法与规范序列化；脚本只严格检查格式并回显，不计算摘要。宿主需要同时核对自己冻结的请求内容、请求标识、摘要和输出材料集合。脚本还会在结束前重新读取输入，正文任何变化都会使本次采集失败。

## 回执语义

回执固定含以下字段，不附加 `role`、`context`、`verified` 或错误明细字段：

```json
{
  "schemaVersion": 1,
  "requestId": "合法请求的原始标识",
  "requestDigest": "合法请求的原始摘要",
  "scriptVersion": "1.0.0",
  "status": "completed",
  "startedAt": "本次脚本进入采集流程的 UTC 时间",
  "finishedAt": "本次采集完成的 UTC 时间",
  "observedUIDBefore": "采集前实际 OCR 得到的 UID",
  "observedUIDAfter": "采集后实际 OCR 得到的 UID",
  "items": [
    {
      "materialId": 104301,
      "name": "「自由」的教导",
      "count": 0,
      "known": true,
      "reason": ""
    }
  ]
}
```

- 每次运行最先覆盖 `started` 回执，`finishedAt` 为空字符串，`items` 为空。输入尚未验证时，请求标识和摘要也为空字符串。合法请求验证后再写入带标识的 `started`。
- 只有采集前后 UID 均为正安全整数、严格等于请求 UID、请求未变化、未过期、未取消，并且回执写入后读回核对一致，才完成 `completed`。
- 条目按请求顺序输出。明确非负安全整数为 `known=true`，包括明确零，`reason=""`。
- 未返回的名称为 `count=null, known=false, reason="not-returned"`；`-2` 为 `count=null, known=false, reason="recognition-failed"`。未知数量不能补零。
- 字符串数量、其他负数、非整数、非有限数、超出安全整数、额外名称或非对象结果均导致整次采集失败。
- 异常、缺少 API、身份不符、取消、过期或输出异常写 `failed`，清空全部部分结果。`failed.finishedAt` 是实际失败处理时间。失败原因只写运行日志和通知。
- 输出同样限制 `512 KiB`；扫描前预检最坏未知结果大小，超限则失败。

写入返回值必须严格为 `true`，随后读回正文必须完全相同。失败回执无法保存时，仅在 `isFile` 确认固定回执是普通文件后，尝试删除该文件；不递归处理目录，不操作其他路径。若文件系统同时拒绝写入和删除，脚本只能报告失败，无法保证旧文件物理消失。宿主必须主动废弃旧回执并核对本次请求；不能靠“文件存在”或旧 `completed` 宣布成功。

## 接入与验证边界

1. 宿主核对脚本版本和预期文件摘要，准备独占本次请求的执行安排。脚本不自行部署，也不创建外部配置。
2. 宿主在安装副本的 `local/request.json` 写入本次冻结请求，废弃旧回执，再运行该脚本。
3. 宿主只接受本次匹配请求、未超时且严格解析的 `completed`；`started`、`failed`、缺失、截断或未知结果都不构成库存完成。
4. 脚本启动后先回主界面，等待 UI 稳定，再读取 UID；计数关闭 `stopByItemSort`。结束时再次回主界面、等待、读取 UID。
5. 停止按钮通过 BetterGI 的取消令牌传递。过期检查位于宿主调用边界；脚本没有独立计时器，若某个宿主调用长时间不返回，宿主仍须设置等待期限并取消对应执行，不得继续接受陈旧回执。

请求区服仅作为宿主给定文本保存在请求中，脚本没有观察或验证游戏区服。脚本也不保证捕获源永远提供新帧，不检测所有未观察到的账号切换。起止时间是本次流程的本地观察边界，不是官方内部背包更新时间。

本地文件、请求随机标识、摘要回显和脚本文件哈希可以帮助关联与排查，不能防止拥有同等文件权限的进程伪造回执。可信库存生产链、区服证明、官方映射校验、状态冻结以及是否可据此推进培养队列，由 AutoBGI 宿主另行实现。此脚本不执行升级、合成或队列出队。

本版本使用技能静态校验及 Node 假宿主验证协议、调用顺序和失败处理。另在隔离的 .NET `8.0.32`、真实 ClearScript `7.4.5` V8 中使用 BetterGI 相同的两个引擎标志，验证了直接 `ExpandoObject` 与真实异步 `Task<object>` 返回路径；宿主互操作回归为 `119/119`，相同普通对象契约在 Node 中为 `77/77`。验证涵盖零、缺失、识别失败、最大安全整数、额外名称或函数、无效数量及保留材料名称。编译隔离测试宿主时存在两条针对 .NET 5/8 引用统一的 `CS1701` 警告，实际 .NET 8 执行及断言通过。

尚未在真实 BetterGI、真实游戏或账号背包中验收；上述互操作与模拟结果不能作为识别准确率、区服证明或真实游戏端到端证明。观察结果也不自动成为可信库存或启动执行依据。
