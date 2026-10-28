# 实际 AutoBGI API 核验

核验日期：2026-10-27。核验方式：读取当前工作区源码，不对正在运行的 AutoBGI 发送控制请求。

## 核验基线

- 主项目：`D:\Documents\go\autobgi`，分支 `maa`。
- HEAD：`298766a0649fe286af2936b5a4def17dcaad5220`，提交说明「背包扫描添加多用户」。
- 0.1 阶段只实现独立客户端；本轮根据用户追加授权增加桌宠服务端适配、最小路由和安全启动入口，未修改主项目工具链。已有 updater、集成路由和网页构建修改继续保留。
- 下文源码路径均相对 AutoBGI 主仓库。源码核验不等于已完成真实服务联调或 Windows GUI 验收。

## 鉴权与账号范围

`menu/routes_api.go` 使用 `ginServer.Group("/api", auth.AuthMiddleware())` 注册受保护业务路由。
`auth/AuthMiddleware.go` 优先读取非空 `apiKey` 请求头，与 `auth.User.Auth.APIKey` 完全比较。
`auth/loginMapper.go` 从当前工作目录中的 `abgiUser.yaml` 加载 `auth.api_key`。

该中间件还支持现有网页令牌，但桌宠只发送 `apiKey`，不尝试默认密钥、不匿名回退、不使用联机 SecretKey，不把凭据放入 URL。
错误鉴权返回 HTTP 401、`{"code":401,"errorCode":"UNAUTHORIZED","error":"未授权"}`。
正确 APIKey 对应当前实例的配置用户；未发现桌宠可使用的账号级 scope 授权契约。客户端只读限制不是服务端只读授权。

## 已核验路由

| 方法与路径 | 输入及响应 | 副作用与范围 | 首版策略 |
| --- | --- | --- | --- |
| `GET /api/index` | 无业务参数；裸 JSON 对象 | 读取实例共享运行快照 | 连接验证与轮询主来源 |
| `GET /api/appInfo` | `status: success`、`data` 含中文版本字段 | 会调用远程版本查询，可能阻塞；实例级 | 不作为心跳，首版不依赖 |
| `GET /api/oneLong/oneLongAllName` | `{"status":"success","data":["配置名"]}` | 枚举 BGI `User/OneDragon` 目录；实例级，目录不存在返回空列表 | 按需只读展示，不推导稳定 presetId 或账号归属 |
| `POST /api/oneLong/startOneLong` | JSON **字符串**，例如 `"日常一条龙"`；成功 `status: success, msg: 启动成功` | 启动真实执行；实例级，无幂等与可靠命令查询保证 | 桌宠不调用；0.2 通过独立 v1 命令入口确认与对账 |
| `POST /api/oneLong/continueOneLong` | JSON 对象 `{"name":"配置名"}`；返回配置与 NextTaskId 信息 | 从 BGI 书签继续真实执行 | 首版不调用，不能与 startOneLong 请求体混淆 |
| `GET /api/oneLong/groupProgress?name=...` | 配置名称；由日志分析生成进度 | 读取日志分析结果，不是可靠任务事件流 | 首版不依赖 |
| `POST /api/closeBgi` | 无业务请求体；`status: received` | 同时执行 `control.CloseSoftware()` 与 `control.CloseYuanShen()` | 不提供“暂停”伪能力；首版不调用 |
| `POST /api/closeYuanShen` | 无业务请求体；`status: received` | 关闭原神 | 首版不调用 |
| `POST /api/indexSX` | `{"instanceId":"当前重启实例标识"}` | 准备助手后重启 AutoBGI，过期实例返回 409 | 永不用于读取、刷新或重连 |
| `GET /api/indexSX` | 无 | **当前版本返回 405，不再重启** | 首版请求白名单之外 |

依据：`menu/routes_runtime.go`、`menu/routes_tasks.go`、`menu/oneLong_handlers.go`、`menu/restart_service.go`、`menu/App.go`、`OneLong/oneLong.go`。

### 与规格公开基线的重要差异

规格第 5 节的历史公开基线将 `GET /api/indexSX` 列为重启。当前分支已改成带实例约束的 POST；GET 由 `RejectLegacyRestart` 返回 405 和 `Allow: POST`。
这不改变桌宠策略：两种重启路由都不属于状态查询白名单。

## `/api/index` 字段契约

实现位置：`bgiStatus/logService.go` 的 `GetIndex`、`GetIndexData`。

| 原字段 | 当前源码类型 | 客户端内部语义 |
| --- | --- | --- |
| `group` | string | 配置组展示名与 `GroupProgress` 拼接，不是稳定 ID |
| `ExpectedToEnd` | string | 开始、时长、已运行、预计结束的多行展示文本；原样显示，不生成倒计时 |
| `line` | string | 路线展示文本 |
| `scriptName` | string | 当前脚本展示文本 |
| `progress` | string | 配置组执行进度原文；不强制解析百分比 |
| `jsProgress` | string | JS 进度原文 |
| `running` | bool | `BetterGI.exe` 进程存活，不等于一条龙正在运行 |
| `title` | string | `auth.User.SysConfig.SystemName`，实例展示名 |
| `isAdmin` | bool | 服务端管理员运行状态；不作为桌宠权限或任务状态来源 |

