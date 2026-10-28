# AutoBGI 桌宠协议 1.0

实现位置：主 AutoBGI 的 `desktopPet/` 与 `menu/routes_desktop_pet.go`。桌宠客户端和交付文件仍位于 `AutoBGI_Desktop_Pet_AI/`。协议需要重新编译、运行包含这些源码的主 AutoBGI；仅升级桌宠不会让旧服务自动拥有新接口。

所有接口使用前缀 `/api/desktop-pet/v1`，仅接受单个 `apiKey` 请求头。JWT、URL token 和匿名请求不能访问此组。每个响应包含 `code`、`message`、`requestId`、带时区的 `serverTime` 和 `data`；失败时 `data=null`。认证失败返回 HTTP 401 / `AUTH_INVALID`，不能触发 legacy 降级。

## 范围与接口

运行范围为 `accountScope=instance`、`executionScope=instance`。本版没有可信的游戏运行账号列表、账号切换或稳定执行任务 ID。实例接口拒绝任何 `accountId` 查询，即使空字符串也会拒绝；命令内 `accountId` 应为 `null`。

| 方法与路径 | 成功 data | 说明 |
| --- | --- | --- |
| `GET /capabilities` | 能力对象 | 独立报告 snapshot、taskPresets、events、commandStatus、commandIdempotency、earnings、backpack、cultivationPlans、scanAccounts、queryRoles；accounts=false |
| `GET /snapshot` | Snapshot | 同一锁内返回状态及对应 eventCursor |
| `GET /task-presets` | Preset 数组 | 只列出当前 BGI 的普通、有效 JSON 文件 |
| `GET /accounts` | 不支持 | HTTP 400 / UNSUPPORTED_CAPABILITY |
| `POST /commands` | Receipt | HTTP 202；仅接受 oneLong.start |
| `GET /commands/{commandId}` | Receipt | 对账接口；不触发重新派发 |
| `GET /events` | SSE | 接受 Last-Event-ID 请求头 |
| `GET /query-roles` | 只读查询角色列表 | 仅使用已有米游社绑定缓存，不刷新 Cookie 或切换游戏账号 |
| `GET /scan-accounts` | 扫描目录列表 | 扫描目录标识不代表运行游戏账号 |
| `GET /backpack?scanAccountId=...` | 背包摘要 | 必须显式选择扫描目录；空字符串表示旧版根目录 |
| `GET /earnings?date=YYYY-MM-DD&queryRoleId=...` | 指定查询角色收益 | 角色参数可选；只读复用正式摩拉总账，没有记录返回 503 |
| `GET /cultivation-plans?scanAccountId=...&queryRoleId=...` | 配置计划和缓存需求 | 两个参数均可选；没有 FIFO 执行器或完成事件能力 |
| `GET /cultivation-queue?presetId=...` | 正式保存的 FIFO 队列，schema 1 | 独立 `cultivationQueue` 能力；只接受真实预设 ID |
| `GET /cultivation-queue?presetId=...&schemaVersion=2` | 同一正式队列及保存的元素、A/E/Q 实际技能编号，schema 2 | 还需独立 `cultivationQueueSkillTargets` 能力；每项明确绑定对象或 null |

领域接口的完整字段见 `desktopPet/domain_types.go`。收益归属于当前配置角色 UID；扫描目录和执行实例分别是独立范围。扫描缺席的材料不写成零，无法核实的 owned/missing 保持 null。培养接口只读本地面板和精确匹配的需求缓存，不能把计划清单或库存齐全解释成自动执行完成。

正式队列使用独立 DTO，与 `/cultivation-plans` 的配置摘要分别读取。缺省查询维持 schema 1 和旧字段集；第二版只接受字面量 `schemaVersion=2`，两个参数各出现一次、顺序可互换，拒绝编码拼写、未知或重复项。服务未实现第二版提供器返回 501，不隐式回退。客户端按读取时能力冻结预期版本，拒绝任意方向的版本失配，不重试旧格式。

