# BetterGI 完整运行副本与任务派发合同

本文件记录 2026-10-28 已实现的任务层接口、固定上游源码事实和验证边界。桌宠 0.2.14 冻结产物未变；本文对应的独立影子入口尚未注册到生产菜单、HTTP 或定时启动链。现有 v1 的 execution、inventory verified、completion 能力继续保持 false。

## 任务层接口

`task/startup_artifact.go` 定义独立的 `OneDragonArtifactStartupWrapper`、准备回调、派发回调和 `OneDragonStartupArtifact`。`RegisterOneDragonArtifactStartupWrapper` 不改变原普通包装器，`StartOneDragonArtifactFrozenOnceIfIdle` 在接线缺失时明确拒绝。

请求始终先冻结用户的逻辑配置。准备回调可以明确传入 nil 以按原配置启动，也可以提交完整影子描述符。描述符必须满足：

- OperationID 为 32 位小写十六进制，物理 Name 精确派生为 `__autobgi_run_` 加 OperationID。
- LogicalName 和 OriginalVersion 与最终检查上下文完全一致。
- ConfigPath 与冻结目录下 `User/OneDragon/Name.json` 精确一致；相对路径、大小写路径别名、含父目录的等价写法、目录外文件均拒绝。
- ConfigVersion 为完整影子正文的 64 位小写 SHA256；派发前同时重新核对逻辑原文件与影子文件。
- 影子正文必须是 JSON 对象，顶层内部 Name 与物理 Name 精确一致。Name 缺失、null、非字符串、重复键、大小写键别名、转义后重复键和尾随 JSON 均拒绝。内部 Name 与摘要使用同一次有界读取，不能分别读取不同文件版本。

普通安全单次入口和旧重试入口都拒绝完整保留名称模式，包括大小写变体。旧入口在警告、关闭、等待和重试之前完成拦截；相似但不符合完整模式的普通名称保持原语义。物理影子只能在专用准备回调内通过验证后派发。

普通路径、专用路径共用一次检查、一次派发与启动互斥保护。重复、并发、保存到范围之外的回调不能重复创建进程。包装器在检查中途提前返回后，检查不能再进入准备回调；入口等待已进入的检查或派发退出后才返回。

`OneDragonDispatchResult` 独立保存 Dispatched、Process 和 Err。进程身份包括 PID、Windows 创建时刻和 Session；身份读取失败也不能抹去已经发生的进程创建。即使包装器吞掉派发错误，任务层仍保存错误。已经派发或结果不确定的异常不能携带 NotDispatched 标记，避免客户端据此自动重发。

## 上游配置消费与保存方式

固定 BetterGI 源码提交：`94c1de65fa5c525496a776ad63087162626dfbe7`。以下结论仅针对该提交的标准一条龙链；没有运行 BetterGI、游戏或账户数据。

官方源码：

