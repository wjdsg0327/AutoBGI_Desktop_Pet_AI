# 独立背包采集观察

日期：2026-10-28。阶段：0.2.14。

本阶段将可实际运行的 BetterGI 背包采集脚本与 AutoBGI 的明确准备、回执读取和只读展示接通。它记录一次数量观察，当前不用于自动完成培养计划，不改变一条龙的每日秘境，也不触发启动。

## 使用流程

1. 更新配套 AutoBGI 服务端及网页。服务端新增表由正常数据库迁移创建；桌宠包不包含服务端可执行文件。
2. 将随包的 `bgi/AutoBGIInventoryEvidence` 原样放入该 AutoBGI 所配置 BetterGI 的 `User/JsScript/AutoBGIInventoryEvidence`。保留 `local/` 子目录；脚本名称、文件及版本必须与随附版本一致。准备操作会核对六个执行输入的 SHA-256；不会自动安装或替换已有脚本。
3. 在 AutoBGI 一条龙页面保存培养队列，明确 UID、区服、角色等级与 A/E/Q 目标。全部条目应已有精确身份及目标匹配的官方计算需求缓存；缺少缓存时会明确拒绝准备，不使用名字或默认目标替代。
4. 点击“准备采集请求”。宿主保存随机请求及完整计划指纹，先废弃固定旧完成回执，再写入 `local/request.json`。这一步不操作游戏。
5. 在 BetterGI 中手动运行 `AutoBGIInventoryEvidence`。它返回主界面，等待后读取 UID，调用已有 `CountInventoryItem`，再次返回主界面读取 UID，核对请求未变并写入 `local/receipt.json`。采集期间不要切换账号或手动操作游戏。
6. 回到原培养队列点击“读取本次回执”。网页与桌宠可以查看此次材料数量、未知原因、采集开始和结束时间。

请求窗口为 15 分钟。计划编辑、重排、目标变化、改名或删建会使旧请求失效；时间过期需要重新准备并实际采集。重复读取不会延长来源时间。同一安装目录只有一份固定请求文件，准备其他配置后，旧配置未读取的回执会因正文不匹配而被拒绝。

当前脚本按固定 BetterGI 源码 API 实现，不宣称某个尚未核实的最低发布版本。缺少所需采集、取消、UID 或文件接口时会失败。运行条件、官方源码依据和原始文件协议见脚本自己的 `README.md`。

## 数据与接口

AutoBGI 使用既有鉴权保护两个操作：

| 操作 | 路由 | JSON 正文字段 |
| --- | --- | --- |
| 准备 | `POST /api/talentPlan/inventory/prepare` | `configName`、`scopeId`、`expectedRevision` |
| 读取 | `POST /api/talentPlan/inventory/read` | 同上，另加本次 `requestId` |

两个操作返回 `status: "success"` 与 `data: {plan, inventoryCollection}`。正文严格拒绝重复、额外、遗漏、错误类型及非规范身份字段。接口不接收文件路径、回执正文、库存正文、`verified` 或任意命令。

`GET /api/talentPlan/progress` 及桌宠独立培养队列响应追加可选的 `inventoryCollection`，旧服务缺少该字段仍然兼容：

| 状态 | 含义 |
| --- | --- |
| `unavailable` | 尚无当前完整计划的有效持久采集记录 |
| `pending` | 已准备本次请求，尚未接受完整回执 |
| `observed` | 本次完成回执已通过请求、文件、身份、材料及时间核对 |
| `expired` | 请求或观察过期，历史数量只作参考 |

观察材料使用 `{materialId, name, known, count, reason}`。明确非负整数含零可以显示；未返回为 `not-returned`，识别失败为 `recognition-failed`，两者 `known=false`、`count=null`。当前最多 4096 个材料、512 KiB 请求或回执，数量不超过 JavaScript 安全整数。官方材料目录在请求内要求编号与名称双向唯一；不同角色共用的完全相同材料在宿主合并。

`sourceTime` 是回执记录的采集开始时间，`finishedAt` 是结束时间。区服不是脚本观察的事实：宿主从官方绑定缓存核对当前计划的唯一 UID/区服元组，拒绝同 UID 多区服歧义。脚本前后 UID OCR 都必须与计划 UID 一致。已有的官方计算需求源更新时间缺失时仍然为空，不用本次读取时间补齐。

数据库持久保存完整请求、首次完成回执与摘要。相同语义回执幂等，不同回执不能覆盖已接受结果；完整计划指纹在事务内重新验证。每个执行目录保留至多 64 个未过期请求，创建时有界清理过期记录。

## 证据边界

固定路径、受限目录句柄、普通文件及链接检查、源码摘要、文件身份和回读校验用于拒绝明显的错配或替换。它们不是阻止外部写入的 Windows 租约，也不能证明本机可写回执一定由实际游戏执行产生。前后 UID 相等同样不能证明采集途中未发生无法观察的账号切换。

因此 `inventoryVerified`、`executionAvailable` 和 `completionEventsAvailable` 均保持 `false`。现有培养需求中的 `owned`、`missing` 保持 `null`，观察数量单独展示。没有引入未经核实的三阶天赋书合成配方，也没有把数量条件满足解释为游戏内升级或天赋已执行。

本阶段使用真实源码、模拟 BetterGI 宿主及隔离 SQLite/文件环境验证。另以 .NET 8.0.32 和真实 ClearScript 7.4.5 V8、BetterGI 相同的两个引擎标志，验证同步 `ExpandoObject` 与真实 `await Task<object>` 两条返回路径，共 119 项通过。脚本仅跳过四个精确名称且确为函数的 .NET 元数据方法；其他额外字段、函数和异常数量仍被拒绝。相同普通对象契约在 Node 中 77 项通过。

技能静态校验为 0 FAIL、1 WARN；由于未核实最低 BetterGI 发布版本而有意省略 `bgi_version`，严格模式仍返回非零，未记为严格校验通过。独立测试宿主编译时的两条 `CS1701` 引用统一警告也保留在证据中。没有启动真实 AutoBGI、BetterGI、原神或原生桌宠窗口；识别准确率、真实服务联调、Windows 焦点/多屏/DPI、250 MB 内存目标和八小时运行仍需各自验收。本阶段证据不替代 0.2.13 的冻结发布审计，也不将整体开发规格标为完成。

## 维护

六个执行输入的检查表位于 AutoBGI `menu/talent_inventory_sources.go`。脚本源码有意变更后，在仓库内执行：

```powershell
.\AutoBGI_Desktop_Pet_AI\scripts\sync-inventory-collector-hashes.ps1
```

该命令只生成源码摘要表，不安装、启动或采集。随后重新验证脚本、主机回执链和配套服务端，使用相同脚本随包分发。发布包只复制固定脚本源码、说明和 `local/.gitkeep`，不包含真实请求、回执、账号资料或配置。