第二版每个条目必须包含 `targetSkills:null`，或仅含 `element` 与 `skillIds` 的对象。元素是七种规范小写名称，技能编号是三个互异的正 JavaScript 安全整数，严格保留已保存 A/E/Q 顺序。它只表达用户目标意图，不证明当前元素、基础面板、库存、配置消费或执行完成。三个执行相关标志继续为 false。完整两版合同、示例与验证边界见 `cultivation-queue.md`、`contracts/cultivation-queue.schema.json` 和 `contracts/cultivation-queue-v2.schema.json`。

收益返回 `businessDate=null`，因为没有核实游戏业务日刷新规则；`timeZone` 为实际服务器本地时区。兼容字段 `date` 表示现有数据库归档的日历日期，`timezone` 与 timeZone 相同。省略查询参数 `date` 时使用服务器当天，桌宠的“今日收益”采用此方式；显式日期仍按该日期查询。不能据此虚构游戏业务日。

## 只读查询角色

角色能力以扁平的 `data.features.queryRoles=true` 声明，与 `earnings` 等字段并列。它与通用 `accounts`、快照、任务配置、命令和事件的执行账户无关：这些范围仍为实例，`accountId` 仍必须为 null。

`GET /query-roles` 不接受任何查询参数。返回示例：

```json
{
  "scope": "query-roles",
  "source": "mihoyo-binding-cache",
  "updatedAt": "2026-10-27T19:00:00+08:00",
  "defaultQueryRoleId": "mys-role:cn_gf01:100000001",
  "items": [
    {
      "queryRoleId": "mys-role:cn_gf01:100000001",
      "uid": "100000001",
      "region": "cn_gf01",
      "nickname": "示例角色"
    }
  ],
  "warnings": ["绑定缓存不代表当前游戏登录角色，也不能证明当前 Cookie 仍有授权。"]
}
```

`updatedAt` 来自已有缓存文件修改时间或原有刷新流程的成功读取时间；无法确认时为 null，不使用请求时间代替。有效角色最多 200 项，原始列表最多 1000 项，超限返回 503，不截断。无效身份和重复 UID+区服会过滤；默认项仅映射现有 `CurrentGameRole()` 实际结果，原默认项无效时为 null，不另选列表中的角色。没有有效绑定缓存返回 503 / `DATA_UNAVAILABLE`。

标识格式固定为 `mys-role:<region>:<uid>`，区服满足 `^[a-z][a-z0-9_]{1,31}$`，UID 为不以零开头的 9 或 10 位数字。客户端仍应原样回传服务器列表中的 ID。显式空值、非法格式返回 400 / `INVALID_QUERY_ROLE`；格式合法但不在当前缓存返回 404 / `QUERY_ROLE_NOT_FOUND`；重复或未知参数返回 400 / `INVALID_QUERY`。

收益和培养支持可选的 `queryRoleId`。省略时兼容原来的默认角色读取；显式选择后必须验证真实列表成员，不存在时不能回退默认角色。收益返回 `scope=query-role`、匹配的 `queryRoleId/currentRole`，且只按该 UID 过滤总账。总账没有区服列，同一 UID 在当前缓存中对应多个区服时，显式角色收益返回 503 / `DATA_UNAVAILABLE`。

培养返回 `scope=instance-plans-query-role`。计划配置由实例共享，角色面板和需求缓存按所选 UID+区服精确读取。扫描目录没有可信角色绑定；即使显式选择扫描目录，角色模式培养的材料 `owned/missing` 仍全部为 null，并提示未绑定原因。扫描档案和独立背包仍可单独读取，不从目录名猜测角色身份。

客户端手动读取业务摘要时，空选择只使用服务端明确的有效默认项，并把实际使用的 `queryRoleId` 返回界面。显式角色消失、角色列表失败、响应 ID 或 UID/区服不匹配都会阻断角色领域块。服务端缺少 `queryRoles` 能力时，旧读取流程继续兼容；非空角色选择在本地拒绝，不探测额外接口。选择不持久化、不改变连接代次，也不进入快照广播或任务确认。