进程状态来源：`bgiStatus/BetterGIClose.go` 的 `pollBetterGIStatus` 检测 `BetterGI.exe`，然后更新 `info.Running`。
旧快照未提供 serverInstanceId、taskId、账号 ID、结构化任务终态、服务端时间、stateVersion 或 eventCursor。
因此 legacy 的 `task.state` 始终为 `unknown`，身份字段保持空；不得依据进程关闭、旧文本或动画结束生成“任务成功”。

## 原有业务接口与本次只读适配

`menu/routes_statistics.go` 已提供 `GET /api/BagStatistics/accounts`，返回 `accounts` 与 `defaultAccount`。
`JsScript/BagScan/accounts.go` 明确这些账户是扫描记录根目录或一层子目录的名称，空字符串代表旧版根目录。
`GET /api/BagStatistics?bagAccount=...` 已支持显式扫描账户，并拒绝重复、无效或不存在的账户；缺失账户不会回退到其他账户。

这证明当前主项目已有背包扫描账户功能，不能写成“主项目没有任何账号接口”。但扫描目录账户不自动等于当前运行账号、授权主体或全局执行器账号。
没有将它映射成通用账号选择能力。0.2 通过服务端只读出口复用解析器、扫描时间与内容版本，客户端只访问 API，不读取服务器磁盘。详见 `domain-audit.md`。

同一文件还提供 `/api/BagStatistics/Morale`、怪物摩拉统计、来源统计和狗粮日志收益接口。0.2 读取正式摩拉总账，区分日历日期与未知游戏业务日；客户端不解析日志自行累加。
`menu/routes_tasks.go` 有 `/api/characterPlan/*` 业务。旧桌宠 `/cultivation-plans` 仅读取该配置及现有算法缓存，没有执行队列或完成事件语义，不修改和重算培养计划。0.2.5 另行协商下述正式队列读取，不改变旧接口合同。

### 0.2.5 正式队列读取

`menu/desktop_pet_cultivation_queue.go` 将正式持久队列和明确目标缓存映射到可选 `CultivationQueueReader`。`desktopPet/cultivation_queue.go` 新增 `GET /api/desktop-pet/v1/cultivation-queue?presetId=...`，使用同一 `apiKey` 鉴权，仅接受真实一条龙 ID。能力声明为独立 `features.cultivationQueue`，与旧摘要及查询角色互不替代。

目录与队列读取共用生命周期锁，服务在提供器调用前后复核完整预设版本和执行目录摘要。响应保留真实绑定身份、保存顺序、明确目标、nullable 面板及需求来源时间；版本使用字符串，库存与缺口保持 null。该路由不联网刷新、不写数据库或配置、不触发游戏控制、不生成完成事件。JSON Schema、合成示例与验证记录见 `docs/contracts/` 及 `cultivation-queue.md`。

## MCP 与联机通道

`MCP/server.go` 注册 `/mcp/sse`（GET/POST）与 `/mcp/messages`（POST）。
当前 SSE 建连方法直接建立会话并发送 `endpoint` 事件；messages 方法内部调用 `isAuthorizedMCPRequest`，可接受既有 APIKey 变体。
该传输承载 MCP JSON-RPC 响应，不是 `/api/index` 的业务事件订阅；不能把 messages 的鉴权结论套到 SSE 建连。

当前 `RunCronTask` 已校验任务名和非负延迟，`MCP/service.go` 的 `AddTaskMcp` 调用 `parameterValidation` 后才安排执行；这与更老公开分支的部分问题不完全相同。
但这些工具仍能真实启动、关闭、签到、采集与录屏，不属于首版规则查询的白名单，也不交给模型自动调用。
`/api/abgiSSE/*` 是联机业务路由，不按名称接入桌宠状态广播。

## 本次新增协议状态

本次新增 `desktopPet/`、`menu/routes_desktop_pet.go` 及注册，提供 `/api/desktop-pet/v1`。完整路由和保证见 `API_V1.md` 与 `api-v1.openapi.yaml`。
这是工作区新增源码，旧部署不会自动获得这些接口，主 AutoBGI 需要重新构建和部署。桌宠 0.2 同时实现 PetV1 和 Legacy 适配器。
自动协商只有明确 404/未实现时才允许验证并切换 legacy；401/403、HTML 登录页和不兼容 JSON 不能通过降级掩盖。

## 安全联调限制

本次契约检查使用本地模拟提供器及隔离数据，不调用生产启动器、关闭、重启、签到或扫描入口。真实 PetV1 客户端与真实协议服务在模拟提供器上完成联调，同一 UUID 重复提交只有一次模拟派发。
真实服务仍需维护者在桌宠输入地址与 APIKey 后联调；默认仅查询，关闭后每次启动需核对确认目标。APIKey 不进入文档、截图、示例配置或 Git。