- [OneDragonFlowViewModel.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/ViewModel/Pages/OneDragonFlowViewModel.cs)
- [OneDragonTaskItem.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Model/OneDragonTaskItem.cs)
- [OneDragonFlowConfig.cs](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/Core/Config/OneDragonFlowConfig.cs)
- [BetterGenshinImpact.csproj](https://github.com/babalae/better-genshin-impact/blob/94c1de65fa5c525496a776ad63087162626dfbe7/BetterGenshinImpact/BetterGenshinImpact.csproj)
- [.NET 9 File.cs](https://github.com/dotnet/runtime/blob/v9.0.0/src/libraries/System.Private.CoreLib/src/System/IO/File.cs)
- [.NET 9 Windows SafeFileHandle](https://github.com/dotnet/runtime/blob/v9.0.0/src/libraries/System.Private.CoreLib/src/Microsoft/Win32/SafeHandles/SafeFileHandle.Windows.cs)

OneDragonFlowViewModel 在 292–338 行枚举配置，按照 JSON 内部 Name 建立并选择对象。命令参数在 551 行出现于日志；552–568 行若找不到相应内部 Name，会继续当前或默认配置。因此，仅有参数日志不能证明选择了正确配置。

575 行的实际选中名称记录，以及之后同一实例的任务数量记录，可以支持“观察到选择与动作绑定”的有限事实。601 行将配置对象交给任务初始化；首次异步游戏启动发生于 625 行。它们没有回报完整文件摘要或每日字段值，不能升级成配置消费收据。

OneDragonTaskItem 的 91–124 行使用绑定配置对象读取每日秘境参数；其执行并不再次读取同名 JSON。OneDragonFlowConfig 的 322–343 行包含每周模式与每日模式的不同消费逻辑，生成每日补丁时必须拒绝每周模式。

标准保存链在 OneDragonFlowViewModel 的 409–426、489–528 行内：SaveConfig 调用 WriteConfig，以配置内部 Name 决定文件路径，523 行原地写入完整正文；异常会显示保存失败提示。任务启动之后及运行进度变化仍会再次保存。对固定提交中 23 个名称或路径含 OneDragon 的 C# 文件进行只读扫描，该标准保存链未使用 File.Replace 或 File.Move；此结论不覆盖任意自定义脚本和未来版本。

上游项目目标框架为 .NET 9。该版本的 File.WriteAllText 进入 FileMode.Create、FileAccess.Write、FileShare.Read 的句柄写入路径，标准成功路径没有请求 DELETE；这与原地截断重写语义一致。此处核对的是源码合同，没有确认用户机器实际部署的 BetterGI 或 .NET 补丁版本。

## 宿主租约的只读审查

本轮只读检查了 `bgiStatus/one_dragon_run.go` 与 `bgiStatus/one_dragon_run_windows.go`，没有修改这些文件。

宿主保留目录句柄，使用只请求 GENERIC_READ、共享 READ 和 WRITE、拒绝 DELETE 的文件句柄固定物理影子身份。标准 .NET 写入方允许现有读取者，租约也允许该写入方申请写权限，两者在成功保存路径上相容。持有租约时禁止删除或替换影子，但正文允许原地改变；不能称为不可变文件，也不能长期持有拒绝写入的只读共享锁。

宿主先保存 original、initial 和 manifest，再创建物理影子。映射读取会依据 original、每日白名单补丁和派生 Name 重建 initial，核对摘要与正文；持久记录不接受任意物理路径。派发前 Verify 重新读取逻辑原文件和当前影子，后续手工更改会使旧确认失效。Close 只释放句柄，不删除运行副本或恢复证据。

标准保存会清除影子的 NextTaskId，首领奖励累计计数等运行状态也写入影子。逻辑原文件保持不变时，这些状态不会自动同步回原配置；未来生产接线必须明确回写或兼容策略。

进程退出、IPC ping、激活转发回应和参数日志均不能证明实际消费。Windows 进程创建时刻与 BetterGI 日志实例初始化时刻含义不同，不能要求两者完全相等。当前接口没有消费确认、自动清理、运行结果或任务成功能力。

## 隔离验证

证据目录：`../.validation/artifact-task-0bbbc77ae4a04cd386d2a93037b65ed5/`。

使用五份生产任务源码的逐字节副本、极小 config stub、合成配置和注入派发器验证，未初始化实际配置包。真实 Task.go 另做语法树入口顺序检查与实际 task 包编译。验证文件摘要记录于 `source-hashes.json`、`validation.json`。

最终普通测试和 race 测试均通过：13 个顶层测试、61 个子测试、66 个叶子用例；Go 事件流共报告 74 个测试通过。覆盖有效影子、nil 普通目标、旧准备摘要、描述符与路径错误、内部 Name 异常、正文变化、忙碌与枚举失败、程序不可用、正文超限、并发与失效回调、提前返回范围、派发后异常守恒、独立接线及保留名拦截。Windows 身份接口只读取当前隔离测试进程。

实际 task 包仅执行编译，未执行生产初始化或任何真实 BetterGI 派发。测试源、源码副本、合成配置、模块及编译残留在完成后逐文件精确清理；具体路径与结果见 `cleanup.json`。本次未触碰历史测试文件或被冻结的 0.2.14 产物。