## Snapshot 与 Preset

Snapshot 至少包含：

```json
{
  "instanceId": "服务端持久实例标识",
  "instanceDisplayName": null,
  "accountId": null,
  "scope": "instance",
  "stateVersion": "不透明并发版本",
  "eventCursor": "本进程epoch:序号",
  "sourceTime": "2026-10-27T19:00:00+08:00",
  "process": {"running": false},
  "task": {
    "id": null,
    "state": "unknown",
    "groupName": null,
    "routeName": null,
    "scriptName": null,
    "progressText": null,
    "scriptProgressText": null,
    "etaText": null,
    "etaAt": null
  }
}
```

`process.running` 来自新鲜的 Windows 进程枚举，枚举失败返回 null，不能当作空闲。`task.state` 保持 unknown，已有日志展示字段仅用于展示。`sourceTime` 表示协议采样时间，不能冒充某条底层日志的实际更新时间。数据读取失败时仍可返回包含未知状态的快照，但预设查询和控制返回 503 / DATA_UNAVAILABLE。

`stateVersion` 随进程占用、启动占用、预设内容或 BGI 目录变化而改变。进度文字和采样心跳不改变该版本。`eventCursor` 是事件位置，可能随进度数据变化推进，不能替代 stateVersion。所有版本均由服务端生成，客户端只能比较相等性。

Preset 包含 `id/name/scope/impact/version`。当前 `id` 的实现为 `preset-` 加规范化配置路径 SHA-256 的前 16 字节十六进制，共 39 字符；它是客户端不应解析的 opaque ID。`version` 是配置内容 SHA-256。目录或文件名变化会得到新的 ID；同名配置内容变化保留 ID、更新 version 和 stateVersion。损坏、超过 2 MiB、目录、链接和不安全参数名不进入可执行名单。

## 命令与恢复

命令 JSON：

```json
{
  "commandId": "8a034754-cc93-41bf-a922-829a60f8ad50",
  "type": "oneLong.start",
  "accountId": null,
  "params": {"presetId": "preset-服务端返回的不透明ID"},
  "expectedStateVersion": "用户确认时的状态版本"
}
```

请求最多 64 KiB，拒绝未知字段、尾随 JSON 和伪造账号。相同命令号与相同完整请求返回原回执；同号不同参数返回 409 / IDEMPOTENCY_CONFLICT。状态改变返回 VERSION_CONFLICT，BetterGI 已运行或启动处理中返回 TASK_BUSY。UUID 大小写归一化处理。

回执包含 `commandId/status/taskId/acceptedAt/dedupeExpiresAt/resultExpiresAt/message`。状态为 accepted、executing、succeeded、rejected、unknown；failed 为兼容协议预留。`succeeded` 仅表示操作系统单次启动动作已提交，整个业务任务仍未知。明确发生在派发前的安全检查失败为 rejected；无法确认副作用或结果保存失败为 unknown。

服务端使用同目录临时写入、文件同步和 Windows 原子替换保存命令日志，并持有跨进程排他文件锁。先保存 accepted，再保存 executing，最后仅调用一次单次启动入口。持久日志默认位于主 AutoBGI 可执行文件目录的 `data/desktop-pet/commands.json`，其中 instanceId 跨重启稳定。日志损坏、锁被占用或初始化失败时保留路径并返回 503，不静默重建后重新控制。

服务重启后，accepted/executing 记录统一转为 unknown，绝不自动再派发。同一命令号重试也仅返回该未知回执。客户端应先查询原回执，用户确认新的操作时才创建新的命令号。

去重重试窗口为 86,400 秒，结果查询窗口为 604,800 秒。过期命令返回 410 / COMMAND_EXPIRED；仍保留命令墓碑拒绝复用旧 ID。日志上限 16 MiB，达到容量时控制按存储不可用拒绝，避免删除历史导致重复执行。生产部署应保存该目录，不能用清空命令日志来解决未知结果。

桌宠客户端只在 HTTP 410 与精确 COMMAND_EXPIRED 同时匹配时提示保留窗口结束，不反射远端原文，不生成新回执状态，也不重发原请求。回执查询与配置、领域手动读取均绑定完整来源、能力和暂停周期；迟到或超时响应无法更新本地 journal 或通知。界面在最终接纳结果、显示错误和补读状态时复核读取周期。当前来源查询失败保留原已知回执，合法查询返回事件与 HTTP 合并后的最高阶段。详见 `manual-read-lifecycle-2026-10-28.md`。

接受和最终派发前都会核对冻结的预设、配置内容和执行上下文；发生变化则拒绝旧确认。安全启动不会关闭 BetterGI、不会关闭游戏、不会自动重试。启动完成后保留 10 秒互斥保护，避免进程尚未出现时第二个新命令抢跑。

底层 BGI 当前没有接收不可变配置快照的事务接口，因此进程派发后被其他程序改写配置不能获得事务级冻结保证。原有 Web、定时任务、外部程序和手工进程操作仍可改变执行器，用户不能据新协议推断整个系统获得全局 exactly-once。常规一条龙入口及底层启动路径已共享启动锁，新协议本身始终不强停。

## 事件

每帧使用 `id=Event.eventId`、`event=Event.type`，`data` 是完整 Event JSON：

```text
id: 服务端epoch:1042
event: command.updated
data: {"schemaVersion":1,"eventId":"服务端epoch:1042","instanceId":"持久实例标识","accountId":null,"taskId":null,"type":"command.updated","occurredAt":"2026-10-27T19:00:00+08:00","data":{"commandId":"命令号","status":"succeeded","taskId":null,"acceptedAt":"时间","dedupeExpiresAt":"时间","resultExpiresAt":"时间","message":"单次启动动作已完成；业务任务结果仍未知"}}

```

本版只生成 `snapshot.updated` 和 `command.updated`。前者 data=Snapshot，后者 data=Receipt。没有可靠 taskId 和终态来源，不生成 task.started/task.finished 或培养完成通知。

先读取 snapshot，再从其 eventCursor 之后订阅。订阅登记、日志补偿和状态更新使用同一把锁，覆盖读取与建连之间的数据变化。事件保留最近 24 小时或 10,000 条，达到任一条件即淘汰。格式错误、超前、过期和其他 epoch 游标返回 HTTP 410 / RESYNC_REQUIRED，客户端必须重新读取快照。事件日志在内存中，重启更换 epoch，明确要求重同步。

服务端每 15 秒发送 SSE 注释保活。每批和每次保活重验最初的请求头密钥，密钥轮换后旧流最迟在下一个保活周期关闭。慢连接写入有 20 秒截止时间；发送期间游标已超出保留窗口则关闭，让重连收到 410。此路由已排除全局 gzip 缓冲。

## 验证边界

使用纯内存提供器和 httptest 验证新协议，不触发真实 BetterGI 或游戏。涵盖并发幂等、请求冲突、版本失效、占用拒绝、接受后配置替换、崩溃恢复、存储锁/损坏/认领失败、过期墓碑、事件补偿/淘汰、认证与密钥撤销。临时测试源码在验证后删除；主服务集成通过根模块构建和静态检查验证。真实主程序重新部署、不同权限下启动、实际游戏流程和长期事件连接仍需现场验收。

2026-10-27：核心协议 9 组模拟测试通过，包含 24 个并发重试；同一组测试在 `-race` 下通过。`go build ./desktopPet ./menu ./OneLong ./task` 和 `go vet ./desktopPet ./menu ./OneLong ./task` 通过。为避免现有 config.init 改写仓库目录的 main.json，测试先编译成 exe，再在专属系统临时目录执行。
