# ABGI 全量 API 文档

> 整理日期：2026-10-28；源码基准：`9cb23acdb` 当前工作区。本文依据路由注册、鉴权中间件、处理器、校验和实际响应构造整理，不表示已经针对运行服务完成联调。所有请求/响应示例为说明性示例，凭据、名称、ID、时间和数据均为占位值。

## 接入说明

- 服务地址使用 `{baseUrl}`，例如 `http://127.0.0.1:8082`；来源为 main.json 的 `post`。空值或 `:` 时默认8082，端口占用会递增，所以应以启动日志中的实际监听端口为准；源码默认启动 HTTP，HTTPS/wss 需相应代理或部署支持。
- 接口按 **HTTP方法＋完整路径** 区分，共 **401 个正常显式端点**（包含可选 MCP 3个、SessionActions 10个，且 ZZZ 初始化成功）；**另列1个条件 ANY 兜底**，与 ZZZ 正常8个接口互斥，不计作正常业务端点。未启用模块时，部分接口不会注册或会返回禁用错误。
- 另有 `/img/*filepath`、`/assets/*filepath` 的 GET/HEAD 静态挂载，共4个静态方法路径，以及全局 OPTIONS、SPA/未知API回退，见文末附录；不混入401个显式端点。
- URL中的 `:id`、`:name`、`:category` 表示一个必填路径段，`*path` 表示可含多层目录的尾部路径；调用时替换占位符，不发送冒号或星号。字符串参数按UTF-8进行URL编码，不把JSON参数擅自放到Query中。
- 文档中的 `string`、`integer`、`boolean`、`T[]`、`map<string,T>` 是字段类型说明，不是可以原样发送的 JSON；可选参数用 `?`，其余字段是否必填以接口说明为准。源码未初始化的切片或映射可能序列化为 `null`，客户端需兼容；具体接口明确初始化为空数组/对象时以其说明为准。未声明 body 时不要自动附 `{}`，特别是接收原始字符串/数组的接口。
- 响应没有统一外壳：包括 `status/data`、`status/msg`、`success`、业务 `code`、直接对象/数组、文本、二进制、空响应。HTTP200不保证业务成功；必须按接口说明查看业务状态和Content-Type。HTTP202仅表示受理，异步任务应继续查状态；修改类操作不保证幂等，不应盲目重试。
- 文档按服务端实际行为标注副作用，包含有写操作的GET。不能因方法为GET就认定完全只读。关机、重启、启动任务、桌面键鼠、配置保存、删除、上传/覆盖/恢复类接口均应由调用方明确确认后使用。

## 认证与安全

### 普通业务认证 A

主要业务路由位于 `/api` 已鉴权路由组。密钥位于 **ABGI运行目录**的 `abgiUser.yaml`：

```yaml
# 请求头中的密钥必须与服务端配置一致，不要把真实密钥写入文档或提交到仓库。

auth:
  api_key: "<ABGI_API_KEY>"
```

标准调用方式：

```http
GET {baseUrl}/api/oneLong/oneLongAllName
apiKey: <ABGI_API_KEY>
```

- 正确且非空的 `apiKey` 请求头优先通过认证，不需要先登录；一般业务接口**不识别** `x-api-key`，也不把 `Authorization: Bearer <API_KEY>` 当作普通密钥认证。
- 普通接口也接受登录返回的JWT：Query `token=<JWT>`、其次 `tk=<JWT>`、最后 Header `Authorization: <JWT>`（原始JWT，**不带Bearer**）。有正确apiKey时不再检查这些JWT；apiKey无效但存在有效JWT时仍可能通过。非空token优先于tk和Authorization，无效token不会自动回退到后者。
- 普通认证失败：HTTP401，application/json，`{"code":401,"errorCode":"UNAUTHORIZED","error":"未授权"}`。
- 使用密钥时身份为配置的本地登录用户，因此个人菜单/偏好等也使用该用户空间；不是多用户子账号权限隔离。
- JWT通过 POST `/api/auth/login` 获取，字段为 `aBgiToken`。当前签发有效期按代码为 **360小时**；SessionActions中的“最近登录”是独立的10分钟限制，不等于JWT有效期。

### 独立认证

| 分类 | 实际要求 | 注意事项 |
|---|---|---|
| 公开 | 无普通apiKey校验 | 登录、版本、部分调试/日志/心跳等；逐接口标记，不表示适合公开公网 |
| 视频 `PlayVideoStream` | Query `tk=<JWT>` | 处理器自己校验，仅传apiKey无效 |
| `/api/session-actions` | 唯一 `Authorization: Bearer <JWT>` | **拒绝apiKey头、token/tk查询参数**；同源、功能开关、强密码、近期登录和回环来源限制见接口详情 |
| MCP认证 M | `/mcp/messages`接受apiKey、x-api-key、Authorization原始密钥或Bearer密钥 | 此处Bearer是API Key，不是JWT；GET/POST `/mcp/sse` 当前未验证密钥 |
| OneBot | 配置OneBot.Secret时 `X-Signature: sha1=<签名>` | 对原始body做HMAC-SHA1；不使用普通apiKey校验 |
| 仓库上传/管理 | 普通认证A，再加仓库授权码或管理员令牌 | ABGI密钥不能替代远端仓库凭据；管理令牌可放X-Repository-Admin-Token，避免与普通JWT冲突 |

源码：`auth/AuthMiddleware.go`、`auth/jwtUtils.go`、`SessionActions/security.go`、`MCP/server.go`、`Notice/OneBot.go`、`abgiRepos/proxy.go`。

### 跨域、浏览器与敏感信息

- 当前CORS允许头为 `Content-Type, Authorization, X-Requested-With`，**未包含apiKey及仓库自定义头**；允许方法也未显式包含PATCH。本文示例默认同源或服务端客户端，不承诺浏览器跨域密钥/PATCH调用成功；此文档任务不修改CORS。
- 浏览器原生WebSocket及EventSource通常不能设置任意请求头；普通认证路由可使用现有JWT token/tk方式，但不能把API Key当作JWT。具体流是否支持此方式以各接口独立认证为准。含令牌Query可能进入访问日志，应控制日志和代理保留范围。
- 密钥具有很强的本机控制能力，请使用自定义密钥、限制访问网络、按需加HTTPS反向代理；不暴露公开日志/截图/配置响应。不要混淆本地API密钥、AI供应商ApiKey、联机加密SecretKey、OneBot签名及仓库凭据。
- 返回配置只对部分字段脱敏，不能把全量 `/api/config` 响应当作可公开数据。本文不读取真实凭据，示例不得用实际密钥、密码或Cookie替换后提交。

## 调用示例

以下为说明性命令，不在本次整理中执行。Windows PowerShell使用 `curl.exe` 避免别名差异。先设置环境变量；JSON写操作示例仅在明确希望启动任务时执行。

```powershell
# 配置当前服务地址与密钥，查询一条龙配置名称。

$baseUrl = 'http://127.0.0.1:8082'
$env:ABGI_API_KEY = '<ABGI_API_KEY>'
curl.exe "$baseUrl/api/oneLong/oneLongAllName" -H "apiKey: $env:ABGI_API_KEY"
```

```http
POST {baseUrl}/api/oneLong/startOneLong
apiKey: <ABGI_API_KEY>
Content-Type: application/json

"每日一条龙"
```

对应说明性响应：

```json
{"status":"success","msg":"启动成功"}
```

```http
POST {baseUrl}/api/startGroups
apiKey: <ABGI_API_KEY>
Content-Type: application/json

["配置组A","配置组B"]
```

```http
POST {baseUrl}/api/taskCron/pause?id=<TASK_ID>
apiKey: <ABGI_API_KEY>
```

```http
POST {baseUrl}/api/auth/login
Content-Type: application/json

{"username":"<USERNAME>","password":"<PASSWORD>"}
```

```json
{"code":200,"aBgiToken":"<JWT>"}
```

SSE示意（MCP握手）与NDJSON示意（JS专家流）是两种不同格式，客户端不可混用解析器：

```text
# MCP SSE：endpoint中的URL由服务器产生；继续保持此连接读取message事件。

event: endpoint
data: http://127.0.0.1:8082/mcp/messages?sessionId=<SESSION_ID>

event: message
data: {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"此材料有10个"}]}}
```

```text
# JS专家NDJSON：每行一个JSON，没有event:或data:前缀。

{"type":"delta","delta":"回答片段"}
{"type":"done"}
```

## 文档目录

- [接入说明](#接入说明)
- [认证与安全](#认证与安全)
- [调用示例](#调用示例)
- [全量接口索引](#api-index)
- [登录与系统信息](#section-01)
- [背包、收益与日志统计](#section-02)
- [BetterGI 配置与 ABGI 设置](#section-03)
- [AI 与技能](#section-04)
- [通知模板与周一记录](#section-05)
- [公开入口、Webhook 与流媒体](#section-06)
- [MCP HTTP 传输与 JSON-RPC](#section-07)
- [运行时、远程桌面、Maa 与会话程序](#section-08)
- [一条龙与脚本文档](#section-09)
- [抽卡记录](#section-10)
- [米游社扫码、角色缓存与旧配置](#section-11)
- [米游社多账号签到](#section-12)
- [战斗策略与角色培养](#section-13)
- [定时任务](#section-14)
- [BetterGI 与 ABGI 更新](#section-15)
- [配置组及地图追踪](#section-16)
- [配置组工作台](#section-17)
- [本地脚本管理与工坊队列](#section-18)
- [绝区零一条龙](#section-19)
- [任务模块数据结构附录](#section-20)
- [版本与更新说明](#section-21)
- [CD 感知采集](#section-22)
- [采集管理与路线包](#section-23)
- [BetterGI 黑名单、日志、OCR 与快捷键](#section-24)
- [矿石与红血统计](#section-25)
- [OBS 录制、回放、推流与媒体](#section-26)
- [团酱多节点代理](#section-27)
- [ABGI 仓库中转](#section-28)
- [WebDAV User 备份与恢复](#section-29)
- [钓鱼与锄地脚本管理](#section-30)
- [集成接口公共数据结构](#section-31)
- [公共数据结构](#section-32)
- [静态资源、OPTIONS 与回退](#section-33)
- [完整性与验证说明](#section-34)

<a id="api-index"></a>

## 全量接口索引

共401个正常显式端点，另1个条件ANY兜底；单击编号跳转详细参数与返回值。URL均在对应详情使用 `{baseUrl}` 完整展开。认证列为导航摘要，功能开关和额外凭据以接口详情为准。

| 编号 | 方法 | 路径 | 认证 | 用途 |
|---|---|---|---|---|
| [001](#api-001) | POST | `/api/auth/login` | 公开 | 使用账号密码登录，获取 JWT；使用 apiKey 的普通业务调用不需要先登录。 |
| [002](#api-002) | GET | `/api/auth/getSystemConfig` | 公开 | 读取登录页标题和功能开关。 |
| [003](#api-003) | GET | `/api/BagStatistics/preference` | apiKey | 读取当前认证用户的背包视图偏好。 |
| [004](#api-004) | PUT | `/api/BagStatistics/preference` | apiKey | 部分更新排序和关注偏好。 |
| [005](#api-005) | GET | `/api/BagStatistics/accounts` | apiKey | 列出真实存在背包扫描记录的账户。 |
| [006](#api-006) | GET | `/api/BagStatistics` | apiKey | 聚合旅行者札记、背包扫描等材料记录，按材料名和时间排序。 |
| [007](#api-007) | GET | `/api/BagStatistics/getBagInfo` | apiKey | 读取米游社背包，按材料分类重新分组。 |
| [008](#api-008) | DELETE | `/api/BagStatistics/DELETE` | apiKey | 取消关注指定材料 |
| [009](#api-009) | POST | `/api/BagStatistics/ADD` | apiKey | 新增关注指定材料 |
| [010](#api-010) | POST | `/api/BagStatistics/CLEAR` | apiKey | 清空全部关注材料 |
| [011](#api-011) | GET | `/api/BagStatistics/Morale` | apiKey | 按日期查询当前米游社角色的摩拉收支记录。 |
| [012](#api-012) | GET | `/api/BagStatistics/MoraleMonsterBook` | apiKey | 统计当前角色累计怪物击杀和摩拉，区分精英及小怪。 |
| [013](#api-013) | GET | `/api/BagStatistics/MoraleIncomeBook` | apiKey | 按日/月/年分类统计收益来源。 |
| [014](#api-014) | POST | `/api/BagStatistics/updateMorale` | apiKey | 从米游社增量更新摩拉收支。 |
| [015](#api-015) | GET | `/api/EatStatistics/accounts` | apiKey | 列出营养袋吃药统计脚本已有账号。 |
| [016](#api-016) | GET | `/api/EatStatistics` | apiKey | 读取指定账号的药品库存快照，不是直接的吃药次数。 |
| [017](#api-017) | GET | `/api/checkBag` | apiKey | 列出库存不少于 8000 的材料，忽略贵重收集物和木材分类。 |
| [018](#api-018) | POST | `/api/deleteBag` | apiKey | 清理历史统计，保留近期数据。 |
| [019](#api-019) | POST | `/api/startGroups` | apiKey | 按给定顺序启动一个或多个配置组。 |
| [020](#api-020) | GET | `/api/getAutoArtifactsPro` | apiKey | 读取狗粮批发脚本记录文件列表及摘要。 |
| [021](#api-021) | GET | `/api/getAutoArtifactsPro2` | apiKey | 读取一个狗粮记录文件的趋势序列。 |
| [022](#api-022) | GET | `/api/logAnalysis` | apiKey | 分析日志材料收益（保留统计次数至少 10 的条目）。 |
| [023](#api-023) | POST | `/api/backup` | apiKey | 备份 BetterGI User 等运行资料。 |
| [024](#api-024) | GET | `/api/gitLog` | apiKey | 查询旧版脚本仓库近三天变更目录。 |
| [025](#api-025) | GET | `/api/gitCommits` | apiKey | 通过远端平台 HTTP 查询真实 Git 提交记录。 |
| [026](#api-026) | GET | `/api/other` | apiKey | 查询日志中配置组耗时及今日配置组状态。 |
| [027](#api-027) | POST | `/api/archive` | apiKey | 提交配置组执行统计，尝试归档并重置日志状态。 |
| [028](#api-028) | GET | `/api/LogAnalysis2Page` | apiKey | 按配置组聚合执行分段、子任务收益和错误。 |
| [029](#api-029) | GET | `/api/LogAnalysis2EfficiencyRoutes` | apiKey | 查询路线效率、耗时与每分钟收益。 |
| [030](#api-030) | GET | `/api/newScriptHouse/jsNames` | apiKey | 对比本地脚本与远端仓库版本。 |
| [031](#api-031) | GET | `/api/newScriptHouse/scripts` | apiKey | 搜索远端可安装脚本。 |
| [032](#api-032) | POST | `/api/newScriptHouse/subscribe` | apiKey | 下载并安装一个脚本。 |
| [033](#api-033) | POST | `/api/newScriptHouse/update/:name` | apiKey | 更新一个已安装脚本。 |
| [034](#api-034) | POST | `/api/newScriptHouse/batchUpdate` | apiKey | 批量更新需要更新的脚本。 |
| [035](#api-035) | POST | `/api/newScriptHouse/delete` | apiKey | 删除指定本地脚本。 |
| [036](#api-036) | GET | `/api/newScriptHouse/history` | apiKey | 读取脚本的本地历史版本（兼容历史文件和目录）。 |
| [037](#api-037) | POST | `/api/newScriptHouse/rollback` | apiKey | 恢复脚本的一个本地历史版本。 |
| [038](#api-038) | GET | `/api/bgiConfig/allConfig` | apiKey | 枚举一条龙配置文件与格式支持状态。 |
| [039](#api-039) | GET | `/api/bgiConfig/findConfig` | apiKey | 读取指定新版一条龙文件、修订值与修改时间。 |
| [040](#api-040) | POST | `/api/bgiConfig/saveConfig` | apiKey | 保存可视化一条龙配置，保留原文件未知字段。 |
| [041](#api-041) | GET | `/api/bgiConfig/options` | apiKey | 读取任务、队伍、副本、首领、角色天赋与各枚举选项。 |
| [042](#api-042) | GET | `/api/bgiConfig/groups` | apiKey | 从指定一条龙排除内置任务，取得配置组。 |
| [043](#api-043) | POST | `/api/bgiConfig/createConfig` | apiKey | 创建一份新的一条龙配置。 |
| [044](#api-044) | POST | `/api/bgiConfig/renameConfig` | apiKey | 重命名一条龙配置。 |
| [045](#api-045) | POST | `/api/bgiConfig/deleteConfig` | apiKey | 删除一条龙配置。 |
| [046](#api-046) | GET | `/api/bgiConfig/autoPick` | apiKey | 读取自动拾取五份名单、模式和运行状态。 |
| [047](#api-047) | PUT | `/api/bgiConfig/autoPick` | apiKey | 保存自动拾取名单或设置。 |
| [048](#api-048) | GET | `/api/config` | apiKey | 读取 ABGI 当前完整设置（部分敏感字段脱敏）。 |
| [049](#api-049) | POST | `/api/saveConfig` | apiKey | 保存 main.json 并应用启动项、RDP、OBS、联机、Maa、AI 等运行设置。 |
| [050](#api-050) | POST | `/api/ai/combat-strategy` | apiKey | 自然语言生成、面板生成、微调或转换 TXT 战斗策略，返回候选但不保存。 |
| [051](#api-051) | POST | `/api/ai/combat-strategy/from-log` | apiKey | 从客户端完整执行日志快照识别四人队伍并生成策略。 |
| [052](#api-052) | POST | `/api/ai/combat-strategy/log-optimization/candidates` | apiKey | 依据日志名称定位当前磁盘的真实 JSON 战斗策略文件。 |
| [053](#api-053) | POST | `/api/ai/combat-strategy/log-optimization/analyze` | apiKey | 对选定策略及日志进行微调分析，生成可确认的覆盖预览。 |
| [054](#api-054) | POST | `/api/ai/combat-strategy/log-optimization/apply` | apiKey | 显式确认后覆盖原 JSON 策略。 |
| [055](#api-055) | POST | `/api/ai/js-script/chat/stream` | apiKey | 只读分析指定本地 JS 脚本的源码、配置及变更信息。 |
| [056](#api-056) | POST | `/api/ai/js-script/chat/reset` | apiKey | 释放脚本对话索引缓存。 |
| [057](#api-057) | POST | `/api/ai/chat/stream` | apiKey | 普通 AI 聊天或工具智能体；命中关键词技能时优先执行技能。 |
| [058](#api-058) | GET | `/api/skill/list` | apiKey | 按关键字或来源类型查询技能。 |
| [059](#api-059) | GET | `/api/skill/detail` | apiKey | 读取单个技能。 |
| [060](#api-060) | POST | `/api/skill/create` | apiKey | 创建一个 AI 技能。 |
| [061](#api-061) | POST | `/api/skill/update` | apiKey | 更新技能可编辑字段。 |
| [062](#api-062) | DELETE | `/api/skill/delete` | apiKey | 删除技能和运行日志。 |
| [063](#api-063) | POST | `/api/skill/toggle` | apiKey | 切换技能启用状态。 |
| [064](#api-064) | POST | `/api/skill/run` | apiKey | 运行指定启用技能并返回完整答案。 |
| [065](#api-065) | POST | `/api/skill/runStream` | apiKey | 运行技能，以纯文本分块输出答案。 |
| [066](#api-066) | GET | `/api/skill/runLogs` | apiKey | 读取最近50条技能运行记录。 |
| [067](#api-067) | DELETE | `/api/skill/clearLogs` | apiKey | 清除一个技能的运行日志。 |
| [068](#api-068) | GET | `/api/skill/export` | apiKey | 导出单技能为 JSON 文件。 |
| [069](#api-069) | GET | `/api/skill/exportAll` | apiKey | 导出全部技能的可移植字段。 |
| [070](#api-070) | POST | `/api/skill/import` | apiKey | 从导出格式导入技能，跳过重名。 |
| [071](#api-071) | GET | `/api/notice/template/context` | apiKey | 读取通知预览上下文及可插入变量。 |
| [072](#api-072) | POST | `/api/notice/template/background` | apiKey | 上传通知背景图并统一转换 PNG。 |
| [073](#api-073) | POST | `/api/notice/template/preview` | apiKey | 用未保存模板生成图片预览。 |
| [074](#api-074) | GET | `/api/autoMonday/records` | apiKey | 读取全自动周一多账户任务及就绪时间。 |
| [075](#api-075) | GET | `/api/test` | 公开 | 读取进程内一条龙进度快照。 |
| [076](#api-076) | GET | `/api/appws` | 公开 | 订阅 APP 心跳和后端广播消息。 |
| [077](#api-077) | GET | `/api/abgiObs/PlayVideoStream` | tk JWT | 播放 OBS 保存目录下的视频。 |
| [078](#api-078) | GET | `/api/aBgiJt` | apiKey | 抓取当前桌面 JPEG。 |
| [079](#api-079) | GET | `/ws/:name` | 公开 | 持续订阅指定 BetterGI 日志文件新增行。 |
| [080](#api-080) | POST | `/bgiWebhook` | 公开 | 接收 BetterGI 事件并推送 Windows 桌面通知。 |
| [081](#api-081) | POST | `/bot/bgiWebhook` | 公开 | 接收任意对象、打印并回显。 |
| [082](#api-082) | POST | `/api/onebot` | OneBot签名 | 接收 OneBot 11 消息并分发机器人命令。 |
| [083](#api-083) | GET | `/api/images` | 公开 | 获取启动时枚举的图片 URL 列表。 |
| [084](#api-084) | GET | `/mcp/sse` | 公开 | 建立 MCP 旧式 SSE 会话；保持长连接接收消息。 |
| [085](#api-085) | POST | `/mcp/sse` | 公开 | 建立 MCP 旧式 SSE 会话；这是相同 SSE 握手处理器，不是 JSON-RPC 消息投递入口。 |
| [086](#api-086) | POST | `/mcp/messages` | MCP密钥 | 向一个现存 SSE 会话提交 JSON-RPC 请求。 |
| [087](#api-087) | GET | `/api/logFiles` | apiKey | 列举BetterGI日志文件名 |
| [088](#api-088) | GET | `/api/logInfo` | apiKey | 读取完整日志文本 |
| [089](#api-089) | GET | `/api/groupLog` | apiKey | 按配置组加载/执行结束切分日志 |
| [090](#api-090) | GET | `/api/oneRemote/sessions` | apiKey | 查询Windows全部多用户会话 |
| [091](#api-091) | GET | `/api/oneRemote/status` | apiKey | 查询本地RDP能力、安全和托管连接状态 |
| [092](#api-092) | GET | `/api/oneRemote/profiles` | apiKey | 列举本地RDP档案（名称排序） |
| [093](#api-093) | POST | `/api/oneRemote/profiles` | apiKey | 创建带加密凭据的RDP档案 |
| [094](#api-094) | PUT | `/api/oneRemote/profiles/:id` | apiKey | 完整更新RDP档案 |
| [095](#api-095) | DELETE | `/api/oneRemote/profiles/:id` | apiKey | 删除RDP档案并移除托管连接 |
| [096](#api-096) | POST | `/api/oneRemote/start` | apiKey | 启动内置RDP档案或兼容1Remote连接 |
| [097](#api-097) | POST | `/api/oneRemote/stop` | apiKey | 停止某个内置RDP连接 |
| [098](#api-098) | POST | `/api/oneRemote/logoff` | apiKey | 注销目标Windows会话并停止匹配档案托管 |
| [099](#api-099) | GET | `/api/oneRemote/desktop/:id/ws` | apiKey | 在网页观看并操作指定内置RDP桌面 |
| [100](#api-100) | GET | `/api/oneRemote/performance` | apiKey | 查询电脑CPU/内存/GPU表现 |
| [101](#api-101) | GET | `/api/homeMenu/favorites` | apiKey | 查询当前用户常用菜单 |
| [102](#api-102) | POST | `/api/homeMenu/favorites` | apiKey | 新增或更新常用菜单并置顶 |
| [103](#api-103) | POST | `/api/homeMenu/favorites/reorder` | apiKey | 重排当前用户常用菜单 |
| [104](#api-104) | DELETE | `/api/homeMenu/favorites` | apiKey | 删除当前用户某个常用菜单 |
| [105](#api-105) | GET | `/api/macro/list` | apiKey | 查询本地宏摘要（创建时间倒序） |
| [106](#api-106) | GET | `/api/macro/detail/:id` | apiKey | 查询宏元信息与完整事件 |
| [107](#api-107) | POST | `/api/macro/startRecord` | apiKey | 开始全局鼠标键盘录制 |
| [108](#api-108) | POST | `/api/macro/stopRecord` | apiKey | 停止当前录制并保存宏 |
| [109](#api-109) | GET | `/api/macro/status` | apiKey | 查询宏录制/回放状态 |
| [110](#api-110) | POST | `/api/macro/play/:id` | apiKey | 异步回放宏 |
| [111](#api-111) | POST | `/api/macro/stopPlay` | apiKey | 终止当前回放 |
| [112](#api-112) | GET | `/api/macro/export/:id` | apiKey | 导出宏包 |
| [113](#api-113) | POST | `/api/macro/import` | apiKey | 导入本地宏包 |
| [114](#api-114) | GET | `/api/macro/preview/:id` | apiKey | 读取宏预览图片 |
| [115](#api-115) | PUT | `/api/macro/:id` | apiKey | 修改宏名称与说明，不改动作 |
| [116](#api-116) | DELETE | `/api/macro/:id` | apiKey | 删除宏及预览 |
| [117](#api-117) | GET | `/api/logKeyword/behaviors` | apiKey | 查询全部日志编排可选行为及参数提示 |
| [118](#api-118) | GET | `/api/logKeyword/rules` | apiKey | 查询日志编排规则与有序动作 |
| [119](#api-119) | POST | `/api/logKeyword/rules` | apiKey | 创建关键词触发规则 |
| [120](#api-120) | PUT | `/api/logKeyword/rules` | apiKey | 完整替换日志编排规则 |
| [121](#api-121) | DELETE | `/api/logKeyword/rules` | apiKey | 删除日志编排规则与关联项 |
| [122](#api-122) | GET | `/api/maa/setup` | apiKey | 获取Maa安装诊断和工作台能力 |
| [123](#api-123) | GET | `/api/maa/images` | apiKey | 列举资源目录下的图片 |
| [124](#api-124) | POST | `/api/maa/images` | apiKey | 上传或覆盖模板图片 |
| [125](#api-125) | DELETE | `/api/maa/images` | apiKey | 删除模板图片 |
| [126](#api-126) | GET | `/api/maa/models` | apiKey | 列举OCR模型目录 |
| [127](#api-127) | GET | `/api/maa/option` | apiKey | 读取Maa基础选项 |
| [128](#api-128) | POST | `/api/maa/option` | apiKey | 保存Maa基础选项并按需应用 |
| [129](#api-129) | GET | `/api/maa/pipelines` | apiKey | 列举所有原生Pipeline JSON文件 |
| [130](#api-130) | GET | `/api/maa/pipeline` | apiKey | 读取原生Pipeline并分析目标依赖 |
| [131](#api-131) | POST | `/api/maa/pipeline` | apiKey | 校验并覆盖保存原生Pipeline |
| [132](#api-132) | DELETE | `/api/maa/pipeline` | apiKey | 删除Pipeline及其项目布局/草稿 |
| [133](#api-133) | POST | `/api/maa/pipeline/copy` | apiKey | 复制Pipeline并重写节点命名空间 |
| [134](#api-134) | GET | `/api/maa/pipeline/export` | apiKey | 以附件下载原生Pipeline JSON |
| [135](#api-135) | GET | `/api/maa/templates` | apiKey | 查询内置新手Pipeline模板 |
| [136](#api-136) | POST | `/api/maa/pipeline/validate` | apiKey | 预检入口、节点引用、资源与常见参数 |
| [137](#api-137) | GET | `/api/maa/project` | apiKey | 读取Pipeline对应编辑器布局 |
| [138](#api-138) | POST | `/api/maa/project` | apiKey | 保存编辑器布局，不改写执行Pipeline |
| [139](#api-139) | GET | `/api/maa/draft` | apiKey | 读取未保存草稿 |
| [140](#api-140) | POST | `/api/maa/draft` | apiKey | 保存未完成Pipeline草稿 |
| [141](#api-141) | DELETE | `/api/maa/draft` | apiKey | 删除指定Pipeline草稿 |
| [142](#api-142) | GET | `/api/maa/status` | apiKey | 查询Maa框架与任务状态 |
| [143](#api-143) | GET | `/api/maa/windows` | apiKey | 枚举Windows桌面窗口 |
| [144](#api-144) | GET | `/api/maa/adbDevices` | apiKey | 枚举ADB设备 |
| [145](#api-145) | POST | `/api/maa/init` | apiKey | 以当前或临时Maa配置初始化控制器 |
| [146](#api-146) | POST | `/api/maa/connection` | apiKey | 连接页面指定的当前窗口或ADB目标 |
| [147](#api-147) | POST | `/api/maa/process/start` | apiKey | 连接目标之前直接启动Windows程序 |
| [148](#api-148) | POST | `/api/maa/targets/scan` | apiKey | 按当前请求条件扫描Windows/ADB候选目标 |
| [149](#api-149) | POST | `/api/maa/targets/preview` | apiKey | 临时查找、激活并截取Windows目标，不替换当前Tasker |
| [150](#api-150) | POST | `/api/maa/targets/activate` | apiKey | 恢复并激活已连接Win32窗口 |
| [151](#api-151) | POST | `/api/maa/run` | apiKey | 执行Maa任务，默认同步等待 |
| [152](#api-152) | POST | `/api/maa/run/async` | apiKey | 将Maa任务放入内存异步队列 |
| [153](#api-153) | GET | `/api/maa/jobs/:id` | apiKey | 读取一个异步任务最新快照 |
| [154](#api-154) | GET | `/api/maa/events` | apiKey | 读取所有任务快照或订阅单任务状态 |
| [155](#api-155) | POST | `/api/maa/stop` | apiKey | 请求停止当前任务或指定异步任务 |
| [156](#api-156) | POST | `/api/maa/shutdown` | apiKey | 释放Maa运行时及控制器 |
| [157](#api-157) | GET | `/api/maa/screenshot` | apiKey | 对当前已连接目标测试截图 |
| [158](#api-158) | GET | `/api/abgiScreen/stream` | apiKey | 持续观看共享编码的本机桌面MJPEG |
| [159](#api-159) | GET | `/api/abgiScreen/status` | apiKey | 查询屏幕流开关、观看数和所选档位质量 |
| [160](#api-160) | GET | `/api/abgiSSE/onlineFallback` | apiKey | 读取联机空房检测设置 |
| [161](#api-161) | PUT | `/api/abgiSSE/onlineFallback` | apiKey | 保存联机空房检测和工坊脚本回退 |
| [162](#api-162) | POST | `/api/abgiSSE/connect/:typeKey` | apiKey | 连接远端联机服务器 |
| [163](#api-163) | POST | `/api/abgiSSE/disconnect` | apiKey | 关闭当前联机连接 |
| [164](#api-164) | GET | `/api/abgiSSE/getOnlineUser` | apiKey | 查询远端联机房间和在线成员 |
| [165](#api-165) | POST | `/api/abgiSSE/HomeownerUpdateGroup` | apiKey | 代理房主修改房间信息 |
| [166](#api-166) | POST | `/api/abgiSSE/HomeownerQueryGroup` | apiKey | 代理房主查询指定房间 |
| [167](#api-167) | POST | `/api/abgiSSE/HomeownerCreateGroup` | apiKey | 代理房主创建房间 |
| [168](#api-168) | GET | `/api/abgiSSE/getRoomLogs` | apiKey | 查询远端当日房间日志并隐藏服务器凭据 |
| [169](#api-169) | POST | `/api/abgiSSE/clearNumberOfLaunches` | apiKey | 清零本机联机发车计数 |
| [170](#api-170) | GET | `/api/abgiSSE/getOnlineStatus` | apiKey | 读取联机状态文本快照 |
| [171](#api-171) | POST | `/api/abgiSSE/sendMessage` | apiKey | 向当前联机WebSocket发送消息 |
| [172](#api-172) | GET | `/api/NumberOfLaunches` | 公开 | 查询本机联机发车次数 |
| [173](#api-173) | GET | `/api/index` | apiKey | 查询首页运行进度摘要 |
| [174](#api-174) | GET | `/api/appInfo` | apiKey | 查询App展示的版本与联机次数 |
| [175](#api-175) | POST | `/api/indexSX` | apiKey | 安排当前实例安全重启 |
| [176](#api-176) | GET | `/api/indexSX` | apiKey | 阻止旧客户端使用GET重启 |
| [177](#api-177) | GET | `/api/archiveList` | apiKey | 查询全部归档记录 |
| [178](#api-178) | DELETE | `/api/archive` | apiKey | 删除一个归档记录 |
| [179](#api-179) | DELETE | `/api/allArchives` | apiKey | 删除全部归档记录 |
| [180](#api-180) | POST | `/api/closeBgi` | apiKey | 关闭BetterGI及原神 |
| [181](#api-181) | POST | `/api/closeYuanShen` | apiKey | 关闭原神 |
| [182](#api-182) | POST | `/api/sendImage` | apiKey | 截图并按当前通知配置发送 |
| [183](#api-183) | GET | `/api/session-actions/status` | Bearer JWT | 查看会话启动功能启用原因及本机模板管理资格 |
| [184](#api-184) | GET | `/api/session-actions/catalog` | Bearer JWT | 查询可从远端启动的已启用模板摘要 |
| [185](#api-185) | GET | `/api/session-actions/runs` | Bearer JWT | 分页查询会话程序启动审计记录 |
| [186](#api-186) | GET | `/api/session-actions/runs/:id` | Bearer JWT | 读取一次会话程序启动状态 |
| [187](#api-187) | POST | `/api/session-actions/runs` | Bearer JWT | 在指定本地RDP档案账户中异步启动已登记模板 |
| [188](#api-188) | GET | `/api/session-actions/templates` | Bearer JWT | 在本机查询全部会话程序模板 |
| [189](#api-189) | POST | `/api/session-actions/templates` | Bearer JWT | 在本机创建固定可执行模板 |
| [190](#api-190) | GET | `/api/session-actions/templates/:id` | Bearer JWT | 在本机读取完整模板 |
| [191](#api-191) | PUT | `/api/session-actions/templates/:id` | Bearer JWT | 完整更新固定可执行模板 |
| [192](#api-192) | DELETE | `/api/session-actions/templates/:id` | Bearer JWT | 在本机删除会话程序模板 |
| [193](#api-193) | POST | `/api/oneLong/startOneLong` | apiKey | 启动指定一条龙。 |
| [194](#api-194) | POST | `/api/oneLong/continueOneLong` | apiKey | 从原生NextTaskId书签继续一条龙。 |
| [195](#api-195) | GET | `/api/oneLong/groupProgress` | apiKey | 从最近运行日志查询组完成情况。 |
| [196](#api-196) | GET | `/api/oneLong/oneLongAllName` | apiKey | 列出一条龙配置名称。 |
| [197](#api-197) | GET | `/api/md` | apiKey | 读取仓库脚本README或战斗/七圣召唤文档。 |
| [198](#api-198) | POST | `/api/gacha/sync` | apiKey | 从本地缓存或祈愿链接增量同步记录。 |
| [199](#api-199) | GET | `/api/gacha/accounts` | apiKey | 查询本地已同步账号。 |
| [200](#api-200) | GET | `/api/gacha/summary` | apiKey | 查询UID抽卡统计及保底。 |
| [201](#api-201) | GET | `/api/gacha/records` | apiKey | 分页查询抽卡明细。 |
| [202](#api-202) | POST | `/api/mysSignIn` | apiKey | 旧兼容入口异步签到所有启用账号。 |
| [203](#api-203) | GET | `/api/ReadCk` | apiKey | 读取BetterGI配置的米游社CK。 |
| [204](#api-204) | GET | `/api/MysCharacterInfo` | apiKey | 读取角色面板缓存。 |
| [205](#api-205) | POST | `/api/MysCharacterInfo/Refresh` | apiKey | 从米游社刷新角色面板。 |
| [206](#api-206) | POST | `/api/MysConfig/FillLogin` | apiKey | 写入签到CK/SK并同步BetterGI CK。 |
| [207](#api-207) | POST | `/api/MysConfig/FillCookie` | apiKey | 只更新扫码CK并保留SK。 |
| [208](#api-208) | POST | `/api/MysConfig/FillBetterGICookie` | apiKey | 只更新BetterGI CK。 |
| [209](#api-209) | POST | `/api/MysConfig/FillCheckin` | apiKey | 保存旧工具签到选项。 |
| [210](#api-210) | POST | `/api/Qrcode` | apiKey | 申请扫码登录二维码。 |
| [211](#api-211) | GET | `/api/Check` | apiKey | 轮询扫码登录状态。 |
| [212](#api-212) | GET | `/api/mys-sign/accounts` | apiKey | 列出签到账号及最近运行。 |
| [213](#api-213) | POST | `/api/mys-sign/accounts` | apiKey | 创建独立签到账号。 |
| [214](#api-214) | PATCH | `/api/mys-sign/accounts/:id` | apiKey | 部分更新账号设置。 |
| [215](#api-215) | PUT | `/api/mys-sign/accounts/:id/credentials` | apiKey | 替换签到账号凭据。 |
| [216](#api-216) | DELETE | `/api/mys-sign/accounts/:id` | apiKey | 删除签到账号。 |
| [217](#api-217) | GET | `/api/mys-sign/accounts/:id/logs` | apiKey | 读取账号最近10条日志。 |
| [218](#api-218) | POST | `/api/mys-sign/runs` | apiKey | 异步执行指定或全部启用账号。 |
| [219](#api-219) | GET | `/api/mys-sign/runs/:batchId` | apiKey | 查询批次及各账号执行状态。 |
| [220](#api-220) | GET | `/api/autoFightStrategy/characters` | apiKey | 查询内置策略角色。 |
| [221](#api-221) | GET | `/api/autoFightStrategy/universal-json` | apiKey | 构造内置万能JSON策略。 |
| [222](#api-222) | GET | `/api/autoFightStrategy/candidates` | apiKey | 查询单角色策略候选。 |
| [223](#api-223) | GET | `/api/autoFightStrategy/generated` | apiKey | 读取已生成策略。 |
| [224](#api-224) | POST | `/api/autoFightStrategy/generate` | apiKey | 校验并覆盖保存策略。 |
| [225](#api-225) | GET | `/api/characterPlan/list` | apiKey | 查询培养计划列表。 |
| [226](#api-226) | GET | `/api/characterPlan/detail` | apiKey | 查询培养计划详情。 |
| [227](#api-227) | POST | `/api/characterPlan/add` | apiKey | 新增培养计划并同步脚本。 |
| [228](#api-228) | POST | `/api/characterPlan/update` | apiKey | 修改培养计划及配置组绑定。 |
| [229](#api-229) | DELETE | `/api/characterPlan/delete` | apiKey | 删除培养计划。 |
| [230](#api-230) | POST | `/api/characterPlan/run` | apiKey | 计算并应用培养计划。 |
| [231](#api-231) | GET | `/api/characterPlan/characters` | apiKey | 从最近面板构建角色选择列表。 |
| [232](#api-232) | GET | `/api/characterPlan/info` | apiKey | 查询角色等级、天赋及材料缺口。 |
| [233](#api-233) | GET | `/api/taskCron/list` | apiKey | 列出定时任务。 |
| [234](#api-234) | POST | `/api/taskCron/add` | apiKey | 新增定时任务。 |
| [235](#api-235) | POST | `/api/taskCron/remove` | apiKey | 删除定时任务。 |
| [236](#api-236) | POST | `/api/taskCron/pause` | apiKey | 暂停定时任务。 |
| [237](#api-237) | POST | `/api/taskCron/resume` | apiKey | 恢复定时任务。 |
| [238](#api-238) | GET | `/api/taskCron/getTasks` | apiKey | 列出可调度任务和参数说明。 |
| [239](#api-239) | GET | `/api/taskCron/scriptQueues` | apiKey | 列出可定时执行的持久化脚本队列。 |
| [240](#api-240) | POST | `/api/taskCron/update` | apiKey | 修改已启动任务的表达式/参数。 |
| [241](#api-241) | POST | `/api/taskCron/AtOnceRun` | apiKey | 立即执行注册任务。 |
| [242](#api-242) | POST | `/api/UpdateBgi/Upload` | apiKey | 上传压缩包执行BetterGI更新。 |
| [243](#api-243) | POST | `/api/UpdateBgi/Download` | apiKey | 启动或复用在线BGI更新任务。 |
| [244](#api-244) | GET | `/api/UpdateBgi/Download` | apiKey | 订阅BGI更新进度，必要时也会发起更新。 |
| [245](#api-245) | GET | `/api/UpdateBgi/DownloadStatus` | apiKey | 查询在线更新状态快照。 |
| [246](#api-246) | POST | `/api/aBgiUpdate/GetLastVersion` | apiKey | 查询ABGI最新版本。 |
| [247](#api-247) | POST | `/api/aBgiUpdate/Update` | apiKey | 更新ABGI并安排重启。 |
| [248](#api-248) | POST | `/api/scriptGroup/UpdatePathing` | apiKey | 替换地图追踪并更新配置组。 |
| [249](#api-249) | POST | `/api/scriptGroup/UpdatePathingNoConfig` | apiKey | 只替换地图追踪目录。 |
| [250](#api-250) | POST | `/api/scriptGroup/cleanAllPathing` | apiKey | 移除指定组中某目录的地图追踪项目。 |
| [251](#api-251) | GET | `/api/scriptGroup/ConfigPathing` | apiKey | 重新扫描并返回地图更新绑定。 |
| [252](#api-252) | POST | `/api/scriptGroup/savePathing` | apiKey | 保存地图追踪更新绑定。 |
| [253](#api-253) | GET | `/api/scriptGroup/listGroups` | apiKey | 递归列出本地配置组名。 |
| [254](#api-254) | POST | `/api/scriptGroup/createGroup` | apiKey | 创建空白配置组。 |
| [255](#api-255) | GET | `/api/scriptGroup/localJsScripts` | apiKey | 查询本地JS脚本，不更新仓库。 |
| [256](#api-256) | GET | `/api/scriptGroup/localPathingFiles` | apiKey | 递归列出本地地图JSON。 |
| [257](#api-257) | GET | `/api/scriptGroup/listAllGroups` | apiKey | 查询远端仓库地图目录树。 |
| [258](#api-258) | GET | `/api/scriptGroup/listPathingUpdatePaths` | apiKey | 重新扫描并保存地图更新目录绑定。 |
| [259](#api-259) | POST | `/api/scriptGroup/groupDetail` | apiKey | 读取项目、组设置、标签与并发版本。 |
| [260](#api-260) | GET | `/api/scriptGroup/configOptions` | apiKey | 读取内置和本地战斗策略选项。 |
| [261](#api-261) | POST | `/api/scriptGroup/saveProjectSettings` | apiKey | 增量原子保存项目与组设置。 |
| [262](#api-262) | GET | `/api/scriptGroup/workspace` | apiKey | 查询当前用户工作台。 |
| [263](#api-263) | PUT | `/api/scriptGroup/workspace/groupPreference` | apiKey | 保存单组收藏/排序。 |
| [264](#api-264) | POST | `/api/scriptGroup/workspace/groupOrder` | apiKey | 保存组显示顺序。 |
| [265](#api-265) | PUT | `/api/scriptGroup/workspace/groupCategories` | apiKey | 批量修改组分类。 |
| [266](#api-266) | POST | `/api/scriptGroup/workspace/collections` | apiKey | 创建分类/快捷组合。 |
| [267](#api-267) | PUT | `/api/scriptGroup/workspace/collections/:id` | apiKey | 修改集合及完整成员序列。 |
| [268](#api-268) | DELETE | `/api/scriptGroup/workspace/collections/:id` | apiKey | 删除当前用户集合。 |
| [269](#api-269) | POST | `/api/scriptGroup/workspace/collections/reorder` | apiKey | 修改同类集合顺序。 |
| [270](#api-270) | PUT | `/api/scriptGroup/workspace/settings` | apiKey | 合并保存动态工作台设置。 |
| [271](#api-271) | POST | `/api/scriptGroup/workspace/migrate` | apiKey | 迁移旧快捷组合和启动时间。 |
| [272](#api-272) | GET | `/api/scriptManagement/list` | apiKey | 查询全部脚本。 |
| [273](#api-273) | GET | `/api/scriptManagement/detail` | apiKey | 查询脚本详情。 |
| [274](#api-274) | POST | `/api/scriptManagement/create` | apiKey | 创建脚本记录。 |
| [275](#api-275) | POST | `/api/scriptManagement/update` | apiKey | 完整更新脚本记录。 |
| [276](#api-276) | DELETE | `/api/scriptManagement/delete` | apiKey | 删除脚本记录。 |
| [277](#api-277) | POST | `/api/scriptManagement/run` | apiKey | 后台启动单脚本。 |
| [278](#api-278) | POST | `/api/scriptManagement/queue` | apiKey | 创建并启动一次性脚本队列。 |
| [279](#api-279) | GET | `/api/scriptManagement/queue/status` | apiKey | 查询运行队列状态。 |
| [280](#api-280) | POST | `/api/scriptManagement/queue/abort` | apiKey | 请求中止运行队列。 |
| [281](#api-281) | GET | `/api/scriptManagement/queues` | apiKey | 查询持久化队列列表。 |
| [282](#api-282) | GET | `/api/scriptManagement/queue/detail` | apiKey | 查询持久化队列详情。 |
| [283](#api-283) | POST | `/api/scriptManagement/queue/create` | apiKey | 保存可复用队列。 |
| [284](#api-284) | POST | `/api/scriptManagement/queue/update` | apiKey | 更新可复用队列。 |
| [285](#api-285) | DELETE | `/api/scriptManagement/queue/delete` | apiKey | 删除持久化队列。 |
| [286](#api-286) | POST | `/api/scriptManagement/queue/run` | apiKey | 启动持久化队列。 |
| [287](#api-287) | GET | `/api/zzz-one-dragon/config` | apiKey | 读取独立ZZZ配置。 |
| [288](#api-288) | POST | `/api/zzz-one-dragon/config` | apiKey | 校验并保存独立ZZZ配置。 |
| [289](#api-289) | POST | `/api/zzz-one-dragon/detect` | apiKey | 只读检测待保存安装。 |
| [290](#api-290) | GET | `/api/zzz-one-dragon/instances` | apiKey | 读取已配置目录官方实例。 |
| [291](#api-291) | POST | `/api/zzz-one-dragon/start` | apiKey | 异步启动官方一条龙。 |
| [292](#api-292) | POST | `/api/zzz-one-dragon/stop` | apiKey | 停止指定受管运行。 |
| [293](#api-293) | GET | `/api/zzz-one-dragon/status` | apiKey | 查询最近运行和存储状态。 |
| [294](#api-294) | GET | `/api/zzz-one-dragon/logs` | apiKey | 游标查询指定运行日志。 |
| [295](#api-295) | ANY | `/api/zzz-one-dragon/*path` | apiKey | 仅ZZZ模块初始化失败时注册的兜底，不与正常8个接口同时存在，不计额外正常业务接口。 |
| [296](#api-296) | GET | `/api/aBgiUpdate/version` | 公开 | 查询当前 ABGI 版本及运行实例，用于确认重启是否完成 |
| [297](#api-297) | GET | `/api/aBgiUpdate/GetBgiVersion` | 公开 | 查询当前与最新 BetterGI 版本 |
| [298](#api-298) | GET | `/api/updateNotes` | 公开 | 读取内置更新说明 |
| [299](#api-299) | GET | `/api/CD-Aware-AutoGather/ReadInfo` | apiKey | 按账户读取采集路线 CD |
| [300](#api-300) | POST | `/api/CD-Aware-AutoGather/CDAllMaterial` | apiKey | 提取 CD 采集材料并加入背包关注列表 |
| [301](#api-301) | POST | `/api/CD-Aware-AutoGather/UpdateAllCD` | apiKey | 将本地脚本仓库的更新同步到 CD 管理对应路线目录 |
| [302](#api-302) | GET | `/api/CDCollectionManagement/AllUserFile` | apiKey | 枚举采集记录账户 |
| [303](#api-303) | GET | `/api/CDCollectionManagement/list` | apiKey | 读取账户路线树及运行记录 |
| [304](#api-304) | GET | `/api/CDCollectionManagement/ReadPickup` | apiKey | 读取账户拾取统计记录 |
| [305](#api-305) | GET | `/api/CDCollectionManagement/materialRoutes` | apiKey | 按材料分析命中路线和效率 |
| [306](#api-306) | GET | `/api/CDCollectionManagement/backpackMaterial` | apiKey | 从材料文件夹名识别材料并查背包数量 |
| [307](#api-307) | GET | `/api/CDCollectionManagement/cookingRecipes` | apiKey | 读取料理与食材目录 |
| [308](#api-308) | POST | `/api/CDCollectionManagement/addCookingRoutes` | apiKey | 按料理份数计算食材并安装可采集材料路线 |
| [309](#api-309) | POST | `/api/CDCollectionManagement/createFolder` | apiKey | 在采集路线目录内创建文件夹 |
| [310](#api-310) | POST | `/api/CDCollectionManagement/createScript` | apiKey | 将远端地图追踪路线加入采集目录 |
| [311](#api-311) | POST | `/api/CDCollectionManagement/deleteNode` | apiKey | 删除采集目录中的文件或节点 |
| [312](#api-312) | POST | `/api/CDCollectionManagement/deleteRoutes` | apiKey | 批量删除路线并返回逐项结果 |
| [313](#api-313) | POST | `/api/CDCollectionManagement/toggleRoutes` | apiKey | 批量禁用或恢复路线 |
| [314](#api-314) | POST | `/api/CDCollectionManagement/resetCooldown` | apiKey | 清除指定账户的选中路线冷却时间 |
| [315](#api-315) | POST | `/api/CDCollectionManagement/exportRoutes` | apiKey | 导出选中路线的标准 ZIP 包 |
| [316](#api-316) | POST | `/api/CDCollectionManagement/importRoutes` | apiKey | 将标准路线包导入采集目录 |
| [317](#api-317) | GET | `/api/CDCollectionManagement/blacklists` | apiKey | 读取账户材料黑名单及并发版本 |
| [318](#api-318) | POST | `/api/CDCollectionManagement/blacklists` | apiKey | 新增账户黑名单条目 |
| [319](#api-319) | PUT | `/api/CDCollectionManagement/blacklists` | apiKey | 修改账户黑名单条目 |
| [320](#api-320) | DELETE | `/api/CDCollectionManagement/blacklists` | apiKey | 删除账户黑名单条目 |
| [321](#api-321) | GET | `/api/betterGi/blackList` | apiKey | 读取 BetterGI 旧版拾取黑名单 |
| [322](#api-322) | POST | `/api/betterGi/addBlackList` | apiKey | 追加 BetterGI 拾取黑名单 |
| [323](#api-323) | POST | `/api/betterGi/deleteBlackList` | apiKey | 删除单个 BetterGI 拾取黑名单项 |
| [324](#api-324) | GET | `/api/autoLog` | apiKey | 按日期读取 ABGI 日志 |
| [325](#api-325) | POST | `/api/ocr/dogFood` | apiKey | 通过百度 OCR 识别狗粮截图并记录数量 |
| [326](#api-326) | GET | `/api/HotKeyConfig/HotKeyQuery` | apiKey | 读取 BetterGI 系统快捷键 |
| [327](#api-327) | POST | `/api/HotKeyConfig/PressKey` | apiKey | 向当前桌面发送按键 |
| [328](#api-328) | GET | `/api/HotKeyConfig/UserHotKeys` | apiKey | 读取当前认证用户自定义快捷键 |
| [329](#api-329) | POST | `/api/HotKeyConfig/UserHotKeys` | apiKey | 创建或更新当前用户自定义快捷键 |
| [330](#api-330) | DELETE | `/api/HotKeyConfig/UserHotKeys` | apiKey | 删除当前用户自定义快捷键 |
| [331](#api-331) | POST | `/api/js/screenShot` | apiKey | 截取桌面并按坐标裁剪保存 |
| [332](#api-332) | GET | `/api/js/QueryHistoryVersion` | apiKey | 查询脚本历史备份版本 |
| [333](#api-333) | POST | `/api/js/BgiRollbackHistoryVersion` | apiKey | 回滚 BetterGI 历史版本并重启 |
| [334](#api-334) | GET | `/api/abundantOre/dashboard` | apiKey | 查看矿石路线可挖状态与数量 |
| [335](#api-335) | GET | `/api/redBlood/stats` | apiKey | 按来源统计近七天回血与死亡 |
| [336](#api-336) | GET | `/api/abgiObs/Status` | apiKey | 查询 OBS 聚合连接与运行状态 |
| [337](#api-337) | GET | `/api/abgiObs/Inventory` | apiKey | 枚举 OBS 场景、来源和音频输入 |
| [338](#api-338) | POST | `/api/abgiObs/TestConnection` | apiKey | 临时测试 OBS 连接，不保存密码 |
| [339](#api-339) | POST | `/api/abgiObs/StartRecording` | apiKey | 开始 OBS 录制 |
| [340](#api-340) | POST | `/api/abgiObs/StopRecording` | apiKey | 停止录制并可选重命名 |
| [341](#api-341) | GET | `/api/abgiObs/IsRecording` | apiKey | 读取录制状态 |
| [342](#api-342) | POST | `/api/abgiObs/PauseRecording` | apiKey | 暂停正在进行的录制 |
| [343](#api-343) | POST | `/api/abgiObs/ResumeRecording` | apiKey | 恢复暂停的录制 |
| [344](#api-344) | POST | `/api/abgiObs/SplitRecording` | apiKey | 切换到新的录制分段 |
| [345](#api-345) | POST | `/api/abgiObs/StartReplayBuffer` | apiKey | 启动 OBS 回放缓冲 |
| [346](#api-346) | POST | `/api/abgiObs/StopReplayBuffer` | apiKey | 停止 OBS 回放缓冲 |
| [347](#api-347) | POST | `/api/abgiObs/StopStream` | apiKey | 停止 OBS 推流 |
| [348](#api-348) | POST | `/api/abgiObs/CreateRecordChapter` | apiKey | 添加录制章节标记 |
| [349](#api-349) | GET | `/api/abgiObs/GetReplayBufferStatus` | apiKey | 查询回放缓冲是否运行 |
| [350](#api-350) | POST | `/api/abgiObs/SaveReplayBuffer` | apiKey | 保存回放缓冲片段 |
| [351](#api-351) | POST | `/api/abgiObs/StartStream` | apiKey | 开始 OBS 推流 |
| [352](#api-352) | GET | `/api/abgiObs/StartStream` | apiKey | 开始 OBS 推流（旧版 GET 兼容入口） |
| [353](#api-353) | GET | `/api/abgiObs/GetStreamStatus` | apiKey | 查询推流运行指标 |
| [354](#api-354) | POST | `/api/abgiObs/SetProgramScene` | apiKey | 切换当前节目场景 |
| [355](#api-355) | POST | `/api/abgiObs/SetSceneItemEnabled` | apiKey | 控制场景来源显示或隐藏 |
| [356](#api-356) | POST | `/api/abgiObs/SetInputMute` | apiKey | 切换音频输入静音 |
| [357](#api-357) | POST | `/api/abgiObs/SetInputVolume` | apiKey | 设置音频输入音量 |
| [358](#api-358) | POST | `/api/abgiObs/SaveSourceScreenshot` | apiKey | 把 OBS 指定来源保存为 PNG |
| [359](#api-359) | GET | `/api/abgiObs/GetVideoInfo` | apiKey | 枚举录制目录中的视频文件 |
| [360](#api-360) | POST | `/api/abgiObs/DeleteVideo` | apiKey | 删除单个录制视频 |
| [361](#api-361) | POST | `/api/abgiObs/DeleteAllVideo` | apiKey | 清空录制目录视频 |
| [362](#api-362) | GET | `/api/abgiTeam/connections` | apiKey | 读取全部已保存节点 |
| [363](#api-363) | POST | `/api/abgiTeam/connections` | apiKey | 添加远端节点并立即检测 |
| [364](#api-364) | PUT | `/api/abgiTeam/connections/:id` | apiKey | 更新节点并重新检测 |
| [365](#api-365) | PATCH | `/api/abgiTeam/connections/:id/enabled` | apiKey | 启用或停用节点 |
| [366](#api-366) | DELETE | `/api/abgiTeam/connections/:id` | apiKey | 删除节点连接 |
| [367](#api-367) | POST | `/api/abgiTeam/connections/:id/test` | apiKey | 主动检测远端节点 |
| [368](#api-368) | GET | `/api/abgiTeam/connections/:id/status` | apiKey | 查询远端运行进度白名单字段 |
| [369](#api-369) | GET | `/api/abgiTeam/connections/:id/catalog` | apiKey | 读取远端节点概览、一条龙与配置组目录 |
| [370](#api-370) | GET | `/api/abgiTeam/connections/:id/hotkeys` | apiKey | 读取远端系统及自定义快捷键 |
| [371](#api-371) | POST | `/api/abgiTeam/connections/:id/hotkeys/press` | apiKey | 向远端发送按键 |
| [372](#api-372) | POST | `/api/abgiTeam/actions/one-long/start` | apiKey | 批量启动选定节点的一条龙 |
| [373](#api-373) | GET | `/api/abgiTeam/connections/:id/managed-scripts` | apiKey | 枚举远端工坊可启动脚本 |
| [374](#api-374) | POST | `/api/abgiTeam/actions/quick` | apiKey | 批量执行白名单快捷操作 |
| [375](#api-375) | POST | `/api/abgiTeam/actions/script-groups/start` | apiKey | 批量在节点启动多个配置组 |
| [376](#api-376) | GET | `/api/abgiTeam/connections/:id/screenshot` | apiKey | 读取远端桌面截图 |
| [377](#api-377) | GET | `/api/abgiTeam/connections/:id/logs/ws` | apiKey | 建立远端最新日志中转 WebSocket |
| [378](#api-378) | GET | `/api/abgiRepos/categories` | apiKey | 获取仓库分类 |
| [379](#api-379) | GET | `/api/abgiRepos/tree` | apiKey | 获取完整分类与文件树 |
| [380](#api-380) | GET | `/api/abgiRepos/files/:category/*path` | apiKey | 下载远端仓库文件 |
| [381](#api-381) | POST | `/api/abgiRepos/subscriptions` | apiKey | 订阅仓库文件或目录并安装到本地 |
| [382](#api-382) | POST | `/api/abgiRepos/subscribe` | apiKey | 订阅仓库文件或目录并安装到本地（兼容别名） |
| [383](#api-383) | POST | `/api/abgiRepos/upload` | apiKey＋授权码 | 凭仓库授权码上传文件或批量文件 |
| [384](#api-384) | POST | `/api/abgiRepos/admin/login` | apiKey | 登录远端仓库管理员 |
| [385](#api-385) | POST | `/api/abgiRepos/admin/codes` | apiKey | 创建限时或永久上传授权码 |
| [386](#api-386) | GET | `/api/abgiRepos/admin/codes` | apiKey | 读取仓库授权码列表 |
| [387](#api-387) | DELETE | `/api/abgiRepos/admin/codes/:id` | apiKey | 撤销仓库授权码 |
| [388](#api-388) | DELETE | `/api/abgiRepos/admin/files/:category/*path` | apiKey | 删除仓库文件或目录 |
| [389](#api-389) | POST | `/api/webdav/test` | apiKey | 测试已配置的WebDAV连接 |
| [390](#api-390) | POST | `/api/webdav/upload` | apiKey | 将当前BetterGI User压缩备份到WebDAV |
| [391](#api-391) | POST | `/api/webdav/download` | apiKey | 下载WebDAV备份并恢复本机User |
| [392](#api-392) | GET | `/api/autoFishingTeyvat/dashboard` | apiKey | 读取鱼点昼夜冷却与可兑换材料看板 |
| [393](#api-393) | GET | `/api/autoFishingTeyvat/configurations` | apiKey | 列出已有钓鱼配置组实例 |
| [394](#api-394) | GET | `/api/autoFishingTeyvat/settings` | apiKey | 读取一个钓鱼实例的设置和并发版本 |
| [395](#api-395) | PUT | `/api/autoFishingTeyvat/settings` | apiKey | 按增量安全保存钓鱼实例设置 |
| [396](#api-396) | GET | `/api/autoHoeingOneDragon/dashboard` | apiKey | 读取锄地路线、怪物、收益与账户设置看板 |
| [397](#api-397) | PUT | `/api/autoHoeingOneDragon/blacklist` | apiKey | 立即保存锄地账户黑名单 |
| [398](#api-398) | GET | `/api/autoHoeingOneDragon/pickupItems` | apiKey | 枚举可复制与已保留的拾取模板 |
| [399](#api-399) | POST | `/api/autoHoeingOneDragon/pickupItems` | apiKey | 从源目录复制一个拾取模板 |
| [400](#api-400) | DELETE | `/api/autoHoeingOneDragon/pickupItems` | apiKey | 删除保留目录中的拾取模板 |
| [401](#api-401) | GET | `/api/autoHoeingOneDragon/generator/options` | apiKey | 读取调试路线生成器的组、默认参数和动态字段 |
| [402](#api-402) | POST | `/api/autoHoeingOneDragon/generator/generate` | apiKey | 原生生成锄地调试路线与分配报告 |

<a id="section-01"></a>

## 登录与系统信息

源码：`auth/LoginService.go`、`auth/loginMapper.go`。

<a id="api-001"></a>

### POST /api/auth/login

- URL：`{baseUrl}/api/auth/login`。
- 用途：使用账号密码登录，获取 JWT；使用 apiKey 的普通业务调用不需要先登录。
- 认证：公开；凭据通过请求体验证。
- 请求格式：application/json。
- 参数：JSON `username:string`、`password:string`：均用于匹配本地账号，业务上必填。
- 返回：HTTP 200，application/json；`{code:200,aBgiToken:string}`，aBgiToken 是 JWT 字符串。
- 错误：400 `{code:400,errorCode:"INVALID_REQUEST",error:string}`；401 USER_NOT_FOUND/PASSWORD_INCORRECT/TOKEN_GENERATION_FAILED；500 INTERNAL_SERVER_ERROR。

<a id="api-002"></a>

### GET /api/auth/getSystemConfig

- URL：`{baseUrl}/api/auth/getSystemConfig`。
- 用途：读取登录页标题和功能开关。
- 认证：公开。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{code:200,systemName:string,rdpEnabled:boolean,sessionActionsEnabled:boolean}`；systemName 含 `のABGI` 后缀。
- 错误：处理器没有显式业务失败分支。


<a id="section-02"></a>

## 背包、收益与日志统计

源码：`menu/routes_statistics.go`、`BackpackStatistics/Preference.go`、`task/RankService.go`、`warehouse/new_script_house.go`。

<a id="api-003"></a>

### GET /api/BagStatistics/preference

- URL：`{baseUrl}/api/BagStatistics/preference`。
- 用途：读取当前认证用户的背包视图偏好。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{code:200,data:BagViewPreference}`；没有记录时 sortMode 为 custom，两组列表均为空数组。
- 错误：500 `{code:500,error:string}`。

<a id="api-004"></a>

### PUT /api/BagStatistics/preference

- URL：`{baseUrl}/api/BagStatistics/preference`。
- 用途：部分更新排序和关注偏好。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `sortMode?:string`（recent/change/name/custom）、`materialNames?:string[]`（自定义排序）、`followedMaterialKeys?:string[]`（来源及材料名组成的稳定 key）；至少提供一项；列表清理空白、去空、按首次顺序去重。
- 返回：HTTP 200，application/json；`{code:200,data:BagViewPreference}`，返回保存后的完整偏好。
- 错误：400 无效 JSON/未提供字段/无效排序；500 `{code:500,error:string}`。
- 副作用：写入当前用户的数据库偏好，不改变材料库存。

<a id="api-005"></a>

### GET /api/BagStatistics/accounts

- URL：`{baseUrl}/api/BagStatistics/accounts`。
- 用途：列出真实存在背包扫描记录的账户。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{accounts:[{account:string,label:string}],defaultAccount:string|null}`；account 空字符串代表旧版根目录；无记录时 defaultAccount=null。
- 错误：500 `{message:string,error:string}`。

<a id="api-006"></a>

### GET /api/BagStatistics

- URL：`{baseUrl}/api/BagStatistics`。
- 用途：聚合旅行者札记、背包扫描等材料记录，按材料名和时间排序。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `bagAccount?:string`；未传按账户列表默认顺序选取，显式空值 `bagAccount=` 代表旧版根账户；参数只能出现一次，必须是已枚举的安全目录名。
- 返回：HTTP 200，application/json；`MaterialRecord[]`；无数据时可返回一条 Data/Cl/Num 均为“没有数据”的占位记录。
- 错误：400 无法解码/重复/非法账户；404 账户记录已移除；500 `{message:string}`。
- 副作用：兼容历史关注配置时，底层查询可能迁移关注记录并更新 main.json；不可视为完全无副作用的 GET。

<a id="api-007"></a>

### GET /api/BagStatistics/getBagInfo

- URL：`{baseUrl}/api/BagStatistics/getBagInfo`。
- 用途：读取米游社背包，按材料分类重新分组。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数；依赖已配置的米游社账号。
- 返回：HTTP 200，application/json；直接返回 `map<string,ChildNode[]>`，键为材料类别，非统一 data 外壳；ChildNode 字段保留大写。
- 错误：处理器忽略 GetBagInfo 错误，可能返回空数据；鉴权失败见公共规则。
- 副作用：可能更新背包缓存或访问米游社。

<a id="api-008"></a>

### DELETE /api/BagStatistics/DELETE

- URL：`{baseUrl}/api/BagStatistics/DELETE`。
- 用途：取消关注指定材料
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `name:string`：材料名称；处理器未作非空校验，调用方应传入实际材料名。
- 返回：HTTP 200，application/json；正常 `{code:200,msg:"ok"}`。
- 错误：数据库失败仍使用 HTTP 200，先写 `{code:400,msg:string}`；当前实现未 return，又追加成功 JSON，异常时响应可能是两个拼接 JSON，不可仅用 HTTP 状态判断成功。
- 副作用：修改本地关注材料表；CLEAR 为全量删除。

<a id="api-009"></a>

### POST /api/BagStatistics/ADD

- URL：`{baseUrl}/api/BagStatistics/ADD`。
- 用途：新增关注指定材料
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `name:string`：材料名称；处理器未作非空校验，调用方应传入实际材料名。
- 返回：HTTP 200，application/json；正常 `{code:200,msg:"ok"}`。
- 错误：数据库失败仍使用 HTTP 200，先写 `{code:400,msg:string}`；当前实现未 return，又追加成功 JSON，异常时响应可能是两个拼接 JSON，不可仅用 HTTP 状态判断成功。
- 副作用：修改本地关注材料表；CLEAR 为全量删除。

<a id="api-010"></a>

### POST /api/BagStatistics/CLEAR

- URL：`{baseUrl}/api/BagStatistics/CLEAR`。
- 用途：清空全部关注材料
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；正常 `{code:200,msg:"ok"}`。
- 错误：数据库失败仍使用 HTTP 200，先写 `{code:400,msg:string}`；当前实现未 return，又追加成功 JSON，异常时响应可能是两个拼接 JSON，不可仅用 HTTP 状态判断成功。
- 副作用：修改本地关注材料表；CLEAR 为全量删除。

<a id="api-011"></a>

### GET /api/BagStatistics/Morale

- URL：`{baseUrl}/api/BagStatistics/Morale`。
- 用途：按日期查询当前米游社角色的摩拉收支记录。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `date:string` 必填（日 YYYY-MM-DD、月 YYYY-MM、年 YYYY）；`type?:string` 默认 day，可选 day/today/month/year（today 等同 day，不自动补 date）；`detail?:string` 为 1 或 true 时读取明细。
- 返回：HTTP 200，application/json；`{data:{target_date:string,total_morale:integer,items:MoraleRecord[]}}`。
- 错误：400 缺账号/date 或 type 非法；500 `{error:string,details:string}`。
- 副作用：查询前可能触发全局历史摩拉记录归档整理（明细/日/月聚合及冗余记录清理），失败只记录日志，不直接使本请求失败。

<a id="api-012"></a>

### GET /api/BagStatistics/MoraleMonsterBook

- URL：`{baseUrl}/api/BagStatistics/MoraleMonsterBook`。
- 用途：统计当前角色累计怪物击杀和摩拉，区分精英及小怪。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数；使用当前角色 UID。
- 返回：HTTP 200，application/json；`{data:MoraleMonsterBook}`，详见模型表。
- 错误：400 未配置米游社角色；500 `{error:string,details:string}`。
- 副作用：查询前可能触发全局历史摩拉记录归档整理（明细/日/月聚合及冗余记录清理），失败只记录日志，不直接使本请求失败。

<a id="api-013"></a>

### GET /api/BagStatistics/MoraleIncomeBook

- URL：`{baseUrl}/api/BagStatistics/MoraleIncomeBook`。
- 用途：按日/月/年分类统计收益来源。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `type?:string` 默认 day，可选 day/today/month/year；`date?:string` 对应 YYYY-MM-DD/YYYY-MM/YYYY，缺省使用当前相应周期；today 归一为 day。
- 返回：HTTP 200，application/json；`{data:MoraleIncomeBook}`；包含分类前后总额、未分类差额、怪物/精英/小怪/宝箱/任务/其他来源数量与摩拉。
- 错误：400 无账号或 type 非法；500 `{error:string,details:string}`。
- 副作用：查询前可能触发全局历史摩拉记录归档整理（明细/日/月聚合及冗余记录清理），失败只记录日志，不直接使本请求失败。

<a id="api-014"></a>

### POST /api/BagStatistics/updateMorale

- URL：`{baseUrl}/api/BagStatistics/updateMorale`。
- 用途：从米游社增量更新摩拉收支。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{message:string,data:MoraleUpdateResult}`；必须查看 data.status/message，HTTP 200 不保证已更新；数据记录总量、插入/跳过数和下次重试时间。
- 错误：业务失败/限流信息体现在 data.status/message；公共 401。
- 副作用：访问远端并写入本地记录；有约 5 分钟调用间隔及并发保护，不要循环高频更新。

<a id="api-015"></a>

### GET /api/EatStatistics/accounts

- URL：`{baseUrl}/api/EatStatistics/accounts`。
- 用途：列出营养袋吃药统计脚本已有账号。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{accounts:string[],default_account:string}`；默认账户优先，无账号时为空数组和空字符串。
- 错误：500 `{error:string,message:string}`。

<a id="api-016"></a>

### GET /api/EatStatistics

- URL：`{baseUrl}/api/EatStatistics`。
- 用途：读取指定账号的药品库存快照，不是直接的吃药次数。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `account?:string`；缺省“默认账户”，显式值不得为空、重复或含路径分隔符/Windows 非法文件字符。
- 返回：HTTP 200，application/json；直接返回 `[{Time:string,Name:string,Count:integer}]`；Count 为该时刻库存；无 JSON tag，字段首字母大写。
- 错误：400 非法/重复账号；404 显式账号不存在；500 读取失败，均 `{error:string,message:string}`。

<a id="api-017"></a>

### GET /api/checkBag

- URL：`{baseUrl}/api/checkBag`。
- 用途：列出库存不少于 8000 的材料，忽略贵重收集物和木材分类。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`map<string,integer>`，材料名映射数量。
- 错误：底层错误不显式透出；可能返回空 map。
- 副作用：可能访问背包远端或更新缓存。

<a id="api-018"></a>

### POST /api/deleteBag

- URL：`{baseUrl}/api/deleteBag`。
- 用途：清理历史统计，保留近期数据。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数；不按当前查看账户限制清理范围。
- 返回：HTTP 200，application/json；`{message:string}`；可能为“清理成功”或“清理部分失败：...”而非固定成功码。
- 错误：业务失败仍可能 HTTP 200，检查 message。
- 副作用：删除非最近三日的本地背包记录；脚本记录按各账户保留最近三批/三次扫描，并可能清理旧摩拉/原石文件。

<a id="api-019"></a>

### POST /api/startGroups

- URL：`{baseUrl}/api/startGroups`。
- 用途：按给定顺序启动一个或多个配置组。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json（数组）。
- 参数：Body 为原始 JSON 字符串数组，例如 `["配置组A","配置组B"]`，不是 `{groups:...}`。
- 返回：HTTP 200，application/json；`{status:"success",message:"Success"}`；启动失败 `{status:"error",message:string}`。
- 错误：400 绑定失败；业务启动失败为 HTTP 200，需检查 status。
- 副作用：启动 BetterGI 任务，不能把重试当成幂等操作。

<a id="api-020"></a>

### GET /api/getAutoArtifactsPro

- URL：`{baseUrl}/api/getAutoArtifactsPro`。
- 用途：读取狗粮批发脚本记录文件列表及摘要。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{title:string,JsVersion:string,items:DogFoodRecord[]}`；title 含本地脚本版本，JsVersion 是远端版本缓存。
- 错误：500 保留相同 title/JsVersion，items=null。
- 副作用：必要时触发后台版本缓存刷新。

<a id="api-021"></a>

### GET /api/getAutoArtifactsPro2

- URL：`{baseUrl}/api/getAutoArtifactsPro2`。
- 用途：读取一个狗粮记录文件的趋势序列。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `fileName:string` 必填；`json?:string` 为 "1" 时直接返回数据，否则返回 items 外壳。
- 返回：HTTP 200，application/json；json=1：`EarningsSeries|null`；否则 `{items:EarningsSeries|null}`。
- 错误：400 `{error:"fileName不能为空"}`；json=1 且读取失败 500 `{error:"读取失败"}`；未设 json=1 时读取错误可能仍为 HTTP 200 items=null。

<a id="api-022"></a>

### GET /api/logAnalysis

- URL：`{baseUrl}/api/logAnalysis`。
- 用途：分析日志材料收益（保留统计次数至少 10 的条目）。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `file?:string` 日志文件标识，缺省行为由底层今日日志读取规则决定。
- 返回：HTTP 200，application/json；`[{name:string,count:integer,isFocus:boolean,category:string,rawItems:[{name:string,count:integer}]}]`；分别表示归一化名称、数量、关注状态、分类与原始 OCR 名称及数量。
- 错误：处理器忽略底层读取错误，可能为 null/空数组。

<a id="api-023"></a>

### POST /api/backup

- URL：`{baseUrl}/api/backup`。
- 用途：备份 BetterGI User 等运行资料。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{status:"received",data:"备份成功"}`。
- 错误：400 `{status:"received",data:object}`；data 直接序列化 error，未保证稳定错误字段。
- 副作用：创建本地备份归档文件。

<a id="api-024"></a>

### GET /api/gitLog

- URL：`{baseUrl}/api/gitLog`。
- 用途：查询旧版脚本仓库近三天变更目录。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{gitLog:[{TypeName:string,Repo:[{FilePath:string,Authors:string,LastUpdated:string,Tags:string,Version:string,Description:string}]}]}`。
- 错误：仓库更新/文件错误仅日志记录，通常返回 HTTP 200 空列表。
- 副作用：底层 Read 会尝试更新本地脚本 Git 仓库；与只读 HTTP 的 gitCommits 不同。

<a id="api-025"></a>

### GET /api/gitCommits

- URL：`{baseUrl}/api/gitCommits`。
- 用途：通过远端平台 HTTP 查询真实 Git 提交记录。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `repo?:string` 默认 script，可选 script/bgi；`days?:integer` 默认 7，无效/<=0 用 7，大于 30 截为 30。
- 返回：HTTP 200，application/json；`{code:200,status:"success",source:string,repoUrl:string,branch:string,since:string,until:string,commits:[{sha:string,shortSha:string,message:string,author:string,date:string,htmlUrl:string}]}`。
- 错误：400 不支持的 repo；502 远端失败，`{code:400或500,status:"error",msg:string,commits:[]}`。
- 副作用：使用数据库缓存并可能访问远端，不操作本地 Git 工作树。

<a id="api-026"></a>

### GET /api/other

- URL：`{baseUrl}/api/other`。
- 用途：查询日志中配置组耗时及今日配置组状态。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `file?:string`：日志文件标识。
- 返回：HTTP 200，application/json；`{GroupTime:GroupAnalysis[],signLog:string,groupPInfo:string}`；signLog 当前初始化为空字符串。
- 错误：处理器忽略耗时读取错误，GroupTime 可为 null。

<a id="api-027"></a>

### POST /api/archive

- URL：`{baseUrl}/api/archive`。
- 用途：提交配置组执行统计，尝试归档并重置日志状态。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON 为 `GroupAnalysis`；使用 GroupName、ArchiveDate、Segments[].StartTime/Consuming；Consuming 是 Go duration 文本（如 "1m30s"），ArchiveDate 可选 YYYY-MM-DD。
- 返回：HTTP 200，text/plain; charset=utf-8；固定文本“成功归档”。
- 错误：400 JSON `{error:string}`；处理器忽略归档业务返回值，执行不足 60 秒/字段无效/数据库失败时也可能回复“成功归档”，不能据此确认已落库。
- 副作用：可能写入/覆盖当天同名配置组归档，并重置 BGI 日志状态。

<a id="api-028"></a>

### GET /api/LogAnalysis2Page

- URL：`{baseUrl}/api/LogAnalysis2Page`。
- 用途：按配置组聚合执行分段、子任务收益和错误。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `file:string` 必填。
- 返回：HTTP 200，application/json；`{status:"success",data:GroupAnalysis[]}`。
- 错误：400 text/plain “缺少 file 参数”；底层读取无有效数据可返回空结果。

<a id="api-029"></a>

### GET /api/LogAnalysis2EfficiencyRoutes

- URL：`{baseUrl}/api/LogAnalysis2EfficiencyRoutes`。
- 用途：查询路线效率、耗时与每分钟收益。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `file:string` 必填。
- 返回：HTTP 200，application/json；`{status:"success",data:EfficiencyRoute[]}`。
- 错误：400 text/plain “缺少 file 参数”。

<a id="api-030"></a>

### GET /api/newScriptHouse/jsNames

- URL：`{baseUrl}/api/newScriptHouse/jsNames`。
- 用途：对比本地脚本与远端仓库版本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{code:200,status:"success",source:string,repoUrl:string,data:ScriptHouseState[]}`。
- 错误：失败仍 HTTP 200：`{code:500,msg:string,data:[]}`。
- 副作用：可能获取远端仓库索引并更新缓存。

<a id="api-031"></a>

### GET /api/newScriptHouse/scripts

- URL：`{baseUrl}/api/newScriptHouse/scripts`。
- 用途：搜索远端可安装脚本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `search?:string` 搜索词。
- 返回：HTTP 200，application/json；`{code:200,msg:"success",data:[{name:string,description:string,version:string,lastUpdated:string}]}`。
- 错误：HTTP 200 `{code:500,msg:string}`。

<a id="api-032"></a>

### POST /api/newScriptHouse/subscribe

- URL：`{baseUrl}/api/newScriptHouse/subscribe`。
- 用途：下载并安装一个脚本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `ScriptName:string` 必填，注意首字母大写。
- 返回：HTTP 200，application/json；`{success:true,message:string}`。
- 错误：400 缺少 ScriptName；500 安装失败，`{error:string}`。
- 副作用：下载、解压并写入本地脚本目录。

<a id="api-033"></a>

### POST /api/newScriptHouse/update/:name

- URL：`{baseUrl}/api/newScriptHouse/update/:name`。
- 用途：更新一个已安装脚本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Path `name:string` 必填，本地脚本名称。
- 返回：HTTP 200，application/json；`{success:true,message:string}`。
- 错误：400 缺脚本名；500 更新失败，`{error:string}`。
- 副作用：备份历史并覆盖脚本，依照 saved_files 等逻辑保留用户数据。

<a id="api-034"></a>

### POST /api/newScriptHouse/batchUpdate

- URL：`{baseUrl}/api/newScriptHouse/batchUpdate`。
- 用途：批量更新需要更新的脚本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{status:"success"|"partial",message:string,updated:[{name:string,chineseName:string,version:string,message:string}],skipped:string[],failures:string[]}`。
- 错误：500 `{status:"error",message:string}`；部分失败为 HTTP 200 status=partial。
- 副作用：访问远端、备份并覆盖多个脚本。

<a id="api-035"></a>

### POST /api/newScriptHouse/delete

- URL：`{baseUrl}/api/newScriptHouse/delete`。
- 用途：删除指定本地脚本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填且应为合法脚本名。
- 返回：HTTP 200，application/json；`{code:200,msg:"success"}`。
- 错误：400 `{code:400,msg:string}`。
- 副作用：删除本地脚本目录，不自动撤销已经使用它的配置组。

<a id="api-036"></a>

### GET /api/newScriptHouse/history

- URL：`{baseUrl}/api/newScriptHouse/history`。
- 用途：读取脚本的本地历史版本（兼容历史文件和目录）。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `jsName:string` 脚本名。
- 返回：HTTP 200，application/json；`{success:true,versions:string[]}`。
- 错误：失败仍 HTTP 200 `{success:false,message:string}`。

<a id="api-037"></a>

### POST /api/newScriptHouse/rollback

- URL：`{baseUrl}/api/newScriptHouse/rollback`。
- 用途：恢复脚本的一个本地历史版本。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `jsName:string`、`version:string`：均用于定位合法历史版本。
- 返回：HTTP 200，application/json；`{success:true,message:string}`。
- 错误：失败仍 HTTP 200 `{success:false,message:string}`。
- 副作用：覆盖当前脚本并恢复约定的用户配置。


<a id="section-03"></a>

## BetterGI 配置与 ABGI 设置

源码：`menu/routes_config.go`、`bgiStatus/BetterGIService.go`、`bgiStatus/OneDragon.go`、`bgiStatus/pickUpList/AutoPick.go`。

本节新版一条龙配置使用原生文件 revision 乐观并发；写入前尽量先查询最新 revision，BetterGI 运行期间必须显式 force 确认。

<a id="api-038"></a>

### GET /api/bgiConfig/allConfig

- URL：`{baseUrl}/api/bgiConfig/allConfig`。
- 用途：枚举一条龙配置文件与格式支持状态。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{success:true,data:{items:[{name:string,supported:boolean,format:string,modifiedAt:string}],betterGIRunning:boolean}}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。

<a id="api-039"></a>

### GET /api/bgiConfig/findConfig

- URL：`{baseUrl}/api/bgiConfig/findConfig`。
- 用途：读取指定新版一条龙文件、修订值与修改时间。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `configName:string` 配置名。
- 返回：HTTP 200，application/json；`{success:true,data:OneDragonRevision}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。

<a id="api-040"></a>

### POST /api/bgiConfig/saveConfig

- URL：`{baseUrl}/api/bgiConfig/saveConfig`。
- 用途：保存可视化一条龙配置，保留原文件未知字段。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `config:OneDragonConfig` 必填；`originalName?:string` 缺省等于 config.name，必须与其相同（重命名另用专用接口）；`revision?:string` 读取时的版本（强烈建议必传；空值跳过版本检查）；`force?:boolean` 默认 false，只确认运行中写入风险，不绕过 revision 冲突。
- 返回：HTTP 200，application/json；`{success:true,data:OneDragonRevision,message:"修改成功"}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。
- 副作用：覆盖对应原生一条龙文件。

<a id="api-041"></a>

### GET /api/bgiConfig/options

- URL：`{baseUrl}/api/bgiConfig/options`。
- 用途：读取任务、队伍、副本、首领、角色天赋与各枚举选项。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{success:true,data:OneDragonEditorOptions}`；实际候选值应从此接口取，不在客户端硬编码。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。

<a id="api-042"></a>

### GET /api/bgiConfig/groups

- URL：`{baseUrl}/api/bgiConfig/groups`。
- 用途：从指定一条龙排除内置任务，取得配置组。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `configName:string` 配置名。
- 返回：HTTP 200，application/json；`{success:true,data:[{name:string,enabled:boolean}]}`；已从目录删除但仍在配置中的组也保留。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。

<a id="api-043"></a>

### POST /api/bgiConfig/createConfig

- URL：`{baseUrl}/api/bgiConfig/createConfig`。
- 用途：创建一份新的一条龙配置。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `name:string` 新配置名；`force?:boolean` 默认 false，用于运行中确认。
- 返回：HTTP 200，application/json；`{success:true,data:OneDragonRevision,message:"创建成功"}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。
- 副作用：创建原生一条龙 JSON 文件，已有同名配置时报冲突。

<a id="api-044"></a>

### POST /api/bgiConfig/renameConfig

- URL：`{baseUrl}/api/bgiConfig/renameConfig`。
- 用途：重命名一条龙配置。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `name:string` 原名、`newName:string` 新名、`revision?:string` 当前版本（建议必传，空值跳过比较）；`force?:boolean` 默认 false。
- 返回：HTTP 200，application/json；`{success:true,message:"重命名成功"}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。
- 副作用：重命名/更新一条龙文件。

<a id="api-045"></a>

### POST /api/bgiConfig/deleteConfig

- URL：`{baseUrl}/api/bgiConfig/deleteConfig`。
- 用途：删除一条龙配置。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `name:string` 配置名、`revision?:string` 当前版本（建议必传，空值跳过比较）、`force?:boolean` 默认 false。
- 返回：HTTP 200，application/json；`{success:true,message:"删除成功"}`。
- 错误：失败 JSON 为 `{success:false,code:string,error_code:string,message:string,error:string}`；400 参数或名称错误，404 文件不存在，409 格式不支持/版本冲突/正在运行需确认/重名，422 配置无效，500 文件操作失败。
- 副作用：删除配置文件。

<a id="api-046"></a>

### GET /api/bgiConfig/autoPick

- URL：`{baseUrl}/api/bgiConfig/autoPick`。
- 用途：读取自动拾取五份名单、模式和运行状态。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{success:true,data:AutoPickData}`。
- 错误：500 `{success:false,code:"BGI_AUTO_PICK_ERROR",error_code:"BGI_AUTO_PICK_ERROR",message:string,error:string}`，详见实际名单读取错误。

<a id="api-047"></a>

### PUT /api/bgiConfig/autoPick

- URL：`{baseUrl}/api/bgiConfig/autoPick`。
- 用途：保存自动拾取名单或设置。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `lists?:AutoPickLists`、`settings?:AutoPickSettings` 至少提供一个；兼容 `{data:{lists,settings}}` 和 `autoPickConfig` 作为 settings 别名。各提交子对象整块替换，不是字段级 PATCH；省略某列表会成为空列表；未提交的整块保留。mode 不区分大小写，空值默认 Whitelist。
- 返回：HTTP 200，application/json；`{success:true,data:AutoPickData,message:string}`。
- 错误：400 请求无效/mode 非法；500 文件读写失败，`{success:false,code:"BGI_AUTO_PICK_ERROR",error_code:"BGI_AUTO_PICK_ERROR",message:string,error:string}`。
- 副作用：依次写入五个名单文件和 User/config.json；每个文件原子替换，但整个六文件操作不是一个事务；允许 BGI 运行时写入。

<a id="api-048"></a>

### GET /api/config

- URL：`{baseUrl}/api/config`。
- 用途：读取 ABGI 当前完整设置（部分敏感字段脱敏）。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{status:"success",data:AbgiConfig}`；AutoStart 尽量读取真实系统状态；脱敏与未脱敏边界见 AbgiConfig 模型说明。
- 错误：自启动状态读取失败只记日志，仍返回设置。
- 副作用：仅查询；不得把返回配置认作所有字段都可安全公开的公共信息。

<a id="api-049"></a>

### POST /api/saveConfig

- URL：`{baseUrl}/api/saveConfig`。
- 用途：保存 main.json 并应用启动项、RDP、OBS、联机、Maa、AI 等运行设置。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON 为 `AbgiConfig` 完整快照；建议先 GET /api/config 再修改提交，不是通用 PATCH。约定的敏感字段为空时保留旧值；OnlineFallback、BGI 关闭控制等部分历史兼容字段省略时保留；其余省略字段可能归零。
- 返回：HTTP 200，application/json；`{status:"success",message:"配置保存成功"}`。
- 错误：400 解析/配置校验失败；500 自启动/序列化/文件写入失败；503 RDP 安全配置应用失败，`{message:string,error?:string}`。部分失败可能发生在 main.json 已落盘之后，不保证整体回滚。
- 副作用：写文件、修改 Windows 启动项与防火墙/远程桌面策略、重连 OBS、关闭 Maa 或刷新 AI；不要自动重试。


<a id="section-04"></a>

## AI 与技能

源码：`menu/routes_config.go`、`abgiAi/CombatStrategyAgent.go`、`abgiAi/CombatLogStrategyAgent.go`、`abgiAi/CombatLogOptimization.go`、`abgiAi/SkillService.go`。

<a id="api-050"></a>

### POST /api/ai/combat-strategy

- URL：`{baseUrl}/api/ai/combat-strategy`。
- 用途：自然语言生成、面板生成、微调或转换 TXT 战斗策略，返回候选但不保存。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `mode?:string`：generate/panel-generate/refine/convert；缺省依次按 sourceText 存在→convert、currentStrategy 存在→refine、否则 generate；`instruction?:string` 最多 8000 字符（普通 generate/refine 必填，panel-generate 可省略）；`currentStrategy?:CombatDocument`（refine 必填，<=256KiB）；`sourceText?:string`（convert 必填）、`sourceFileName?:string`（来源说明）；`selectedCharacters?:string[]` 1~4 名角色用于面板模式；`thinkingLevel?:string` none/low/medium/high，默认 low。
- 返回：HTTP 200，application/json；`{status:"success",data:CombatResult}`；needsClarification=true 是正常结果，不代表生成了策略。
- 错误：400 参数/AI 未开启（通用预设可在关闭 AI 时返回）；503 模型不可用；502 生成/上游异常；504 超时。重试型错误含 error_code、retry_after_seconds，Retry-After 为 60 或 120 秒。
- 副作用：可能访问 AI 与角色面板；不会自动写入策略文件。

<a id="api-051"></a>

### POST /api/ai/combat-strategy/from-log

- URL：`{baseUrl}/api/ai/combat-strategy/from-log`。
- 用途：从客户端完整执行日志快照识别四人队伍并生成策略。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `logLines:string[]` 主要必需；`sourceFileName?:string`、`groupName?:string`、`segmentIndex?:integer` 为来源快照说明，不作为服务器文件路径；`teamId?:string` 多队伍时传入上次返回的 teams[].id；请求<=16MiB。
- 返回：HTTP 200，application/json；`{status:"success",data:CombatLogResult}`；信息不足为 needsClarification；多队伍为 needsTeamSelection=true 和 teams，不调用模型；选定后返回 strategy、characters、fileName。
- 错误：400 解析/队伍选择/AI未启用；413 请求过大；503 模型不可用；502 上游失败（可有 Retry-After:60）。
- 副作用：仅分析和返回候选，不保存文件。

<a id="api-052"></a>

### POST /api/ai/combat-strategy/log-optimization/candidates

- URL：`{baseUrl}/api/ai/combat-strategy/log-optimization/candidates`。
- 用途：依据日志名称定位当前磁盘的真实 JSON 战斗策略文件。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `logLines:string[]`；请求<=16MiB。共用结构里的 fileName/instruction/token/confirmed 在此阶段无需传。
- 返回：HTTP 200，application/json；`{status:"success",data:{candidates:[{strategyName:string,fileName:string,error?:string}],warnings:string[]}}`；fileName 是后续 analyze 所需真实相对路径。
- 错误：400 日志/定位错误；413 过大，`{status:"error",message:string}`。
- 副作用：只读扫描策略文件，不执行或修改。

<a id="api-053"></a>

### POST /api/ai/combat-strategy/log-optimization/analyze

- URL：`{baseUrl}/api/ai/combat-strategy/log-optimization/analyze`。
- 用途：对选定策略及日志进行微调分析，生成可确认的覆盖预览。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `logLines:string[]`、`fileName:string`（来自 candidates）；`instruction?:string` 补充优化目标<=8000 字符；请求<=16MiB。
- 返回：HTTP 200，application/json；通常 `{status:"success",data:{token:string,strategyName:string,fileName:string,summary:string,warnings:string[],changes:string[],originalContent:string,optimizedContent:string}}`；token 有效15分钟。需澄清时 data 为 CombatResult 或 `{needsClarification:true,clarification:string,summary:string}`，不一定返回 token。
- 错误：400 候选无效/配置/AI错误；409 策略 Info.Name 已变；413 过大；503 模型/预览存储不可用；502/504 上游异常。
- 副作用：不覆盖文件；创建服务端内存预览。预览归属实际按 Authorization 请求头哈希绑定，analyze/apply 必须保持该头一致；仅 apiKey 调用时不要误以为是按不同 key 独立隔离。

<a id="api-054"></a>

### POST /api/ai/combat-strategy/log-optimization/apply

- URL：`{baseUrl}/api/ai/combat-strategy/log-optimization/apply`。
- 用途：显式确认后覆盖原 JSON 策略。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `token:string` 来自 analyze；`confirmed:boolean` 必须 true；其他字段不作为写入内容或路径依据；请求<=16MiB。
- 返回：HTTP 200，application/json；`{status:"success",message:"已覆盖原 JSON 战斗策略",data:{fileName:string}}`。
- 错误：400 未明确确认/目录无效；409 token失效/归属不符/目录或原文件变化；413 过大，`{status:"error",message:string}`。
- 副作用：原子替换预览对应的真实策略，成功后 token 删除；必须保持与 analyze 相同 Authorization 头。

<a id="api-055"></a>

### POST /api/ai/js-script/chat/stream

- URL：`{baseUrl}/api/ai/js-script/chat/stream`。
- 用途：只读分析指定本地 JS 脚本的源码、配置及变更信息。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `conversationId:string` 必填，8~128 字符，字母数字开头，仅字母数字/点/下划线/冒号/连字符；`scriptName:string` 必填安全脚本目录名；`message:string` 必填<=12000字符；`history?:[{role:string,content:string}]` 最多24条，role 为 user/assistant，每条content非空<=12000字符，总计<=64000字符。同会话不能切换脚本，空闲缓存约30分钟失效。
- 返回：HTTP 200，application/x-ndjson; charset=utf-8；逐行 `{type:"delta",delta:string}`，终止 `{type:"done"}`；流内失败 `{type:"error",message:string}`；客户端自行拼接 delta，不是 SSE。
- 错误：开始前400 `{message:string,error?:string}`，500 智能体初始化失败；开始后 HTTP 不再改变，只写 error 事件。
- 副作用：只读文件/既有脚本 Git 源与 AI 请求，不执行脚本。

<a id="api-056"></a>

### POST /api/ai/js-script/chat/reset

- URL：`{baseUrl}/api/ai/js-script/chat/reset`。
- 用途：释放脚本对话索引缓存。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `conversationId:string`，格式限制与 stream 一致。
- 返回：HTTP 200，application/json；`{message:"脚本 AI 会话缓存已清除"}`。
- 错误：400 `{message:string,error?:string}`。
- 副作用：删除服务端内存会话缓存，不删除脚本文件。

<a id="api-057"></a>

### POST /api/ai/chat/stream

- URL：`{baseUrl}/api/ai/chat/stream`。
- 用途：普通 AI 聊天或工具智能体；命中关键词技能时优先执行技能。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `message:string` 必填非空；`mode?:string` 默认 chat，chat 为普通聊天、tool 为工具模式；值会去空白、转小写；其他值返回未知模式错误。
- 返回：HTTP 200，text/plain; charset=utf-8，按文本块输出完整答案；不是 SSE 或 NDJSON。
- 错误：400 JSON `{message:string,error?:string}`，包括 AI未启用、空消息、技能/模型调用失败；输出中断无额外统一事件。
- 副作用：tool 模式或匹配技能可调用业务工具，可能启动任务/写入数据，不保证只读。

<a id="api-058"></a>

### GET /api/skill/list

- URL：`{baseUrl}/api/skill/list`。
- 用途：按关键字或来源类型查询技能。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `keyword?:string` 匹配名称/描述；`type?:string` 精确匹配技能类型。
- 返回：HTTP 200，application/json；`{code:1,msg:"ok",data:Skill[]}`，按 sort 升序、id降序。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。

<a id="api-059"></a>

### GET /api/skill/detail

- URL：`{baseUrl}/api/skill/detail`。
- 用途：读取单个技能。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `id:integer` 必填，可解析为非负整数。
- 返回：HTTP 200，application/json；`{code:1,msg:"ok",data:Skill}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。

<a id="api-060"></a>

### POST /api/skill/create

- URL：`{baseUrl}/api/skill/create`。
- 用途：创建一个 AI 技能。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON 为 Skill 的可编辑字段；`name:string` 必填非空且唯一；type 默认 user、triggerType 默认 manual、aiMode 默认 tool；其余编辑字段见 Skill 模型；id/createdAt/updatedAt 应交由服务端生成。
- 返回：HTTP 200，application/json；`{code:1,msg:"创建成功",data:Skill}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：写入技能表。

<a id="api-061"></a>

### POST /api/skill/update

- URL：`{baseUrl}/api/skill/update`。
- 用途：更新技能可编辑字段。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `id:integer` 必须非0；其余 Skill 编辑字段按完整快照提交；没有提供的编辑字段会被写为零值，不是 PATCH；source 和 createdAt 不在更新映射内。
- 返回：HTTP 200，application/json；`{code:1,msg:"更新成功"}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：修改数据库技能。

<a id="api-062"></a>

### DELETE /api/skill/delete

- URL：`{baseUrl}/api/skill/delete`。
- 用途：删除技能和运行日志。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `id:integer` 必填。
- 返回：HTTP 200，application/json；`{code:1,msg:"删除成功"}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：删除技能及该技能运行日志。

<a id="api-063"></a>

### POST /api/skill/toggle

- URL：`{baseUrl}/api/skill/toggle`。
- 用途：切换技能启用状态。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `id:integer` 必填。
- 返回：HTTP 200，application/json；`{code:1,msg:"ok",data:{enabled:boolean}}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：写入新的 enabled。

<a id="api-064"></a>

### POST /api/skill/run

- URL：`{baseUrl}/api/skill/run`。
- 用途：运行指定启用技能并返回完整答案。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `skillId:integer` 必填；`input?:string` 用户输入，可用于模板中的 `{{input}}`。
- 返回：HTTP 200，application/json；`{code:1,msg:"ok",data:{output:string,duration:integer}}`，duration 毫秒。
- 错误：400 `{message:"参数错误"}`；其余 通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：调用 AI/业务工具并保存运行日志，tool 技能可能改变系统状态。

<a id="api-065"></a>

### POST /api/skill/runStream

- URL：`{baseUrl}/api/skill/runStream`。
- 用途：运行技能，以纯文本分块输出答案。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `skillId:integer`、`input?:string` 同 run。
- 返回：HTTP 200 text/plain; charset=utf-8，答案文本块；调用模型完成后按块写入，不是 SSE。
- 错误：400 JSON `{message:string}`；业务失败仍可能 HTTP200 JSON `{code:0,msg:string}`，客户端按 Content-Type 解析。
- 副作用：同 run，保存运行日志，可能调用有副作用工具。

<a id="api-066"></a>

### GET /api/skill/runLogs

- URL：`{baseUrl}/api/skill/runLogs`。
- 用途：读取最近50条技能运行记录。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `skillId?:string` 按技能过滤；未传返回所有技能最近50条。
- 返回：HTTP 200，application/json；`{code:1,msg:"ok",data:SkillRunLog[]}`。
- 错误：处理器未显式暴露数据库查询错误。

<a id="api-067"></a>

### DELETE /api/skill/clearLogs

- URL：`{baseUrl}/api/skill/clearLogs`。
- 用途：清除一个技能的运行日志。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `skillId:string` 必填非空。
- 返回：HTTP 200，application/json；`{code:1,msg:"清空成功"}`。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：删除该技能日志。

<a id="api-068"></a>

### GET /api/skill/export

- URL：`{baseUrl}/api/skill/export`。
- 用途：导出单技能为 JSON 文件。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：Query `id:integer` 必填。
- 返回：HTTP 200，application/json；`{version:"1.0",...SkillExportItem}`；Content-Disposition 为 attachment，文件名 skill_<name>.json。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。

<a id="api-069"></a>

### GET /api/skill/exportAll

- URL：`{baseUrl}/api/skill/exportAll`。
- 用途：导出全部技能的可移植字段。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{version:"1.0",skills:SkillExportItem[]}`；不包含 id/enabled/sort/source/时间。
- 错误：处理器未显式返回数据库错误。

<a id="api-070"></a>

### POST /api/skill/import

- URL：`{baseUrl}/api/skill/import`。
- 用途：从导出格式导入技能，跳过重名。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON 推荐 `{version?:string,skills:SkillExportItem[]}`；skills非空。单技能 export 的对象不是可靠可导入格式：当前实现只有首轮绑定失败才尝试单对象且不重置已消费请求体，务必包装进 skills 数组。
- 返回：HTTP 200，application/json；`{code:1,msg:string}`，msg 汇报导入成功数及重名跳过数。
- 错误：通常 HTTP 200 `{code:0,msg:string}`；需看业务 code，不能仅看 HTTP。
- 副作用：逐项创建 imported 技能；不保证整个批次事务。


<a id="section-05"></a>

## 通知模板与周一记录

源码：`Notice/notice_template_http.go`、`Notice/notice_template.go`、`JsScript/AutoMonday/records.go`。

<a id="api-071"></a>

### GET /api/notice/template/context

- URL：`{baseUrl}/api/notice/template/context`。
- 用途：读取通知预览上下文及可插入变量。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{status:"success",data:{time:string,log:string,group:string,route:string,jsScript:string,expectedToEnd:string,progress:string},variables:map<string,string>}`；变量映射含 时间/日志/配置组/正在运行路线/正在运行js脚本/ExpectedToEnd/进度条 的模板占位符。
- 错误：处理器没有显式业务错误分支。

<a id="api-072"></a>

### POST /api/notice/template/background

- URL：`{baseUrl}/api/notice/template/background`。
- 用途：上传通知背景图并统一转换 PNG。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：multipart/form-data。
- 参数：FormData `file:file` 必填；PNG/JPEG/WebP，<=10MiB；宽>=240且高>=120，宽<=4096、高<=8192、像素<=16Mi。
- 返回：HTTP 200，application/json；`{status:"success",data:{path:string,width:integer,height:integer}}`；path 为 /img/notice-template/...png，可与 baseUrl 拼接读取。
- 错误：400 无文件/格式或尺寸错误；413 过大；500 保存转换失败，`{message:string,error?:string}`。
- 副作用：保存背景图；触发过期且未被当前模板引用的背景图清理（约24小时）。

<a id="api-073"></a>

### POST /api/notice/template/preview

- URL：`{baseUrl}/api/notice/template/preview`。
- 用途：用未保存模板生成图片预览。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：application/json。
- 参数：JSON `template:NoticeTemplate`、`log?:string`；请求<=256KiB；template.Type为空时 image；必须有上传背景 path；Markdown<=64KiB/1000行；字号0或10~72，其他约束见模板模型。
- 返回：HTTP 200 图片文件（image/png），Cache-Control:no-store；不返回 JSON data。
- 错误：400 配置/渲染错误；413 过大；429 同时渲染达到2个；`{message:string,error?:string}`。
- 副作用：生成临时预览文件，传输结束后删除；不保存模板配置。

<a id="api-074"></a>

### GET /api/autoMonday/records

- URL：`{baseUrl}/api/autoMonday/records`。
- 用途：读取全自动周一多账户任务及就绪时间。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{serverNow:string,accounts:[{account:string,fileName:string,updatedAt:string,tasks:[{name:string,readyAt:string}]}],warnings:[{fileName:string,line?:integer,message:string}]}`；时间为 RFC3339。
- 错误：404 auto_monday_records_not_found；500 better_gi_address_invalid/auto_monday_records_unavailable，`{error:string,message:string}`。


<a id="section-06"></a>

## 公开入口、Webhook 与流媒体

源码：`menu/routes_public.go`、`Notice/OneBot.go`、`abgiSSE/websocket_message.go`。

<a id="api-075"></a>

### GET /api/test

- URL：`{baseUrl}/api/test`。
- 用途：读取进程内一条龙进度快照。
- 认证：公开。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{status:"success",msg:{OneLongName:string,Details:map<string,boolean>,Order:string[]}}`；字段保留大写。
- 错误：没有显式业务失败分支。

<a id="api-076"></a>

### GET /api/appws

- URL：`{baseUrl}/api/appws`。
- 用途：订阅 APP 心跳和后端广播消息。
- 认证：公开；处理器允许任意 Origin。
- 请求格式：WebSocket 握手（ws/wss URL）。
- 参数：无业务参数；需要标准 WebSocket Upgrade 请求。
- 返回：HTTP101 切换协议；WebSocket 文本消息，约每5秒发送字面量 `heartbeat`，其他业务广播也是文本；客户端发入消息仅被读取，不定义 RPC 响应。
- 错误：握手错误由 WebSocket 升级处理；断开后需客户端重连。

<a id="api-077"></a>

### GET /api/abgiObs/PlayVideoStream

- URL：`{baseUrl}/api/abgiObs/PlayVideoStream`。
- 用途：播放 OBS 保存目录下的视频。
- 认证：仅 Query tk JWT；只传 apiKey 不可用。
- 请求格式：HTTP 文件读取；可带 Range 请求头。
- 参数：Query `tk:string` 必填有效 JWT；`path:string` 必填受限视频路径，必须可解析在 ObsSavePath 范围内。
- 返回：HTTP200/文件 Range 请求可为206，视频 Content-Type 按扩展名确定；Content-Disposition:inline；返回二进制文件而不是 JSON。
- 错误：401 `{status:"error",msg:string}` JWT无效；400 非法路径；404 视频不存在。

<a id="api-078"></a>

### GET /api/aBgiJt

- URL：`{baseUrl}/api/aBgiJt`。
- 用途：抓取当前桌面 JPEG。
- 认证：apiKey（公共认证规则 A）。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP200 image/jpeg，二进制 JPEG；Cache-Control:no-store，X-Content-Type-Options:nosniff。
- 错误：500 `{status:"error",code:"SCREENSHOT_FAILED",error:"截图失败"}`。
- 副作用：读取当前可捕获桌面；图片可能包含敏感信息。

<a id="api-079"></a>

### GET /ws/:name

- URL：`{baseUrl}/ws/:name`。
- 用途：持续订阅指定 BetterGI 日志文件新增行。
- 认证：公开；处理器允许任意 Origin。
- 请求格式：WebSocket 握手。
- 参数：Path `name:string` 必填日志文件名；握手后从文件末尾开始，每约500ms等待新行，不补历史。
- 返回：HTTP101 WebSocket；每条文本帧是一行新增日志；打开失败时文本帧“无法打开日志文件”后关闭。
- 错误：升级失败或读取失败导致关闭，不使用统一 JSON。
- 副作用：公开日志流，可能含敏感日志；文档仅记录现状，不表示可安全暴露公网。

<a id="api-080"></a>

### POST /bgiWebhook

- URL：`{baseUrl}/bgiWebhook`。
- 用途：接收 BetterGI 事件并推送 Windows 桌面通知。
- 认证：公开。
- 请求格式：application/json。
- 参数：JSON `event?:string` 通知标题、`result?:integer` 结果、`timestamp?:string` 时间、`screenshot?:string` 截图、`message?:string` 通知内容、`send_to?:string` 人员；字段没有 required 校验，实际通知主要使用 event/message。
- 返回：HTTP200 空响应体；当前成功分支未写 JSON。
- 错误：400 `{message:"参数格式错误",error:string}`；桌面推送失败仅记日志。
- 副作用：向当前 Windows 桌面推送通知。

<a id="api-081"></a>

### POST /bot/bgiWebhook

- URL：`{baseUrl}/bot/bgiWebhook`。
- 用途：接收任意对象、打印并回显。
- 认证：公开。
- 请求格式：application/json。
- 参数：JSON 任意对象 `map<string,any>`。
- 返回：HTTP 200，application/json；`{status:"success",msg:object}`；msg 为提交的同一 JSON 对象，字段由请求决定。
- 错误：400 `{message:"参数格式错误",error:string}`。
- 副作用：请求内容输出到日志/控制台，不要传敏感凭据。

<a id="api-082"></a>

### POST /api/onebot

- URL：`{baseUrl}/api/onebot`。
- 用途：接收 OneBot 11 消息并分发机器人命令。
- 认证：不经过普通 apiKey；OneBot.Secret 非空时强制签名，否则无签名校验。
- 请求格式：application/json。
- 参数：JSON `OneBotEvent`；消息分发要求 post_type="message" 且 raw_message 非空。若服务端配置 OneBot.Secret，Header `X-Signature:string` 必填，格式 `sha1=<HMAC-SHA1原始请求体的十六进制值>`，不可先重新格式化 JSON 再签名。
- 返回：HTTP 200，application/json；`{status:"ok"}`；不代表异步命令已经执行完成。
- 错误：401 缺少/错误签名 `{error:string}`；400 读body失败/invalid json。
- 副作用：异步触发已注册机器人消息处理器，可能启动任务和执行控制命令。

<a id="api-083"></a>

### GET /api/images

- URL：`{baseUrl}/api/images`。
- 用途：获取启动时枚举的图片 URL 列表。
- 认证：公开。
- 请求格式：无请求体；参数置于 URL Query。
- 参数：无业务参数。
- 返回：HTTP 200，application/json；`{images:string[]}`；Cache-Control:public,max-age=86400；Expires 约3天。
- 错误：没有显式业务错误分支。


<a id="section-07"></a>

## MCP HTTP 传输与 JSON-RPC

源码：`MCP/server.go`、`MCP/service.go`、`MCP/screenshot.go`。

仅启动时 Control.IsMcp=true 才挂载。先持续打开 SSE，取得 endpoint 事件给出的 sessionId，再向该 messages URL 发 JSON-RPC；HTTP202 只表示入队，结果从原 SSE 的 message 事件读取。

<a id="api-084"></a>

### GET /mcp/sse

- URL：`{baseUrl}/mcp/sse`。
- 用途：建立 MCP 旧式 SSE 会话；保持长连接接收消息。
- 认证：公开（当前 handleSSE 不验证密钥）。
- 请求格式：长连接，建议 Accept:text/event-stream。
- 参数：无业务参数；请求体不被处理器解析。
- 返回：HTTP200 text/event-stream；先 `event: endpoint`，data 为完整 `/mcp/messages?sessionId=...` URL；后续 `event: message`，data 为 JsonRpcResponse JSON；断开时删除会话。
- 错误：断开后旧 sessionId 不可复用，需重新建会话。

<a id="api-085"></a>

### POST /mcp/sse

- URL：`{baseUrl}/mcp/sse`。
- 用途：建立 MCP 旧式 SSE 会话；这是相同 SSE 握手处理器，不是 JSON-RPC 消息投递入口。
- 认证：公开（当前 handleSSE 不验证密钥）。
- 请求格式：长连接，建议 Accept:text/event-stream。
- 参数：无业务参数；请求体不被处理器解析。
- 返回：HTTP200 text/event-stream；先 `event: endpoint`，data 为完整 `/mcp/messages?sessionId=...` URL；后续 `event: message`，data 为 JsonRpcResponse JSON；断开时删除会话。
- 错误：断开后旧 sessionId 不可复用，需重新建会话。

<a id="api-086"></a>

### POST /mcp/messages

- URL：`{baseUrl}/mcp/messages`。
- 用途：向一个现存 SSE 会话提交 JSON-RPC 请求。
- 认证：MCP专用密钥认证 M：支持 apiKey、x-api-key、Authorization: <API_KEY> 或 Authorization: Bearer <API_KEY>；这里 Bearer 是密钥而非 JWT。
- 请求格式：application/json。
- 参数：Query `sessionId:string` 必填；JSON `jsonrpc:string`（按协议传 "2.0"，处理器未严格校验该值）、`method:string`、`params?:object`、`id?:string|number`；方法和参数见下表。
- 返回：HTTP202 空响应体；实际结果/错误通过该会话 SSE 返回。
- 错误：401 未授权；400 缺sessionId/无效JSON；404 会话不存在；410 会话关闭；429 等待槽位已满，错误 JSON `{error:string}`（401额外code:401）。
- 副作用：同会话串行执行，等待槽位最多8个；工具可能具有写操作/任务启动副作用。

#### MCP 消息模型与方法

请求为 `{jsonrpc:"2.0",id:1,method:"tools/call",params:{name:"queryBackpack",arguments:{materialName:"晶核"}}}`。响应为 `{jsonrpc:"2.0",id:1,result:...}` 或 `{jsonrpc:"2.0",id:1,error:{code:integer,message:string}}`；无 id 时响应 id 可能省略。

| method | params | result / 行为 |
|---|---|---|
| initialize | 可不传；处理器不读取协商参数 | protocolVersion="2024-11-05"，capabilities={tools:{}}，serverInfo={name:"abgi-info-mcp",version:"1.0.0"} |
| notifications/initialized | 可不传 | 通知，无 SSE 响应 |
| tools/list | 可不传 | {tools:[{name:string,description:string,inputSchema:JSON Schema}]} |
| tools/call | name:string 必填；arguments?:object | {content:[{type:"text",text:string}],isError?:boolean}；文本工具返回原字符串，其他值先序列化 JSON 再置于 text；业务错误以 isError=true 和 "Error: ..." 表示 |

未知方法或工具：JSON-RPC -32601；参数类型、必填、范围、任务名称错误：-32602。HTTP202不意味着工具成功。

#### MCP 工具（全部10个）

以下工具均通过 POST /mcp/messages 的 tools/call 调用，URL/认证/HTTP返回沿用该接口；arguments 对象即下表参数。

| 工具 | 用途与参数（均在 arguments） | content[0].text 解码后的结果及副作用 |
|---|---|---|
| findBgiIndex | 无业务参数；查询 BGI 当前进度 | MCPRes：Time 日期、Msg="bgi运行情况"、MapData 数组；首项包含“当前配置组：”“预计：”“当前路线：”“进度：”“bgi运行状态：”“js运行进度：”，其中“bgi运行状态：”为 boolean，其余值为 string；其余元素可能为 null |
| RunCronTask | taskName:string 必填，使用 tools/list inputSchema.enum 的实时任务名；params?:string 任务附加参数；delayInSeconds?:integer 默认0且>=0 | 文本受理提示；立即执行也是异步，不保证任务成功。当前内置任务：启动一条龙、关闭原神和关闭bgi、米游社签到、启动配置组、备份user；一条龙参数必须为真实配置名；启动配置组目前校验单个真实组名，不能假设任意空格分隔组合会通过 |
| continueOneDragon | configName?:string，不传使用 BetterGI 当前选中的一条龙 | MCPRes，MapData 含“一条龙配置”“从此执行任务”“任务ID”；按原生 NextTaskId 书签启动，具有任务副作用 |
| startObsRecording | 无业务参数 | 字符串“OBS 录屏已开始”；启动 OBS 录制 |
| stopObsRecording | videoName?:string，停止后可重命名 | {message:"OBS 录屏已结束",videoPath:string}；停止并可能改名文件 |
| captureDesktopScreenshot | 无业务参数 | {mimeType:"image/jpeg",base64:string,dataUrl:string}；base64 是 JPEG，dataUrl 含 data:image/jpeg;base64, 前缀 |
| queryCharacterBuild | characterName:string 必填；uid?:string、region?:string、cookie?:string，不传使用默认账号/本地配置 | Markdown 字符串，含角色面板及评分；会更新米游社角色缓存。不要在日志/公开示例中填写真实 cookie |
| queryBackpack | materialName:string 必填 | 字符串“此材料有N个”；不是裸整数 |
| collectMaterialRoutes | materialName:string 必填，quantity:integer 必填且>0 | MCPRes；MapData 含“材料”“目标数量”“刷新CD”“远程目录”“选择路线”({remoteFolder:string,fileName:string,count:integer}[])、“路线总数量”(integer)、“本地目录”“新增文件”(string[])、“已存在文件”(string[])；下载并加入路线 |
| collectCookingRoutes | recipeName:string 必填；servings?:integer 默认1，1~999 | MCPRes；MapData 首项为“料理”“份数”“新增材料路线”“已存在材料路线”“跳过材料”“失败材料”；后续每项“材料”“单份数量”“需要数量”“状态”“说明”，有路线时追加“本地目录”“新增文件”“已存在文件”；会安装可采集食材路线，跳过商店和加工项 |

MCPRes 为 `{Time:string,Msg:string,MapData:(object|null)[]}`（字段大写）；不要把这里的字符串内 JSON 与外层 JSON-RPC 混淆。

<a id="section-08"></a>

## 运行时、远程桌面、Maa 与会话程序

本章覆盖 `menu/routes_runtime.go` 的96条及 `SessionActions/http.go` 的10条。URL相对于 `{baseUrl}`，WebSocket使用 `ws`/`wss`；`datetime`表示JSON时间字符串，可选返回字段可能省略。

#### 本章认证与错误约定

- **apiKey**：头 `apiKey: <ABGI_API_KEY>`；支持主鉴权既有JWT方式。未认证统一401 JSON `{code:401,errorCode:"UNAUTHORIZED",error:"未授权"}`，逐接口不重复列出。
- **apiKey+RDP**：另需 `sys_config.rdp_enabled=true`，否则403 `{code:403,errorCode:"RDP_FEATURE_DISABLED",error:string}`。
- **apiKey+Maa运行开关**：另需 `Maa.Enable=true`，否则403 `{status:"disabled",message:string}`；搭建/编辑接口不要求开关。
- **Maa错误**：未特别说明为400 JSON `{status:"error",message:string,data?:RuntimeMaaStatus}`。**宏错误**：JSON `{status:"error",message:string,data?:RuntimeMacroStatus}`，开始录制/回放服务错误可能附状态。
- **RDP错误**：JSON `{error:string}`；档案不存在404、重复/资源上限/桌面不可用/并发会话冲突409、参数400、保护账户/禁用403、防火墙不安全503、凭据拒绝422、登录超时504、连接失败502、其他500；只适用于实际触发该错误的处理分支。
- **会话Bearer**：唯一头 `Authorization: Bearer <登录JWT>`，不是API Key；禁止apiKey头和token/tk查询参数；有Origin则须同源。401为BEARER_TOKEN_REQUIRED/UNAUTHORIZED，跨源403 ORIGIN_MISMATCH。
- **会话可用条件**：session_actions_enabled、RDP开关、embedded模式、非默认且至少12字符登录密码、Windows任务计划程序可用；否则403 SESSION_ACTIONS_DISABLED/STRONG_CREDENTIALS_REQUIRED或503 TASK_SCHEDULER_UNAVAILABLE。
- **会话本机管理条件**：会话可用条件＋来源回环IP（不看转发头）＋JWT签发距今10分钟内；否则403 LOOPBACK_REQUIRED/RECENT_AUTH_REQUIRED。会话错误统一JSON `{code:integer,errorCode:string,error:string}`。
- 下文“JSON”内容类型均为 `application/json; charset=utf-8`，“文本”为 `text/plain; charset=utf-8`；字段示意不是可直接发送的JSON示例。

#### 运行时共享结构

| 结构 | 字段及类型、含义 |
|---|---|
| RuntimeGroupLogSegment | `index:integer`运行段序号；`start_time/end_time:string`起止日期时间；`lines:string[]`原日志 |
| RuntimeRDPProfile | `id/name/username:string`编号/显示名/账户；`domain?:string`域；`width/height:integer`桌面像素；`autoStart/hideFromLogin/hasPassword:boolean`自启/隐藏/已设密码；`createdAt/updatedAt:datetime`时间。无密码明文或密文 |
| RuntimeRDPProfileInput | `name/username:string`必填；`password:string`创建必填，更新空串保留；`domain:string`默认空；`width/height:integer`默认1920/1080；`autoStart/hideFromLogin:boolean`默认false。名称及域+账户不区分大小写去重；宽320–3840高200–2160；存量超限只能向下兼容，绝对上限8192×4320；仅本机账户可隐藏；PUT普通字段缺省会按零值/默认覆盖 |
| RuntimeRDPManagedSession | `profileId:string`档案；`state:string`starting/connected/reconnecting/failed/stopped；`desired:boolean`保持连接；`lastError?:string`错误；`startedAt?:datetime; updatedAt:datetime`时间；`reconnectAttempts:integer`重连数；`circuitOpen:boolean`熔断；`reservedBytes:integer`缓存预留 |
| RuntimeRDPConcurrent | `enforced/supported:boolean`是否要求/是否支持后台并发；`provider/reason:string`能力来源/说明；`productName/editionId/installationType?:string`Windows版本 |
| RuntimeRDPFirewall | `enforced/compliant:boolean`安全规则要求/合规；`allowedRemoteAddresses:string[]`允许来源；`error?:string; checkedAt?:datetime`故障/检查时间 |
| RuntimeRDPProtectedAccount | `source/username/sid:string; domain?:string; sessionId:integer`保护来源、账户、SID、域及会话 |
| RuntimeRDPResources | `activeSessions/maxActiveSessions:integer`当前/最大连接；`reservedBytes/maxReservedBytes:integer`已预留/上限字节；`maxDesktopWidth/maxDesktopHeight/maxReconnectAttempts:integer`像素和重连上限 |
| RuntimeRDPStatus | `mode:string`模式；`localHost:string; localPort:integer`本地目标；`reachable:boolean; reachabilityError:string`端口探测；`concurrentSessions:RuntimeRDPConcurrent; managedSessions:RuntimeRDPManagedSession[]; firewall:RuntimeRDPFirewall; protected:{accounts:RuntimeRDPProtectedAccount[],error:string}; resources:RuntimeRDPResources` |
| RuntimeFavorite | `id:integer; userKey:string; menuKey/menuText/groupKey/groupTitle:string; sort:integer; createdAt/updatedAt:datetime`当前用户常用菜单，sort越小越靠前 |
| RuntimeMacroSummary | `id/name/description:string; createdAt:datetime; screen:{width:integer,height:integer}; eventCount:integer; durationMs:integer; sourceAppVersion/coordinateMode:string; hasPreview:boolean; previewUrl?:string`宏元信息、时长毫秒及预览URL |
| RuntimeMacroEvent | `type:string`mouse_move/mouse_down/mouse_up/mouse_click/mouse_wheel/key_down/key_up/key_tap；`delayMs:integer`相对前一动作延迟0–600000；`x/y/clicks/keycode/rawcode/wheelAmount/wheelDelta?:integer`坐标/点击/键码/滚轮；`button/key/windowTitle?:string`按钮/按键/窗口名 |
| RuntimeMacroDetail | RuntimeMacroSummary全部字段＋`events:RuntimeMacroEvent[]`，最多200000事件 |
| RuntimeMacroStatus | `state:string`idle/recording/playing；`macroId/macroName?:string; eventCount:integer; startedAt?:datetime; elapsedMs:integer; lastError/stopHotkey/panicHotkey/durationText?:string`运行状态及停止热键 |
| RuntimeLogBehavior | `value/label/description/dataPlaceholder:string; keyOptions?:{value:string,label:string}[]`行为键及填写提示，键盘行为可选键目录 |
| RuntimeLogRule | `id:integer; enabled:boolean; keywords:{id:integer,ruleId:integer,keyword:string}[]; actions:RuntimeLogAction[]; createdAt/updatedAt:datetime` |
| RuntimeLogAction | `id/ruleId/sort:integer; behavior/data:string; createdAt/updatedAt:datetime`顺序、行为键和字符串参数 |
| RuntimeLogRuleInput | `id:integer`仅更新必填；`enabled:boolean`默认false；`keywords:string[]`必填清理空白后至少1项且全库去重；`actions:{behavior:string,data:string}[]`至少1项，behavior必须来自behaviors；data随行为验证，嵌套JSON编码为字符串 |
| RuntimeArchive | `id:integer; title:string; execute_time:string; created_at:datetime`归档编号、标题、执行/创建时间 |
| RuntimeOnlineRoom | `group_name:string; group_id:integer; count:integer; description:string; is_homeowner:boolean; members:{name:string,is_simulated:boolean,abgi_type:string}[]`房间与成员 |
| RuntimeOnlineFallback | `enabled:boolean; timeoutMinutes:integer; groupId/scriptId:integer`空房检测；间隔1–1440分钟，读取或关闭缺省0补5；启用必须选正数房间及已存在工坊脚本 |

#### Maa共享结构

Pipeline与基础选项是开放JSON协议；以下描述本地包装及常见SDK字段，不虚构固定DTO限制。源码 `MaaRuntime/service.go`、`MaaRuntime/extended_api.go`、`MaaRuntime/window_action.go`、`MaaRuntime/process_action.go`、`MaaRuntime/pipeline_requirements.go`、`MaaRuntime/preview.go`、`config/Config.go`。

Maa 编辑接口使用相对于服务工作目录的固定路径：Pipeline 为 `MaaResources/pipeline`，图片为 `MaaResources/image`，OCR 模型为 `MaaResources/model/ocr`，基础选项为 `MaaUser/config/maa_option.json`，项目编辑数据为 `MaaUser/maa-projects`。这些编辑目录不随 `config.Maa.ResourceDir`、`config.Maa.UserDir` 的自定义值变化；运行时资源和用户目录仍以配置为准。

| 结构 | 字段及类型、含义 |
|---|---|
| RuntimeMaaConfig | `Enable:boolean; LibDir/ResourceDir/UserDir/Controller/WindowName/WindowClassName/ProcessName/AdbPath/AdbAddress/AgentPath:string`，大小写如示，DLL/资源/用户目录默认lib\\MaaFramework\\bin、MaaResources、MaaUser，Controller默认win32；win32筛选全部为空时WindowName默认原神。窗口及ADB筛选/代理路径；内部流程可用blank空控制器 |
| RuntimeMaaStatus | `maaInited/initialized/running/activated:boolean; version/controller/windowTitle/currentWindowAlias:string; config:RuntimeMaaConfig; windowAliases:string[]; lastEntry/lastStatus/lastDetail/lastError/lastRunAt:string`框架/任务状态、窗口及最后运行信息；lastRunAt本地yyyy-MM-dd HH:mm:ss或空串；连接激活失败可能附`activationError:string` |
| RuntimeMaaWindow | `handle/className/windowName/processName:string; windowX/windowY/windowWidth/windowHeight/clientWidth/clientHeight/pid/parentPid:integer; visible/minimized/usable/available:boolean`句柄、几何、进程及可用性 |
| RuntimeMaaAdb | `name/adbPath/address/screencapMethod/inputMethod/config:string`设备名、路径、地址、SDK采集/输入方式、设备配置JSON字符串 |
| RuntimeMaaImage | `mimeType:string="image/png"; base64:string; dataUrl:string; width/height:integer`PNG Base64与完整dataURL及像素 |
| RuntimeMaaPipeline | `{[nodeId:string]:RuntimeMaaNode}`节点ID映射；file必须pipeline目录内相对路径，禁绝对路径/上跳，缺.json补齐 |
| RuntimeMaaNode | 开放对象；`recognition/action?:string\|{type:string,param?:object}`识别/动作；`next/on_error/onError?:RuntimeMaaReference\|RuntimeMaaReference[]`流转；`roi?:number[4]; target?:string\|number[]; template/expected?:string\|string[]; model/input_text/package/custom_action?:string; key?:integer\|integer[]; post_delay/repeat/repeat_delay?:integer; custom_action_param?:object; attach?:object`原生资源/参数/扩展。自定义窗口/进程动作参照RuntimeMaaAcquireInput/RuntimeMaaProcessInput；attach.abgi保存编辑器描述；其他原生SDK字段保留 |
| RuntimeMaaReference | `string`或`{name?:string,node?:string,jump_back?:boolean,anchor?:boolean}`；支持[JumpBack]/[Anchor]字符串前缀 |
| RuntimeMaaRequirements | `targetMode:string`pipeline/external/none；`entry:string; reachable:string[]; hasPipelineTarget:boolean`入口/可达节点/自管目标 |
| RuntimeMaaOptions | `draw_quality:integer`默认85并限制0–100；`logging:boolean`默认true；`save_draw:boolean`默认false；`save_on_error:boolean`默认true；`stdout_level:integer`默认2并限制0–7（off/fatal/error/warn/info/debug/trace/all）；其他原生键保留；缺文件读取时创建 |
| RuntimeMaaProject | `schemaVersion:integer=2; file/entry/updatedAt:string; viewport:{x:number,y:number,zoom:number}; nodes:{[nodeId:string]:{x:number,y:number}}`编辑器布局，zoom默认1且<=0改1；旧v1读取保持原开放JSON（可能含steps） |
| RuntimeMaaProcessInput | `executable:string`必填存在的本地绝对.exe/.com文件禁UNC/设备路径；`arguments:string[]`默认空，<=128项且无NUL；`workingDirectory:string`默认exe目录且须存在；`wait:boolean`默认false；`startDelay:integer`默认0范围0–600000毫秒；`waitTimeoutMs:integer`等待时默认600000、上限3600000。直接起进程，不通过shell |
| RuntimeMaaProcessResult | `pid:integer; executable/workingDirectory:string; waited/exited:boolean`启动PID、规范化路径、是否等待/正常退出 |
| RuntimeMaaAcquireInput | `alias:string`默认window_auto最多64字节；`selector:{source?:string,launchStepId?:string,processName?:string,windowName?:string,className?:string}`，兼容顶层launchStepId/processName/windowName/className；`includeChildProcesses/startIfMissing:boolean`默认false；`launch?:{executable:string,arguments?:string[],workingDirectory?:string}`复用进程安全约束；`matchPolicy:string`默认best可unique；`timeoutMs:integer`默认30000上限600000；`pollIntervalMs:integer`默认500范围50–5000；`stabilityMs?:integer`默认1000，0–30000且<超时；`restore?:boolean`默认true；`activate?:boolean`兼容读但强制前台。selector嵌套优先，source可foreground/launchstep/launchexecutable/selector；自动启动须有launch，不能同时关联其他启动步骤 |
| RuntimeMaaRunResult | `id:integer; entry/status:string; nodes:integer; success:boolean; nodeId/nodeLabel/windowAlias/failedPath/message?:string`SDK任务结果、节点数与错误位置；停止时status=stopped |
| RuntimeMaaJobEvent | `at:datetime; type:string; node/nodeId/nodeLabel/windowAlias/failedPath/message?:string; taskId?:integer`异步事件，最多200条 |
| RuntimeMaaJob | `id/status/entry:string; maaTaskId?:integer; currentNode/nodeId/nodeLabel/windowAlias/failedPath?:string; progress:integer; totalNodes?:integer; events:RuntimeMaaJobEvent[]; result?:RuntimeMaaRunResult; error?:string; startedAt:datetime; finishedAt?:datetime`状态queued/running/success/failed/stopped，进度1–100；达到50条时优先淘汰最早结束的任务，活跃任务不受该数硬限制；重启后丢失，准备失败可带部分结果 |

#### 会话共享结构

| 结构 | 字段及类型、含义 |
|---|---|
| RuntimeSessionTemplateInput | `name:string`必填1–128字节；`description:string`默认空最多512字节；`executablePath:string`必填本地已存在绝对.exe普通文件；`arguments:string[]`默认空<=128项、无NUL，命令行<=32767 UTF-16单元；`workingDirectory:string`默认exe目录须存在；`enabled?:boolean`创建默认true更新缺省保留。禁UNC/设备路径/NUL/百分号展开 |
| RuntimeSessionTemplate | `id/name/description/executablePath/workingDirectory:string; arguments:string[]; enabled:boolean; createdAt/updatedAt:datetime`固定可启动模板，不含Windows密码 |
| RuntimeSessionCatalog | `id/name/description:string`已启用模板的远端安全摘要，不含exe路径/参数 |
| RuntimeSessionRun | `id/actionId/actionName/profileId/profileName/targetAccount/operator:string; sessionId:integer; status:string; errorCode?:string; createdAt/updatedAt:datetime; profileStartedAt/sessionDiscoveredAt/launchRequestedAt/completedAt?:datetime`启动审计；status=queued/starting_profile/waiting_for_session/registering/launching/launched/launch_unconfirmed/failed；launched只代表启动被确认，不代表业务完成 |

<a id="api-087"></a>

### GET /api/logFiles

- URL：`{baseUrl}/api/logFiles`。
- **用途**：列举BetterGI日志文件名
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {files:string[]}，用于fileName/file查询。
- **错误**：目录读取失败时处理器直接返回，可能得到200空响应，不存在固定错误JSON。
- **副作用**：读取BetterGI的log目录。
- **源码**：`bgiStatus/logService.go`。

<a id="api-088"></a>

### GET /api/logInfo

- URL：`{baseUrl}/api/logInfo`。
- **用途**：读取完整日志文本
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query fileName:string，实际应填写logFiles的文件名；处理器未显式非空校验。
- **返回**：200 文本，内容是日志原文。
- **错误**：读取失败先写500文本“读取日志失败”，代码未return，可能继续写内容；不能假定JSON错误。
- **副作用**：无。
- **源码**：`bgiStatus/logService.go`。

<a id="api-089"></a>

### GET /api/groupLog

- URL：`{baseUrl}/api/groupLog`。
- **用途**：按配置组加载/执行结束切分日志
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填日志名；group:string必填配置组名。
- **返回**：200 JSON {status:"success",group:string,file:string,segments:RuntimeGroupLogSegment[],total:integer}，total段数。
- **错误**：400缺file/group；500打开/扫描失败，JSON {status:"error",message:string}。
- **副作用**：无；未结束段也返回。
- **源码**：`bgiStatus/logService.go`。

<a id="api-090"></a>

### GET /api/oneRemote/sessions

- URL：`{baseUrl}/api/oneRemote/sessions`。
- **用途**：查询Windows全部多用户会话
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{SessionName:string,Username:string,ID:string,State:string}[]}；字段首字母大写，ID为字符串。
- **错误**：500 JSON {error:string}，Windows qwinsta查询失败。
- **副作用**：启动只读qwinsta查询。
- **源码**：`OneRemote/service.go；OneRemote/Function.go`。

<a id="api-091"></a>

### GET /api/oneRemote/status

- URL：`{baseUrl}/api/oneRemote/status`。
- **用途**：查询本地RDP能力、安全和托管连接状态
- **认证**：apiKey+RDP。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeRDPStatus}。
- **错误**：受公共RDP开关保护；探测错误通常写入data内部错误字段而仍返回200。
- **副作用**：执行端口/安全状态探测。
- **源码**：`OneRemote/service.go`。

<a id="api-092"></a>

### GET /api/oneRemote/profiles

- URL：`{baseUrl}/api/oneRemote/profiles`。
- **用途**：列举本地RDP档案（名称排序）
- **认证**：apiKey+RDP。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeRDPProfile[]}。
- **错误**：500 JSON {error:string}，档案文件无法读取/解析。
- **副作用**：无。
- **源码**：`OneRemote/service.go；OneRemote/profiles.go`。

<a id="api-093"></a>

### POST /api/oneRemote/profiles

- URL：`{baseUrl}/api/oneRemote/profiles`。
- **用途**：创建带加密凭据的RDP档案
- **认证**：apiKey+RDP。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeRDPProfileInput；password必填。
- **返回**：201 JSON {status:"success",data:RuntimeRDPProfile}。
- **错误**：遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：写档案文件，密码由Windows DPAPI保护；可修改Windows登录界面账户隐藏状态，失败会回滚档案。
- **源码**：`OneRemote/service.go；OneRemote/profiles.go`。

<a id="api-094"></a>

### PUT /api/oneRemote/profiles/:id

- URL：`{baseUrl}/api/oneRemote/profiles/:id`。
- **用途**：完整更新RDP档案
- **认证**：apiKey+RDP。
- **请求格式**：application/json请求体。
- **参数**：Path id:string必填；Body RuntimeRDPProfileInput；password空串沿用旧值。
- **返回**：200 JSON {status:"success",data:RuntimeRDPProfile}。
- **错误**：遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：覆盖档案及可选账户隐藏状态；不是局部PATCH。
- **源码**：`OneRemote/service.go；OneRemote/profiles.go`。

<a id="api-095"></a>

### DELETE /api/oneRemote/profiles/:id

- URL：`{baseUrl}/api/oneRemote/profiles/:id`。
- **用途**：删除RDP档案并移除托管连接
- **认证**：apiKey+RDP。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：200 JSON {message:"success"}。
- **错误**：遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：停止连接/自动重连，删除档案，并恢复原本被隐藏的Windows账户。
- **源码**：`OneRemote/service.go`。

<a id="api-096"></a>

### POST /api/oneRemote/start

- URL：`{baseUrl}/api/oneRemote/start`。
- **用途**：启动内置RDP档案或兼容1Remote连接
- **认证**：apiKey。
- **请求格式**：可空请求体；提供profileId时用application/json，兼容名称在Query。
- **参数**：Body profileId:string可选，非空时优先；否则Query launcher:string可选，其次username:string，至少有一项。
- **返回**：200 JSON {message:"success"}。
- **错误**：400 JSON格式错误/launcher为空；其余遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：建立RDP连接或调用外部1Remote；内置分支服务内部仍检查RDP功能与安全条件。
- **源码**：`OneRemote/service.go`。

<a id="api-097"></a>

### POST /api/oneRemote/stop

- URL：`{baseUrl}/api/oneRemote/stop`。
- **用途**：停止某个内置RDP连接
- **认证**：apiKey+RDP。
- **请求格式**：application/json请求体。
- **参数**：Body profileId:string必填非空；仅embedded模式支持。
- **返回**：200 JSON {message:"success"}。
- **错误**：400外部模式/缺profileId；其余遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：断开连接并停止自动重连，不等同于注销Windows账户。
- **源码**：`OneRemote/service.go`。

<a id="api-098"></a>

### POST /api/oneRemote/logoff

- URL：`{baseUrl}/api/oneRemote/logoff`。
- **用途**：注销目标Windows会话并停止匹配档案托管
- **认证**：apiKey。
- **请求格式**：可空请求体；Body为application/json，兼容目标为Query。
- **参数**：Body profileId:string可选，优先；否则Query id:string会话ID优先于sessionID:string；或username:string；至少指定一种。
- **返回**：200 JSON {message:"success"}。
- **错误**：400请求格式/目标为空；其余遵循本章RDP错误映射，错误体{error:string}。
- **副作用**：注销会话会终止该用户程序；停止匹配托管连接与重连。
- **源码**：`OneRemote/service.go`。

<a id="api-099"></a>

### GET /api/oneRemote/desktop/:id/ws

- URL：`{baseUrl}/api/oneRemote/desktop/:id/ws`。
- **用途**：在网页观看并操作指定内置RDP桌面
- **认证**：apiKey+RDP。
- **请求格式**：HTTP WebSocket Upgrade；帧JSON及二进制，非普通JSON下载。
- **参数**：Path id:string必填档案ID；WS客户端发JSON RuntimeRDPDesktopInput（见协议说明）。
- **返回**：101升级WebSocket；服务端先发{type:"ready",width:integer,height:integer,state:string}，每秒{type:"status",state:string,error:string}；二进制帧见协议说明。
- **错误**：遵循本章RDP错误映射，错误体{error:string}；跨源升级拒绝，升级后坏JSON/读超时关闭；非法输入丢弃无逐消息错误响应。
- **副作用**：读取桌面并可远程注入鼠标/键盘；断线自动释放本连接按下键/按钮。
- **源码**：`OneRemote/desktop_service.go；OneRemote/desktop_hub.go；OneRemote/manager.go`。

<a id="api-100"></a>

### GET /api/oneRemote/performance

- URL：`{baseUrl}/api/oneRemote/performance`。
- **用途**：查询电脑CPU/内存/GPU表现
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{CPUUsage:string,MemoryUsage:string,GPUUsage:string}}；内容是中文描述含百分比，不是数值。
- **错误**：500 JSON {error:string}为预留分支；采集失败通常在对应描述字段写“获取失败”仍200。
- **副作用**：采样约1秒，并可能调用nvidia-smi。
- **源码**：`OneRemote/service.go；OneRemote/Function.go`。

<a id="api-101"></a>

### GET /api/homeMenu/favorites

- URL：`{baseUrl}/api/homeMenu/favorites`。
- **用途**：查询当前用户常用菜单
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {code:200,data:RuntimeFavorite[]}，按sort、id升序。
- **错误**：500 JSON {code:500,error:string}。
- **副作用**：无。
- **源码**：`menu/HomeFavoriteMenu.go；models/HomeFavoriteMenuModel.go`。

<a id="api-102"></a>

### POST /api/homeMenu/favorites

- URL：`{baseUrl}/api/homeMenu/favorites`。
- **用途**：新增或更新常用菜单并置顶
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body menuKey/menuText:string必填非空；groupKey/groupTitle:string可选默认空，全部去首尾空格。
- **返回**：200 JSON {code:200,data:RuntimeFavorite}。
- **错误**：400 JSON {code:400,error:string}格式/空标识名称；500保存失败。
- **副作用**：按当前用户+menuKey写数据库，其余菜单sort递增，目标置0。
- **源码**：`menu/HomeFavoriteMenu.go`。

<a id="api-103"></a>

### POST /api/homeMenu/favorites/reorder

- URL：`{baseUrl}/api/homeMenu/favorites/reorder`。
- **用途**：重排当前用户常用菜单
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body menuKeys:string[]可选；空白/不存在/重复键忽略，未提交的已有菜单按原顺序追加。
- **返回**：200 JSON {code:200,data:string[]}，data为最终完整menuKey顺序。
- **错误**：400格式错误；500读取/写入失败，JSON {code:integer,error:string}。
- **副作用**：事务更新排序。
- **源码**：`menu/HomeFavoriteMenu.go`。

<a id="api-104"></a>

### DELETE /api/homeMenu/favorites

- URL：`{baseUrl}/api/homeMenu/favorites`。
- **用途**：删除当前用户某个常用菜单
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query menuKey:string必填，trim后非空。
- **返回**：200 JSON {code:200}；不存在也成功。
- **错误**：400缺menuKey；500数据库失败，JSON {code:integer,error:string}。
- **副作用**：删除当前用户匹配记录。
- **源码**：`menu/HomeFavoriteMenu.go`。

<a id="api-105"></a>

### GET /api/macro/list

- URL：`{baseUrl}/api/macro/list`。
- **用途**：查询本地宏摘要（创建时间倒序）
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMacroSummary[]}。
- **错误**：500 宏错误体，宏目录读取失败。
- **副作用**：无。
- **源码**：`macro/handler.go；macro/store.go`。

<a id="api-106"></a>

### GET /api/macro/detail/:id

- URL：`{baseUrl}/api/macro/detail/:id`。
- **用途**：查询宏元信息与完整事件
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填宏ID。
- **返回**：200 JSON {status:"success",data:RuntimeMacroDetail}。
- **错误**：404 宏错误体，未找到/非法ID/包校验失败。
- **副作用**：无。
- **源码**：`macro/handler.go；macro/types.go`。

<a id="api-107"></a>

### POST /api/macro/startRecord

- URL：`{baseUrl}/api/macro/startRecord`。
- **用途**：开始全局鼠标键盘录制
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body name:string必填trim后非空，截到80字符；description:string默认空截600；stopHotkey:string默认ctrl+alt+f12；coordinateMode:string默认absolute且只支持absolute。
- **返回**：200 JSON {status:"success",data:RuntimeMacroStatus}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：全局捕获键鼠并开始计时，同一时刻只允许一个录制/回放；停止后保存宏。
- **源码**：`macro/handler.go；macro/service.go`。

<a id="api-108"></a>

### POST /api/macro/stopRecord

- URL：`{baseUrl}/api/macro/stopRecord`。
- **用途**：停止当前录制并保存宏
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMacroSummary}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：停止捕获并写宏包/预览。
- **源码**：`macro/handler.go`。

<a id="api-109"></a>

### GET /api/macro/status

- URL：`{baseUrl}/api/macro/status`。
- **用途**：查询宏录制/回放状态
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMacroStatus}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`macro/handler.go`。

<a id="api-110"></a>

### POST /api/macro/play/:id

- URL：`{baseUrl}/api/macro/play/:id`。
- **用途**：异步回放宏
- **认证**：apiKey。
- **请求格式**：application/json请求体；即使默认参数也发送{}。
- **参数**：Path id:string必填；Body speed:number默认1（<=0改1，>5限5）；scaleCoordinates:boolean默认true；focusWindowTitle:string可选默认空。
- **返回**：200 JSON {status:"success",data:RuntimeMacroStatus}，仅启动确认，后续轮询status。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：真实发送键鼠，按需聚焦窗口及缩放坐标；急停Esc或ctrl+alt+f12。
- **源码**：`macro/handler.go；macro/player.go`。

<a id="api-111"></a>

### POST /api/macro/stopPlay

- URL：`{baseUrl}/api/macro/stopPlay`。
- **用途**：终止当前回放
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",message:"已停止回放"}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：取消回放并释放按键。
- **源码**：`macro/handler.go`。

<a id="api-112"></a>

### GET /api/macro/export/:id

- URL：`{baseUrl}/api/macro/export/:id`。
- **用途**：导出宏包
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：200 application/zip附件，文件名<宏名称>.abgimacro.zip；内含manifest.json（schemaVersion=1及宏摘要字段但无hasPreview/previewUrl）、events.json（RuntimeMacroEvent[]）、可选preview.jpg。
- **错误**：404 宏错误体，未找到/非法ID；导出写入失败500，若已输出压缩流则响应可能不完整。
- **副作用**：读取并打包宏。
- **源码**：`macro/handler.go；macro/store.go`。

<a id="api-113"></a>

### POST /api/macro/import

- URL：`{baseUrl}/api/macro/import`。
- **用途**：导入本地宏包
- **认证**：apiKey。
- **请求格式**：multipart/form-data。
- **参数**：Form file:file必填，文件名后缀.zip或.abgimacro.zip，压缩上传不超过30MiB；manifest/events各解压条目最多20MiB，preview最多5MiB；manifest版本1且事件/时长摘要必须一致。
- **返回**：200 JSON {status:"success",data:RuntimeMacroSummary}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：临时保存上传文件，校验后导入macros目录；临时文件请求结束删除。
- **源码**：`macro/handler.go；macro/service.go`。

<a id="api-114"></a>

### GET /api/macro/preview/:id

- URL：`{baseUrl}/api/macro/preview/:id`。
- **用途**：读取宏预览图片
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：200 image/jpeg图片字节（本地preview.jpg），支持标准文件响应。
- **错误**：404 JSON {status:"error",message:"预览不存在"}。
- **副作用**：无。
- **源码**：`macro/handler.go`。

<a id="api-115"></a>

### PUT /api/macro/:id

- URL：`{baseUrl}/api/macro/:id`。
- **用途**：修改宏名称与说明，不改动作
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Path id:string必填；Body name:string必填非空，description:string可选默认空；name/description先trim并分别截断到80/600字符。
- **返回**：200 JSON {status:"success",data:RuntimeMacroSummary}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：写宏元数据；运行时不允许修改。
- **源码**：`macro/handler.go；macro/store.go`。

<a id="api-116"></a>

### DELETE /api/macro/:id

- URL：`{baseUrl}/api/macro/:id`。
- **用途**：删除宏及预览
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：200 JSON {status:"success",message:"已删除"}。
- **错误**：400 宏错误体，message说明参数、任务占用或存储错误。
- **副作用**：移除对应本地宏目录；当前正在运行的目标宏不可删除，其他宏可删除，目标不存在也返回成功。
- **源码**：`macro/handler.go`。

<a id="api-117"></a>

### GET /api/logKeyword/behaviors

- URL：`{baseUrl}/api/logKeyword/behaviors`。
- **用途**：查询全部日志编排可选行为及参数提示
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON，顶层RuntimeLogBehavior[]，无data外壳。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`LogKeyword/LogKeywordService.go`。

<a id="api-118"></a>

### GET /api/logKeyword/rules

- URL：`{baseUrl}/api/logKeyword/rules`。
- **用途**：查询日志编排规则与有序动作
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON，顶层RuntimeLogRule[]，updatedAt/id倒序。
- **错误**：500 JSON {code:500,error:string}。
- **副作用**：无。
- **源码**：`LogKeyword/LogKeywordService.go；models/LogAutomationModel.go`。

<a id="api-119"></a>

### POST /api/logKeyword/rules

- URL：`{baseUrl}/api/logKeyword/rules`。
- **用途**：创建关键词触发规则
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeLogRuleInput；id忽略，字符串清理空白；特殊行为data须满足behaviors提示及服务校验。
- **返回**：201 JSON，顶层RuntimeLogRule；创建后重读失败仍201 {msg:"创建成功",id:integer}。
- **错误**：400参数/关键词重复/未知行为或行为data无效；500数据库失败，JSON {code:integer,error:string}。
- **副作用**：写规则、关键词、动作并重载运行时匹配配置，后续命中可能触发键鼠/程序/通知操作。
- **源码**：`LogKeyword/LogKeywordService.go`。

<a id="api-120"></a>

### PUT /api/logKeyword/rules

- URL：`{baseUrl}/api/logKeyword/rules`。
- **用途**：完整替换日志编排规则
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeLogRuleInput，id:integer必填>0；keywords/actions均完整提交。
- **返回**：200 JSON，顶层RuntimeLogRule；重读失败仍200 {msg:"更新成功",id:integer}。
- **错误**：400输入/行为校验；404规则不存在；500数据库，JSON {code:integer,error:string}。
- **副作用**：事务替换关键词/动作，更新enabled并重载匹配。
- **源码**：`LogKeyword/LogKeywordService.go`。

<a id="api-121"></a>

### DELETE /api/logKeyword/rules

- URL：`{baseUrl}/api/logKeyword/rules`。
- **用途**：删除日志编排规则与关联项
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query id:integer必填>0。
- **返回**：200 JSON {msg:"删除成功"}。
- **错误**：400无效id；404不存在；500查询/删除失败，JSON {code:integer,error:string}。
- **副作用**：事务删除规则/关键词/动作，重载运行时匹配。
- **源码**：`LogKeyword/LogKeywordService.go`。

<a id="api-122"></a>

### GET /api/maa/setup

- URL：`{baseUrl}/api/maa/setup`。
- **用途**：获取Maa安装诊断和工作台能力
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{enabled:boolean,framework:boolean,resource:boolean,pipeline:boolean,user:boolean,maaVersion:string,dllVersion:string,sdkVersion:string,libDir?:string,resourceDir?:string,pipelineDir?:string,userDir?:string,logDir?:string,dynamicControllerRebind:boolean,capabilities:{dynamicControllerRebind:boolean,multiWindow:boolean,mixedAdbAndWindows:boolean,autoStartWindow:boolean,uniqueWindowMatch:boolean,targetPreview:boolean,hotkey:boolean},ready:boolean}}；各boolean表示目录/能力是否存在，ready还要求Enable。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：可能创建pipeline及user目录；即使GET也不是完全无写入。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-123"></a>

### GET /api/maa/images

- URL：`{baseUrl}/api/maa/images`。
- **用途**：列举资源目录下的图片
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:string[]|null}，排序后的相对路径，支持png/jpg/jpeg/webp/bmp。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：资源目录解析可能创建目录。
- **源码**：`MaaRuntime/service.go`。

<a id="api-124"></a>

### POST /api/maa/images

- URL：`{baseUrl}/api/maa/images`。
- **用途**：上传或覆盖模板图片
- **认证**：apiKey。
- **请求格式**：multipart/form-data或application/json。
- **参数**：Form file:file必填且name:string可选默认上传名；或JSON name:string必填、dataUrl:string必填（完整dataURL或Base64）；二者择一，解码后1–10MiB；name相对路径禁上跳，后缀png/jpg/jpeg/webp/bmp。
- **返回**：200 JSON {status:"success",data:{name:string,size:integer}}，相对路径与字节数。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：创建目录并覆盖同名文件；只检查后缀和大小，不保证内容可被图像解码。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-125"></a>

### DELETE /api/maa/images

- URL：`{baseUrl}/api/maa/images`。
- **用途**：删除模板图片
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query name:string必填资源内相对路径，后缀及路径约束同上传。
- **返回**：200 JSON {status:"success"}。
- **错误**：400 Maa错误体参数/目录错误；404删除失败含文件不存在。
- **副作用**：删除本地资源图片。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-126"></a>

### GET /api/maa/models

- URL：`{baseUrl}/api/maa/models`。
- **用途**：列举OCR模型目录
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{name:string,value:string,hasDet:boolean,hasRec:boolean,hasKeys:boolean}[]}；value为相对模型目录，布尔分别表示det.onnx/rec.onnx/keys.txt存在。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：读取目录；模型结果缓存2分钟。
- **源码**：`MaaRuntime/service.go`。

<a id="api-127"></a>

### GET /api/maa/option

- URL：`{baseUrl}/api/maa/option`。
- **用途**：读取Maa基础选项
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaOptions}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：若选项文件不存在会写入默认配置。
- **源码**：`MaaRuntime/service.go`。

<a id="api-128"></a>

### POST /api/maa/option

- URL：`{baseUrl}/api/maa/option`。
- **用途**：保存Maa基础选项并按需应用
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeMaaOptions开放对象，缺省五个规范字段补默认，额外SDK字段保留。
- **返回**：200 JSON {status:"success",message:"Maa 基础选项已保存",data:RuntimeMaaOptions}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：覆盖选项文件；框架已初始化时立即应用全局选项，应用失败时文件可能已经写成功。
- **源码**：`MaaRuntime/service.go`。

<a id="api-129"></a>

### GET /api/maa/pipelines

- URL：`{baseUrl}/api/maa/pipelines`。
- **用途**：列举所有原生Pipeline JSON文件
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:string[]|null}，排序的pipeline目录内相对路径。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：目录解析可能创建pipeline目录。
- **源码**：`MaaRuntime/service.go`。

<a id="api-130"></a>

### GET /api/maa/pipeline

- URL：`{baseUrl}/api/maa/pipeline`。
- **用途**：读取原生Pipeline并分析目标依赖
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填相对路径；entry:string可选，缺省按文件入口推断。
- **返回**：200 JSON {status:"success",data:{file:string,content:RuntimeMaaPipeline,entry:string,runRequirements:RuntimeMaaRequirements}}；file为规范化文件名，content保留文件节点顺序。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：读取文件。
- **源码**：`MaaRuntime/service.go`。

<a id="api-131"></a>

### POST /api/maa/pipeline

- URL：`{baseUrl}/api/maa/pipeline`。
- **用途**：校验并覆盖保存原生Pipeline
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body file:string必填；content:RuntimeMaaPipeline必填，必须可解析为节点对象。
- **返回**：200 JSON {status:"success",message:"Pipeline 已保存"}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：按完整资源包校验后格式化覆盖文件，仅保留最新版。
- **源码**：`MaaRuntime/service.go`。

<a id="api-132"></a>

### DELETE /api/maa/pipeline

- URL：`{baseUrl}/api/maa/pipeline`。
- **用途**：删除Pipeline及其项目布局/草稿
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填，受资源内路径限制。
- **返回**：200 JSON {status:"success"}。
- **错误**：400路径/删除错误；404不存在；500 JSON {status:"error",message:string}表示Pipeline已删除但项目清理失败。
- **副作用**：永久删除原生文件及对应本地项目目录，不保留历史。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-133"></a>

### POST /api/maa/pipeline/copy

- URL：`{baseUrl}/api/maa/pipeline/copy`。
- **用途**：复制Pipeline并重写节点命名空间
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body file:string必填源文件；target:string必填目标相对路径，两者受file规则限制。
- **返回**：200 JSON {status:"success",data:{file:string}}，file为目标规范名。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：创建/覆盖目标文件；节点ID及next/on_error/target等内部引用加目标命名空间，不原样复用ID。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-134"></a>

### GET /api/maa/pipeline/export

- URL：`{baseUrl}/api/maa/pipeline/export`。
- **用途**：以附件下载原生Pipeline JSON
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填。
- **返回**：200 application/json文件字节，Content-Disposition: attachment，文件名为basename。
- **错误**：400 Maa错误体路径错误；404文件不存在。
- **副作用**：无。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-135"></a>

### GET /api/maa/templates

- URL：`{baseUrl}/api/maa/templates`。
- **用途**：查询内置新手Pipeline模板
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{id:string,name:string,description:string,controller:string,content:RuntimeMaaPipeline}[]}；含启动Windows程序/应用、停止应用、文字/图片/坐标点击、文本输入、按键、等待、循环及启动后点击模板。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：读取OCR目录决定模板模型名；不启动模板。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-136"></a>

### POST /api/maa/pipeline/validate

- URL：`{baseUrl}/api/maa/pipeline/validate`。
- **用途**：预检入口、节点引用、资源与常见参数
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body content:RuntimeMaaPipeline可选；file:string可选（content未提供时读取）；entry:string可选；内容或文件至少提供其一，否则返回valid=false。
- **返回**：200 JSON {status:"success",data:{valid:boolean,errors:{path:string,stepId?:string,code:string,level:string,message:string}[],missingImages:string[],missingModels:string[]}}；校验未通过仍200且valid=false。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：只预检，不保存或运行。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-137"></a>

### GET /api/maa/project

- URL：`{baseUrl}/api/maa/project`。
- **用途**：读取Pipeline对应编辑器布局
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填。
- **返回**：200 JSON {status:"success",data:RuntimeMaaProject|历史v1开放对象}；不存在时返回v2默认布局，不写新文件。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：读取布局，不改写原生Pipeline。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-138"></a>

### POST /api/maa/project

- URL：`{baseUrl}/api/maa/project`。
- **用途**：保存编辑器布局，不改写执行Pipeline
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body file:string必填；metadata:{entry?:string,viewport?:{x:number,y:number,zoom:number},nodes?:{[nodeId:string]:{x:number,y:number}},updatedAt?:string}可选；其他历史字段不持久化。
- **返回**：200 JSON {status:"success",data:RuntimeMaaProject}；updatedAt由服务端UTC RFC3339时间覆盖。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：写独立project.json，仅保留v2布局字段。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-139"></a>

### GET /api/maa/draft

- URL：`{baseUrl}/api/maa/draft`。
- **用途**：读取未保存草稿
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填。
- **返回**：200 JSON {status:"success",data:{file:string,updatedAt:string,content:RuntimeMaaPipeline}|null}；草稿不存在为null；历史文件原样开放JSON返回。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：只读草稿。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-140"></a>

### POST /api/maa/draft

- URL：`{baseUrl}/api/maa/draft`。
- **用途**：保存未完成Pipeline草稿
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body file:string必填；content:任意合法JSON值必填（通常RuntimeMaaPipeline），不进行正式Pipeline完整校验。
- **返回**：200 JSON {status:"success"}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：写draft.json，包含file、服务端RFC3339 updatedAt和原content。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-141"></a>

### DELETE /api/maa/draft

- URL：`{baseUrl}/api/maa/draft`。
- **用途**：删除指定Pipeline草稿
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query file:string必填。
- **返回**：200 JSON {status:"success"}，不存在也成功。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：删除draft.json，不删除Pipeline。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-142"></a>

### GET /api/maa/status

- URL：`{baseUrl}/api/maa/status`。
- **用途**：查询Maa框架与任务状态
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaStatus}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`MaaRuntime/service.go`。

<a id="api-143"></a>

### GET /api/maa/windows

- URL：`{baseUrl}/api/maa/windows`。
- **用途**：枚举Windows桌面窗口
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaWindow[]}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：按需加载SDK并扫描窗口；不连接目标。
- **源码**：`MaaRuntime/service.go`。

<a id="api-144"></a>

### GET /api/maa/adbDevices

- URL：`{baseUrl}/api/maa/adbDevices`。
- **用途**：枚举ADB设备
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaAdb[]}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：按已配置AdbPath探测设备并可能加载SDK。
- **源码**：`MaaRuntime/service.go`。

<a id="api-145"></a>

### POST /api/maa/init

- URL：`{baseUrl}/api/maa/init`。
- **用途**：以当前或临时Maa配置初始化控制器
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json，可空请求体。
- **参数**：Body RuntimeMaaConfig可选，先从当前配置复制后绑定；LibDir为空时回退整个当前配置；处理器忽略JSON绑定错误，客户端仍应发送合法对象。
- **返回**：200 JSON {status:"success",message:"Maa 初始化成功",data:RuntimeMaaStatus}。
- **错误**：409任务运行中不能初始化；400 Maa错误体初始化失败，含data状态。
- **副作用**：释放旧控制器、加载SDK/资源并连接窗口或ADB，不保存全局配置。
- **源码**：`MaaRuntime/service.go`。

<a id="api-146"></a>

### POST /api/maa/connection

- URL：`{baseUrl}/api/maa/connection`。
- **用途**：连接页面指定的当前窗口或ADB目标
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeMaaConfig＋WindowHandle:string可选窗口句柄；WindowPID:integer可选；restore/activate:boolean默认true；persist:boolean默认false。Controller/LibDir/ResourceDir/UserDir/AdbPath为空沿用当前全局对应值。
- **返回**：200 JSON {status:"success",data:RuntimeMaaStatus}，激活失败仍可能200并附activated=false、activationError。
- **错误**：400 Maa错误体输入/连接/保存失败；409任务运行中拒绝。保存失败时连接可能已经成功。
- **副作用**：替换控制器、恢复/激活窗口；persist=true写main.json并设Enable=true。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-147"></a>

### POST /api/maa/process/start

- URL：`{baseUrl}/api/maa/process/start`。
- **用途**：连接目标之前直接启动Windows程序
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeMaaProcessInput。
- **返回**：200 JSON {status:"success",message:"Windows 程序已启动",data:RuntimeMaaProcessResult}。
- **错误**：400 Maa错误体格式/安全校验；500 {status:"error",message:string,data:RuntimeMaaProcessResult}启动或等待错误。
- **副作用**：启动本地exe/com；wait=true可等待到超时并终止超时进程，写Maa运行日志。
- **源码**：`MaaRuntime/process_action.go`。

<a id="api-148"></a>

### POST /api/maa/targets/scan

- URL：`{baseUrl}/api/maa/targets/scan`。
- **用途**：按当前请求条件扫描Windows/ADB候选目标
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json，可空请求体。
- **参数**：Body Controller:string可选，adb走设备分支否则Windows；WindowName/WindowClassName/ProcessName:string可选模糊筛选；AdbPath/AdbAddress:string可选；处理器忽略绑定错误。
- **返回**：200 JSON {status:"success",data:RuntimeMaaWindow[]|RuntimeMaaAdb[]}，取决于Controller。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误。
- **副作用**：扫描设备/窗口，不保存旧页面配置。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-149"></a>

### POST /api/maa/targets/preview

- URL：`{baseUrl}/api/maa/targets/preview`。
- **用途**：临时查找、激活并截取Windows目标，不替换当前Tasker
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeMaaAcquireInput＋windowHandle:string可选非零十六进制/十进制句柄；windowPID:integer可选。startIfMissing且有launch时允许程序重启更换PID。
- **返回**：200 JSON {status:"success",data:{mimeType:string,base64:string,dataUrl:string,width:integer,height:integer,window:{handle:string,windowName:string,className:string,pid:integer,processName:string}}}。
- **错误**：400 Maa错误体参数/查找/激活/截图失败；409任务运行或准备中。
- **副作用**：可能自动启动launch程序、恢复并聚焦窗口；临时控制器用后释放。
- **源码**：`MaaRuntime/preview.go；MaaRuntime/window_action.go`。

<a id="api-150"></a>

### POST /api/maa/targets/activate

- URL：`{baseUrl}/api/maa/targets/activate`。
- **用途**：恢复并激活已连接Win32窗口
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaStatus}。
- **错误**：400 Maa错误体，data附activated=false及activationError。
- **副作用**：改变窗口前台/最小化状态。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-151"></a>

### POST /api/maa/run

- URL：`{baseUrl}/api/maa/run`。
- **用途**：执行Maa任务，默认同步等待
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json请求体。
- **参数**：Body entry:string必填非空入口（trim后使用；异步缺失时会先202后任务failed）；override:RuntimeMaaPipeline或SDK允许的覆盖值，可选；async:boolean默认false。
- **返回**：同步200 JSON {status:"success",message:"Maa 任务执行完成",data:RuntimeMaaRunResult}；async=true为202 JSON {status:"success",data:{jobId:string,status:"queued"}}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误；SDK失败可能在返回结果success=false表示，须同时检查结果。
- **副作用**：真实执行Pipeline，可能启动程序、操作窗口/ADB、键鼠输入。
- **源码**：`MaaRuntime/service.go；MaaRuntime/extended_api.go`。

<a id="api-152"></a>

### POST /api/maa/run/async

- URL：`{baseUrl}/api/maa/run/async`。
- **用途**：将Maa任务放入内存异步队列
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json请求体。
- **参数**：Body entry:string必填非空入口（trim后使用；异步缺失时会先202后任务failed）；override:RuntimeMaaPipeline或SDK允许的覆盖值，可选；async:boolean默认false；本接口始终异步，async字段不改变行为。
- **返回**：202 JSON {status:"success",data:{jobId:string,status:"queued"}}，受理不等于执行成功。
- **错误**：400 Maa错误体JSON绑定；执行失败由jobs/events查询，不改已发出的202。
- **副作用**：后台运行真实Pipeline并记录内存任务/事件。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-153"></a>

### GET /api/maa/jobs/:id

- URL：`{baseUrl}/api/maa/jobs/:id`。
- **用途**：读取一个异步任务最新快照
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填jobId。
- **返回**：200 JSON {status:"success",data:RuntimeMaaJob}。
- **错误**：404 JSON {status:"error",message:"任务不存在"}。
- **副作用**：无。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-154"></a>

### GET /api/maa/events

- URL：`{baseUrl}/api/maa/events`。
- **用途**：读取所有任务快照或订阅单任务状态
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query jobId:string可选；空为所有任务JSON，非空为SSE。
- **返回**：无jobId：200 JSON {status:"success",data:RuntimeMaaJob[]}按startedAt升序；指定：200 text/event-stream; charset=utf-8，event: snapshot，data为{status:"success",data:RuntimeMaaJob}，立即一次后每500ms发送，终态success/failed/stopped后关闭。
- **错误**：指定jobId不存在404 JSON {status:"error",message:"任务不存在"}；流中断不再返回第二个JSON错误。
- **副作用**：保持只读长连接；不接受Last-Event-ID断点回放。
- **源码**：`MaaRuntime/extended_api.go`。

<a id="api-155"></a>

### POST /api/maa/stop

- URL：`{baseUrl}/api/maa/stop`。
- **用途**：请求停止当前任务或指定异步任务
- **认证**：apiKey+Maa运行开关。
- **请求格式**：application/json，可空请求体。
- **参数**：Body jobId:string可选默认空；空请求体也允许。
- **返回**：200 JSON {status:"success",message:string,data:RuntimeMaaStatus}；message区分已结束/已发送停止请求。
- **错误**：400 Maa错误体格式/停止错误；指定jobId不存在404同错误体。
- **副作用**：标记取消并请求停止任务；成功响应不表示后台清理已完成。
- **源码**：`MaaRuntime/service.go`。

<a id="api-156"></a>

### POST /api/maa/shutdown

- URL：`{baseUrl}/api/maa/shutdown`。
- **用途**：释放Maa运行时及控制器
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",message:"Maa 运行时已释放",data:RuntimeMaaStatus}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：等待运行锁并释放Tasker/控制器/资源，更新lastStatus=shutdown。
- **源码**：`MaaRuntime/service.go`。

<a id="api-157"></a>

### GET /api/maa/screenshot

- URL：`{baseUrl}/api/maa/screenshot`。
- **用途**：对当前已连接目标测试截图
- **认证**：apiKey+Maa运行开关。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeMaaImage}。
- **错误**：400 Maa错误体，message说明参数、文件、SDK或设备错误；任务运行中/未初始化也为400。
- **副作用**：可能恢复并激活当前Win32窗口，截图后解除输入占用。
- **源码**：`MaaRuntime/service.go`。

<a id="api-158"></a>

### GET /api/abgiScreen/stream

- URL：`{baseUrl}/api/abgiScreen/stream`。
- **用途**：持续观看共享编码的本机桌面MJPEG
- **认证**：apiKey；还需Control.AbgiScreen=true。
- **请求格式**：无请求体，HTTP流。
- **参数**：Query profile:string可选，空=原尺寸30fps/质量60；smooth=最长边1280/5fps/45；balanced=最长边1920/10fps/60；clear=原尺寸15fps/80；hd=原尺寸15fps/95；fps:integer可选覆盖帧率，非法值用档位默认，数字限制1–30。
- **返回**：200 multipart/x-mixed-replace; boundary=frame，每段Content-Type:image/jpeg、Content-Length和JPEG字节；Cache-Control:no-store；关闭连接或开关停止。
- **错误**：400 JSON {status:"error",msg:"不支持的画质档位"}；屏幕开关关闭403 {status:"error",msg:string}；写超时断开。
- **副作用**：开始/共享桌面采集与编码，增加观看人数；每帧写超时10秒。
- **源码**：`abgiScreen/abgiScreen.go；abgiScreen/frames.go`。

<a id="api-159"></a>

### GET /api/abgiScreen/status

- URL：`{baseUrl}/api/abgiScreen/status`。
- **用途**：查询屏幕流开关、观看数和所选档位质量
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query profile:string可选，空=原尺寸30fps/质量60；smooth=最长边1280/5fps/45；balanced=最长边1920/10fps/60；clear=原尺寸15fps/80；hd=原尺寸15fps/95；fps:integer可选覆盖帧率，非法值用档位默认，数字限制1–30。
- **返回**：200 JSON {enabled:boolean,activeClients:integer,streamBytes:integer,frameRate:integer,targetFps:integer,quality:integer,streamWidth:integer,streamHeight:integer,frameAgeMs:integer|null,bounds:{x:integer,y:integer,width:integer,height:integer},lastFrameAt:datetime,lastError:string}；frameRate为全局上限30，streamBytes累计所有观看端字节，frameAgeMs是服务端帧龄。
- **错误**：400 JSON {status:"error",msg:string}非法档位；未启用仍可查询200。
- **副作用**：只读，不会因状态查询增加观看人数。
- **源码**：`abgiScreen/abgiScreen.go；abgiScreen/frames.go`。

<a id="api-160"></a>

### GET /api/abgiSSE/onlineFallback

- URL：`{baseUrl}/api/abgiSSE/onlineFallback`。
- **用途**：读取联机空房检测设置
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {code:1,data:RuntimeOnlineFallback}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`abgiSSE/online_fallback.go；config/OnlineFallback.go`。

<a id="api-161"></a>

### PUT /api/abgiSSE/onlineFallback

- URL：`{baseUrl}/api/abgiSSE/onlineFallback`。
- **用途**：保存联机空房检测和工坊脚本回退
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body RuntimeOnlineFallback；启用时interval/group/script完整提交；禁用未传间隔补5。
- **返回**：200 JSON {code:1,message:"空房检测设置已保存",data:RuntimeOnlineFallback}。
- **错误**：400 JSON {message:string}格式/间隔/房间/脚本校验；500保存失败同结构。
- **副作用**：写main.json并调整当前未发车连接的检测计时；目标房间无人时会下线并启动所选脚本。
- **源码**：`abgiSSE/online_fallback.go；config/OnlineFallback.go`。

<a id="api-162"></a>

### POST /api/abgiSSE/connect/:typeKey

- URL：`{baseUrl}/api/abgiSSE/connect/:typeKey`。
- **用途**：连接远端联机服务器
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：Path typeKey:string必填占位（当前handler未使用该值）；Query runDebug:string可选，仅字面true为true。
- **返回**：200 文本“连接成功”。
- **错误**：400 JSON {message:string}本地账户/密钥配置错误；400文本为连接失败原因。
- **副作用**：根据线路A/B、当日次数及runDebug决定debug/noDebug，建立出站WebSocket并启动联机监听，后续可能分发脚本。
- **源码**：`abgiSSE/aBgiSEEService.go`。

<a id="api-163"></a>

### POST /api/abgiSSE/disconnect

- URL：`{baseUrl}/api/abgiSSE/disconnect`。
- **用途**：关闭当前联机连接
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 空响应，无JSON体。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：关闭连接并取消相关等待/重连/检测生命周期。
- **源码**：`menu/routes_runtime.go；abgiSSE/wsclient.go`。

<a id="api-164"></a>

### GET /api/abgiSSE/getOnlineUser

- URL：`{baseUrl}/api/abgiSSE/getOnlineUser`。
- **用途**：查询远端联机房间和在线成员
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON 顶层RuntimeOnlineRoom[]；解密/HTTP/读响应/JSON失败时返回数字0，不是空数组。
- **错误**：handler不区分远端错误状态，失败多数以200数字0表示。
- **副作用**：向已配置联机服务器发GET请求。
- **源码**：`menu/routes_runtime.go；abgiSSE/wsclient.go`。

<a id="api-165"></a>

### POST /api/abgiSSE/HomeownerUpdateGroup

- URL：`{baseUrl}/api/abgiSSE/HomeownerUpdateGroup`。
- **用途**：代理房主修改房间信息
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body group_name:string房间名、description:string说明、is_active/is_open:boolean；uid:string可省略且会被本地配置加密UID覆盖；本地未声明required标签。
- **返回**：200 JSON {message:string}，远端说明原样字符串返回，不能用HTTP200判断业务成功。
- **错误**：JSON绑定失败也为200 {error:"数据错误"}；远端请求失败200 {message:"更新配房间失败"}；其他远端失败也200 message。
- **副作用**：发送远端房间更新请求；is_active/is_open缺省为false，gorm默认注解不会给请求体补true。
- **源码**：`abgiSSE/wsclient.go`。

<a id="api-166"></a>

### POST /api/abgiSSE/HomeownerQueryGroup

- URL：`{baseUrl}/api/abgiSSE/HomeownerQueryGroup`。
- **用途**：代理房主查询指定房间
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body group_name:string应填写房间名；uid:string可忽略由后端覆盖；本地无非空校验。
- **返回**：200 JSON {message:{description:string,group_name:string,is_active:boolean,is_open:boolean}}；远端message对象字段透传，缺字段可null。
- **错误**：绑定/加密失败仍200 {error:"数据错误"}；请求或远端失败200 {message:string}。
- **副作用**：出站房间查询。
- **源码**：`abgiSSE/wsclient.go`。

<a id="api-167"></a>

### POST /api/abgiSSE/HomeownerCreateGroup

- URL：`{baseUrl}/api/abgiSSE/HomeownerCreateGroup`。
- **用途**：代理房主创建房间
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body name:string必填非空；description:string可选默认空；uid:string会被本地加密UID覆盖。
- **返回**：200 JSON {message:string}，字符串来自远端创建结果。
- **错误**：400 JSON {error:string}绑定/name缺失；加密失败200 {error:"数据错误"}；远端失败仍200 {message:string}。
- **副作用**：在远端创建房间。
- **源码**：`abgiSSE/wsclient.go`。

<a id="api-168"></a>

### GET /api/abgiSSE/getRoomLogs

- URL：`{baseUrl}/api/abgiSSE/getRoomLogs`。
- **用途**：查询远端当日房间日志并隐藏服务器凭据
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query group_name:string必填trim后非空。
- **返回**：HTTP状态与远端保持一致，JSON为远端完整原始值；本地不定义/裁剪成功schema，不能承诺固定fields。调用者应按所连联机服务版本的HomeownerRoomLogs契约解析，本文不编造日志条目字段。
- **错误**：400 {error:string}缺房间/密钥；500加密或生成请求；502网络/读取失败或成功却非JSON；远端非JSON错误改{error:string}并保留错误状态。
- **副作用**：以本地配置UID加密后POST远端，仅代理查询；超时15秒。
- **源码**：`abgiSSE/wsclient.go`。

<a id="api-169"></a>

### POST /api/abgiSSE/clearNumberOfLaunches

- URL：`{baseUrl}/api/abgiSSE/clearNumberOfLaunches`。
- **用途**：清零本机联机发车计数
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"received",data:"清0成功"}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：修改联机共享计数。
- **源码**：`menu/routes_runtime.go`。

<a id="api-170"></a>

### GET /api/abgiSSE/getOnlineStatus

- URL：`{baseUrl}/api/abgiSSE/getOnlineStatus`。
- **用途**：读取联机状态文本快照
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON顶层string，例“在线”或连接/拒绝原因；无status/data壳。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`menu/routes_runtime.go；abgiSSE/wsclient.go`。

<a id="api-171"></a>

### POST /api/abgiSSE/sendMessage

- URL：`{baseUrl}/api/abgiSSE/sendMessage`。
- **用途**：向当前联机WebSocket发送消息
- **认证**：apiKey。
- **请求格式**：无请求体；msg在Query，不是JSON。
- **参数**：Query msg:string，当前未做非空校验。
- **返回**：200 JSON {status:"received",data:"发送成功"}。
- **错误**：400 JSON {status:"received",data:string}为发送失败原因（status字段仍received）。
- **副作用**：向联机服务器发送消息。
- **源码**：`menu/routes_runtime.go`。

<a id="api-172"></a>

### GET /api/NumberOfLaunches

- URL：`{baseUrl}/api/NumberOfLaunches`。
- **用途**：查询本机联机发车次数
- **认证**：公开，无需认证。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {number:integer}。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`abgiSSE/aBgiSEEService.go`。

<a id="api-173"></a>

### GET /api/index

- URL：`{baseUrl}/api/index`。
- **用途**：查询首页运行进度摘要
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {group:string,ExpectedToEnd:string,line:string,scriptName:string,progress:string,running:boolean,jsProgress:string,title:string,isAdmin:boolean}；group含组进度，ExpectedToEnd为多行起始/时长/已运行/预计时间文本；字段大小写按原样。
- **错误**：除公共认证错误外，无显式业务错误响应。
- **副作用**：无；只读查询。
- **源码**：`bgiStatus/logService.go；bgiStatus/BgiLogStatus.go`。

<a id="api-174"></a>

### GET /api/appInfo

- URL：`{baseUrl}/api/appInfo`。
- **用途**：查询App展示的版本与联机次数
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{"上线次数":integer,"aBgi版本":string,"最新aBgi版本":string,"bgi版本":string,"最新bgi版本":string|{version:string,created_at:string,browser_download_url:string,size:integer}}}；非lcb分支最新bgi版本为版本信息对象，lcb分支为字符串。
- **错误**：版本查询错误被忽略，字段可能为空或零值，仍200。
- **副作用**：请求远端版本源。
- **源码**：`menu/App.go；bgiStatus/uploadBgi.go`。

<a id="api-175"></a>

### POST /api/indexSX

- URL：`{baseUrl}/api/indexSX`。
- **用途**：安排当前实例安全重启
- **认证**：apiKey。
- **请求格式**：application/json请求体。
- **参数**：Body instanceId:string必填且等于当前进程实例；从/api/aBgiUpdate/version获取对应实例信息。
- **返回**：202 JSON {success:true,restartScheduled:true,instanceId:string,targetVersion:string,msg:string}；Cache-Control:no-store。
- **错误**：400 {success:false,msg:string}格式；409实例已变化/已有更新重启任务；500 {success:false,restartScheduled:false,msg:string}助手握手失败。
- **副作用**：先准备重启助手，成功响应flush之后退出并重启ABGI；禁止自动重试旧实例请求。
- **源码**：`menu/restart_service.go`。

<a id="api-176"></a>

### GET /api/indexSX

- URL：`{baseUrl}/api/indexSX`。
- **用途**：阻止旧客户端使用GET重启
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：405 JSON {success:false,msg:"重启接口已更新，请强制刷新页面后重试"}；Allow: POST。
- **错误**：本接口总是405，不触发重启。
- **副作用**：无。
- **源码**：`menu/restart_service.go`。

<a id="api-177"></a>

### GET /api/archiveList

- URL：`{baseUrl}/api/archiveList`。
- **用途**：查询全部归档记录
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON顶层RuntimeArchive[]，无data壳。
- **错误**：处理器没有显式错误响应，底层查询结果直接返回。
- **副作用**：无。
- **源码**：`menu/routes_runtime.go；models/models.go`。

<a id="api-178"></a>

### DELETE /api/archive

- URL：`{baseUrl}/api/archive`。
- **用途**：删除一个归档记录
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query id:integer必填可被Atoi解析（未额外校验正数）。
- **返回**：200 文本“删除成功”。
- **错误**：400文本“无效的ID”；500文本“删除失败”。
- **副作用**：按id删除归档数据库记录。
- **源码**：`menu/routes_runtime.go`。

<a id="api-179"></a>

### DELETE /api/allArchives

- URL：`{baseUrl}/api/allArchives`。
- **用途**：删除全部归档记录
- **认证**：apiKey。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 文本“全部删除成功”。
- **错误**：500文本“全部删除失败”。
- **副作用**：永久清空所有归档数据库记录。
- **源码**：`menu/routes_runtime.go`。

<a id="api-180"></a>

### POST /api/closeBgi

- URL：`{baseUrl}/api/closeBgi`。
- **用途**：关闭BetterGI及原神
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"received",data:"BGI关闭成功"}。
- **错误**：handler未汇报进程关闭结果，无单独业务错误响应。
- **副作用**：调用CloseSoftware及CloseYuanShen终止相关进程。
- **源码**：`menu/routes_runtime.go`。

<a id="api-181"></a>

### POST /api/closeYuanShen

- URL：`{baseUrl}/api/closeYuanShen`。
- **用途**：关闭原神
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"received",data:"原神关闭成功"}。
- **错误**：handler未汇报关闭结果，无单独业务错误响应。
- **副作用**：终止原神进程。
- **源码**：`menu/routes_runtime.go`。

<a id="api-182"></a>

### POST /api/sendImage

- URL：`{baseUrl}/api/sendImage`。
- **用途**：截图并按当前通知配置发送
- **认证**：apiKey。
- **请求格式**：无请求体。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"received",data:"发送成功"}。
- **错误**：400 JSON {status:"received",data:"截图失败"}；发送失败同400但data直接序列化error对象，结构不保证为可读string。
- **副作用**：保存本地jt.jpg并发送通知，可能涉及外部平台。
- **源码**：`menu/routes_runtime.go`。

<a id="api-183"></a>

### GET /api/session-actions/status

- URL：`{baseUrl}/api/session-actions/status`。
- **用途**：查看会话启动功能启用原因及本机模板管理资格
- **认证**：会话Bearer。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:{enabled:boolean,configured:boolean,canManageTemplates:boolean,strongCredentials:boolean,reasons:string[],reason:string}}；reasons可能含SESSION_ACTIONS_DISABLED/RDP_FEATURE_DISABLED/EMBEDDED_MODE_REQUIRED/STRONG_CREDENTIALS_REQUIRED/TASK_SCHEDULER_UNAVAILABLE。
- **错误**：仅会话Bearer/同源公共错误；本接口不要求功能开启。
- **副作用**：无。
- **源码**：`SessionActions/http.go`。

<a id="api-184"></a>

### GET /api/session-actions/catalog

- URL：`{baseUrl}/api/session-actions/catalog`。
- **用途**：查询可从远端启动的已启用模板摘要
- **认证**：会话Bearer＋会话可用条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeSessionCatalog[]}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：无；不返回路径和参数。
- **源码**：`SessionActions/http.go`。

<a id="api-185"></a>

### GET /api/session-actions/runs

- URL：`{baseUrl}/api/session-actions/runs`。
- **用途**：分页查询会话程序启动审计记录
- **认证**：会话Bearer＋会话可用条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Query page:integer默认1；pageSize:integer默认20，上限100；非法/小于1采用默认。
- **返回**：200 JSON {status:"success",data:{items:RuntimeSessionRun[],page:integer,pageSize:integer,total:integer}}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：无。
- **源码**：`SessionActions/http.go`。

<a id="api-186"></a>

### GET /api/session-actions/runs/:id

- URL：`{baseUrl}/api/session-actions/runs/:id`。
- **用途**：读取一次会话程序启动状态
- **认证**：会话Bearer＋会话可用条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填trim后作为运行ID。
- **返回**：200 JSON {status:"success",data:RuntimeSessionRun}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：无。
- **源码**：`SessionActions/http.go`。

<a id="api-187"></a>

### POST /api/session-actions/runs

- URL：`{baseUrl}/api/session-actions/runs`。
- **用途**：在指定本地RDP档案账户中异步启动已登记模板
- **认证**：会话Bearer＋会话可用条件＋最近10分钟登录。
- **请求格式**：application/json，最多256KiB，必须且仅一个对象；字段大小写严格，拒绝未知/重复字段。
- **参数**：Body profileId:string必填非空；actionId:string必填非空；只允许固定模板，不接受exe/参数自由覆盖。
- **返回**：202 JSON {status:"accepted",data:RuntimeSessionRun}，初态queued；重复活动任务409 {code:409,errorCode:"ACTION_ALREADY_RUNNING",error:string,data:RuntimeSessionRun}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回；每操作者/直连IP各最多每分钟5次，否则429 RATE_LIMITED；JWT超过10分钟403 RECENT_AUTH_REQUIRED。
- **副作用**：写启动审计；按需启动RDP档案、等待目标会话并通过Windows任务计划程序启动模板；需轮询确认，禁止将202等同启动成功。
- **源码**：`SessionActions/http.go；SessionActions/coordinator.go`。

<a id="api-188"></a>

### GET /api/session-actions/templates

- URL：`{baseUrl}/api/session-actions/templates`。
- **用途**：在本机查询全部会话程序模板
- **认证**：会话Bearer＋会话本机管理条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：无业务参数。
- **返回**：200 JSON {status:"success",data:RuntimeSessionTemplate[]}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：无；只允许本机来源，即使只读也要求最近登录。
- **源码**：`SessionActions/http.go`。

<a id="api-189"></a>

### POST /api/session-actions/templates

- URL：`{baseUrl}/api/session-actions/templates`。
- **用途**：在本机创建固定可执行模板
- **认证**：会话Bearer＋会话本机管理条件。
- **请求格式**：application/json，最多256KiB，必须且仅一个对象；字段大小写严格，拒绝未知/重复字段。
- **参数**：Body RuntimeSessionTemplateInput，enabled缺省true。
- **返回**：201 JSON {status:"success",data:RuntimeSessionTemplate}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：写数据库模板；不立即运行程序。
- **源码**：`SessionActions/http.go；SessionActions/validation.go`。

<a id="api-190"></a>

### GET /api/session-actions/templates/:id

- URL：`{baseUrl}/api/session-actions/templates/:id`。
- **用途**：在本机读取完整模板
- **认证**：会话Bearer＋会话本机管理条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：200 JSON {status:"success",data:RuntimeSessionTemplate}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：无；返回本地exe及参数，仅本机可读。
- **源码**：`SessionActions/http.go`。

<a id="api-191"></a>

### PUT /api/session-actions/templates/:id

- URL：`{baseUrl}/api/session-actions/templates/:id`。
- **用途**：完整更新固定可执行模板
- **认证**：会话Bearer＋会话本机管理条件。
- **请求格式**：application/json，最多256KiB，必须且仅一个对象；字段大小写严格，拒绝未知/重复字段。
- **参数**：Path id:string必填；Body RuntimeSessionTemplateInput完整提交；enabled缺省保留旧值，description/arguments等普通字段缺省会清空/默认。
- **返回**：200 JSON {status:"success",data:RuntimeSessionTemplate}。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：覆盖数据库模板，执行前仍会重新验证目标文件。
- **源码**：`SessionActions/http.go；SessionActions/validation.go`。

<a id="api-192"></a>

### DELETE /api/session-actions/templates/:id

- URL：`{baseUrl}/api/session-actions/templates/:id`。
- **用途**：在本机删除会话程序模板
- **认证**：会话Bearer＋会话本机管理条件。
- **请求格式**：无请求体；查询参数按URL编码。
- **参数**：Path id:string必填。
- **返回**：204 无响应体。
- **错误**：遵循会话公共错误；400 INVALID_REQUEST/INVALID_ACTION_TEMPLATE；404 ACTION_NOT_FOUND/PROFILE_NOT_FOUND/RUN_NOT_FOUND；409 AMBIGUOUS_RDP_SESSION/CONSOLE_SESSION_REJECTED；504 SESSION_START_TIMEOUT；500 INTERNAL_ERROR，均{code,errorCode,error}，仅实际触发分支返回。
- **副作用**：删除模板，不承诺终止此前已启动的程序。
- **源码**：`SessionActions/http.go`。

#### RDP桌面WebSocket协议补充

- 客户端输入对象RuntimeRDPDesktopInput：`type:string`必填，可为mouseMove/mouseDown/mouseUp/wheel/keyDown/keyUp/text/shortcut；`x/y:integer`鼠标坐标会裁剪到桌面；`button:integer`鼠标键；`delta:number`滚轮限制-10到10；`scanCode:integer`键盘扫描码1–0xE0FF；`text:string`必填非空，单次最多512个UTF-16单元；`shortcut:string`为showDesktop或taskView。各字段只在对应type下需要。输入JSON最大4096字节。
- 服务端二进制每帧前16字节：0–3为ASCII ARF1；4–5 x、6–7 y、8–9 width、10–11 height均uint16小端；12–15 sequence为uint32小端；16起JPEG数据。帧可为桌面局部更新，客户端按坐标合成，不是每次全屏JPEG。
- 每20秒WebSocket Ping，读Pong延长60秒读期限；同源校验允许无Origin/同Host/同X-Forwarded-Host。原生浏览器WebSocket不能自定义apiKey头时，主鉴权已有token/tk JWT查询方式可用，不能把API Key直接塞进token。

#### 日志编排行为参数补充

`RuntimeLogRuleInput.actions[].data`是字符串参数，各行为目录由GET /api/logKeyword/behaviors实时提供。run_managed_script/run_managed_queue须非空昵称/队列名；start_rdp_profile/logoff_rdp_profile须非空档案；start_multi_user/logoff_multi_user须非空用户名；run_maa_pipeline须Pipeline参数；run_session_action须包含profileId和actionId的严格JSON字符串且不支持日志模板；keyboard_operation及OBS行为有独立校验。不能把任意行为都视为data可空。源码：`LogKeyword/LogKeywordService.go`。

<a id="section-09"></a>

## 一条龙与脚本文档

源码：`menu/routes_tasks.go`、`menu/oneLong_handlers.go`、`OneLong/oneLong.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-193"></a>

### POST /api/oneLong/startOneLong

- URL：`{baseUrl}/api/oneLong/startOneLong`。
- 用途：启动指定一条龙。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json，根值为字符串而非对象。
- 参数：body根值：string 必填，去空白后非空，为配置名称。
- 返回：HTTP200，application/json；`{status:"success",msg:"启动成功"}`。
- 错误：400 `{status:"error",code:"ONE_DRAGON_NAME_REQUIRED",error:string}`；500 code=ONE_DRAGON_START_FAILED；另见公共401鉴权错误。
- 副作用：关闭BetterGI，按设置关闭原神，然后启动统一一条龙流程。

示例（占位值）：
```json
"<一条龙配置名>"
```

<a id="api-194"></a>

### POST /api/oneLong/continueOneLong

- URL：`{baseUrl}/api/oneLong/continueOneLong`。
- 用途：从原生NextTaskId书签继续一条龙。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.name：string 必填非空，配置名称。
- 返回：HTTP200，application/json；`{status:"success",msg:string,configName:string,nextTaskId:string,nextTaskName:string}`，分别为提示、配置名和继续目标ID/名称。
- 错误：400 名称缺失；404 ONE_DRAGON_NOT_FOUND；409 ONE_DRAGON_FORMAT_UNSUPPORTED；422 ONE_DRAGON_RESUME_NOT_SET/ONE_DRAGON_RESUME_TASK_NOT_FOUND/ONE_DRAGON_RESUME_TASK_DISABLED/ONE_DRAGON_CONFIG_INVALID；500 ONE_DRAGON_START_FAILED；JSON `{status:"error",code:string,error:string}`；另见公共401鉴权错误。
- 副作用：校验书签后关闭/启动程序，书签由BGI消费。

示例（占位值）：
```json
{"name":"<一条龙配置名>"}
```

<a id="api-195"></a>

### GET /api/oneLong/groupProgress

- URL：`{baseUrl}/api/oneLong/groupProgress`。
- 用途：从最近运行日志查询组完成情况。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.name：string 必填非空。
- 返回：HTTP200，application/json；`{status:"success",data:bgiStatus.OneDragonGroupProgress}`，详见结构附录。
- 错误：400 缺名称；404 配置不存在；409 格式不支持；422 配置无效；500 日志分析错误；JSON `{status:"error",code:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-196"></a>

### GET /api/oneLong/oneLongAllName

- URL：`{baseUrl}/api/oneLong/oneLongAllName`。
- 用途：列出一条龙配置名称。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:string[]|null}`，移除.json后缀，目录读取失败返回[]。
- 错误：无专用错误，读取失败按空列表处理；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-197"></a>

### GET /api/md

- URL：`{baseUrl}/api/md`。
- 用途：读取仓库脚本README或战斗/七圣召唤文档。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.filePath：string，处理器未强制必填，仓库相对路径如js/<目录>或combat/<文件>。
- 返回：HTTP200，application/json；`{status:"success",data:string}`，data为Markdown原文或缺少文档提示文字。
- 错误：读取失败通常仍200并以data文本提示；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="section-10"></a>

## 抽卡记录

源码：`GachaLog/handler.go`、`GachaLog/types.go`、`models/GachaLogModel.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-198"></a>

### POST /api/gacha/sync

- URL：`{baseUrl}/api/gacha/sync`。
- 用途：从本地缓存或祈愿链接增量同步记录。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.source：string 必填local/url；url：string，source=url时必填有效完整祈愿链接，local时不需要。
- 返回：HTTP200，application/json；`{code:200,data:GachaLog.SyncResult}`，partial表示部分池失败，pools含各池结果。
- 错误：400 参数/链接无效；404 安装路径、缓存或账号不存在；408 请求取消；409 UID不匹配；422 链接过期或无记录；502 上游失败；500 数据库错误。JSON `{code:number,error_code:string,error:string}`；另见公共401鉴权错误。
- 副作用：读取本地缓存或访问米哈游，向本地数据库写入记录；url中的authkey属敏感凭据。

示例（占位值）：
```json
{"source":"local"}
```

<a id="api-199"></a>

### GET /api/gacha/accounts

- URL：`{baseUrl}/api/gacha/accounts`。
- 用途：查询本地已同步账号。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{code:200,data:GachaLog.AccountDTO[]}`。
- 错误：400 参数/链接无效；404 安装路径、缓存或账号不存在；408 请求取消；409 UID不匹配；422 链接过期或无记录；502 上游失败；500 数据库错误。JSON `{code:number,error_code:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-200"></a>

### GET /api/gacha/summary

- URL：`{baseUrl}/api/gacha/summary`。
- 用途：查询UID抽卡统计及保底。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.uid：string 必填；query.pool：integer 可选默认0，0/100/200/301/302/500代表全部/新手/常驻/角色/武器/集录，角色池合并原始301和400。
- 返回：HTTP200，application/json；`{code:200,data:GachaLog.Summary}`，未知统计值可为null。
- 错误：400 参数/链接无效；404 安装路径、缓存或账号不存在；408 请求取消；409 UID不匹配；422 链接过期或无记录；502 上游失败；500 数据库错误。JSON `{code:number,error_code:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-201"></a>

### GET /api/gacha/records

- URL：`{baseUrl}/api/gacha/records`。
- 用途：分页查询抽卡明细。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.uid：string 必填；query.pool：integer 可选默认0，0/100/200/301/302/500代表全部/新手/常驻/角色/武器/集录，角色池合并原始301和400；rank：integer默认0，允许0/3/4/5；page：integer默认1且>=1；page_size：integer默认20，1..100；keyword：string可选，最多100字符。
- 返回：HTTP200，application/json；`{code:200,data:GachaLog.RecordPage}`，list项为models.GachaRecord。
- 错误：400 参数/链接无效；404 安装路径、缓存或账号不存在；408 请求取消；409 UID不匹配；422 链接过期或无记录；502 上游失败；500 数据库错误。JSON `{code:number,error_code:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="section-11"></a>

## 米游社扫码、角色缓存与旧配置

源码：`Mihoyo/service.go`、`Mihoyo/config_fill.go`、`Mihoyo/character_info.go`、`MysSignIn/handlers.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-202"></a>

### POST /api/mysSignIn

- URL：`{baseUrl}/api/mysSignIn`。
- 用途：旧兼容入口异步签到所有启用账号。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",message:"米游社签到任务已启动",batchId:string}`，不代表完成。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：写入批次及日志，后台执行签到。

<a id="api-203"></a>

### GET /api/ReadCk

- URL：`{baseUrl}/api/ReadCk`。
- 用途：读取BetterGI配置的米游社CK。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:200,message:string}`，message为完整Cookie，敏感返回。
- 错误：无额外业务错误；另见公共401鉴权错误。
- 副作用：只读，但回传真实凭据，不得打印/公开。

<a id="api-204"></a>

### GET /api/MysCharacterInfo

- URL：`{baseUrl}/api/MysCharacterInfo`。
- 用途：读取角色面板缓存。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.uid、region：string 可选，缺省优先当前游戏角色，否则选择最新匹配缓存。
- 返回：HTTP200，application/json；`{status:"success",message:"获取角色信息成功",data:Mihoyo.CharacterInfoResult}`。
- 错误：400 JSON `{status:"error",message:string}` 缓存不存在/读取失败；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-205"></a>

### POST /api/MysCharacterInfo/Refresh

- URL：`{baseUrl}/api/MysCharacterInfo/Refresh`。
- 用途：从米游社刷新角色面板。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，不读取JSON body。
- 参数：query.cookie：string可选，缺省读取mihoyo_login_result.txt第一行；uid、region：string可选，角色定位条件。
- 返回：HTTP200，application/json；`{status:"success",message:"更新角色信息成功",data:Mihoyo.CharacterInfoResult}`。
- 错误：400 凭据/远端查询失败；500 缓存保存失败；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：访问米游社并保存本地缓存；Cookie放URL有日志泄露风险。

<a id="api-206"></a>

### POST /api/MysConfig/FillLogin

- URL：`{baseUrl}/api/MysConfig/FillLogin`。
- 用途：写入签到CK/SK并同步BetterGI CK。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.login_result：string必填，多行登录结果，须解析为同账号CK/SK。
- 返回：HTTP200，application/json；`{status:"success",message:"已写入 CK/SK 和 BetterGI CK"}`。
- 错误：400 格式/账号不一致；500 文件写入失败，可能部分成功；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：改写签到工具及BetterGI用户配置。

<a id="api-207"></a>

### POST /api/MysConfig/FillCookie

- URL：`{baseUrl}/api/MysConfig/FillCookie`。
- 用途：只更新扫码CK并保留SK。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.login_result：string必填，包含有效Cookie的登录结果。
- 返回：HTTP200，application/json；`{status:"success",message:"已写入扫码 CK，现有 SK 保持不变"}`。
- 错误：400 Cookie无效；409 与原SK账号/MID不一致；500写入失败，可能部分成功；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：改写签到CK及BetterGI CK。

<a id="api-208"></a>

### POST /api/MysConfig/FillBetterGICookie

- URL：`{baseUrl}/api/MysConfig/FillBetterGICookie`。
- 用途：只更新BetterGI CK。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.login_result：string必填，包含Cookie的登录结果。
- 返回：HTTP200，application/json；`{status:"success",message:"已写入 BetterGI CK"}`。
- 错误：400格式/Cookie缺失；500写入失败；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：改写BetterGI用户配置。

<a id="api-209"></a>

### POST /api/MysConfig/FillCheckin

- URL：`{baseUrl}/api/MysConfig/FillCheckin`。
- 用途：保存旧工具签到选项。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.checkin_list：integer[]必填且至少一项，允许1/2/3/4/5/6/8，其他值拒绝；重复项去重，按内置游戏顺序保存。
- 返回：HTTP200，application/json；`{status:"success",message:"已写入需要签到的游戏"}`。
- 错误：400 JSON/选项无效；500写入失败；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：改写MihoyoBBSTools/config/config.yaml。

<a id="api-210"></a>

### POST /api/Qrcode

- URL：`{baseUrl}/api/Qrcode`。
- 用途：申请扫码登录二维码。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{ticket:string,device:string,app_id:0,qr_image:string,tips:string}`；qr_image为PNG data URL，ticket/device用于GET /api/Check（大小写以路由为准）。
- 错误：500 `{error:string}` 远端申请失败；另见公共401鉴权错误。
- 副作用：申请远端票据，票据按敏感值处理。

<a id="api-211"></a>

### GET /api/Check

- URL：`{baseUrl}/api/Check`。
- 用途：轮询扫码登录状态。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.ticket、device：string必填，来自Qrcode；app_id服务器固定0。
- 返回：HTTP200，application/json；`{status:"pending"|"scanned"|"success"|"expired",data?:string,file?:string,message?:string}`；success的data为多行CK/SK，file是保存提示不是下载URL。
- 错误：400 `{error:"参数不足"}`；502 `{status:"failed",error:string}`；过期仍可能HTTP200；另见公共401鉴权错误。
- 副作用：成功时保存mihoyo_login_result.txt，响应包含凭据不得公开。

<a id="section-12"></a>

## 米游社多账号签到

源码：`MysSignIn/handlers.go`、`MysSignIn/types.go`、`MysSignIn/accounts.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-212"></a>

### GET /api/mys-sign/accounts

- URL：`{baseUrl}/api/mys-sign/accounts`。
- 用途：列出签到账号及最近运行。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",accounts:MysSignIn.Account[]}`，凭据为掩码。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-213"></a>

### POST /api/mys-sign/accounts

- URL：`{baseUrl}/api/mys-sign/accounts`。
- 用途：创建独立签到账号。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.loginResult：string必填完整登录结果；name：string可选默认掩码UID名，最多64字符；enabled：boolean可选默认true；settings：MysSignIn.AccountSettings可选，缺省采用内置模板。
- 返回：HTTP201，application/json；`{status:"success",account:MysSignIn.Account}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：保存独立账号YAML，拒绝重复UID。

示例（占位值）：
```json
{"name":"<备注>","loginResult":"<登录结果>","enabled":true}
```

<a id="api-214"></a>

### PATCH /api/mys-sign/accounts/:id

- URL：`{baseUrl}/api/mys-sign/accounts/:id`。
- 用途：部分更新账号设置。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：path.id：string必填32位小写十六进制；body.name：string可选1..64字符；enabled：boolean可选；community：MysSignIn.CommunitySettings可选；games：MysSignIn.GameSettings可选；未提交顶层字段保留，提交子设置按整个子设置替换。
- 返回：HTTP200，application/json；`{status:"success",account:MysSignIn.Account}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：写入账号YAML，执行中账号不可改。

<a id="api-215"></a>

### PUT /api/mys-sign/accounts/:id/credentials

- URL：`{baseUrl}/api/mys-sign/accounts/:id/credentials`。
- 用途：替换签到账号凭据。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：path.id：string必填32位小写十六进制；body.loginResult：string必填，须同原UID/MID。
- 返回：HTTP200，application/json；`{status:"success",account:MysSignIn.Account}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：改写账号CK/SK，运行中拒绝。

<a id="api-216"></a>

### DELETE /api/mys-sign/accounts/:id

- URL：`{baseUrl}/api/mys-sign/accounts/:id`。
- 用途：删除签到账号。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：path.id：string必填32位小写十六进制。
- 返回：HTTP200，application/json；`{status:"success",message:"账号已删除"}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：删除账号YAML配置及该账号的全部签到运行日志；运行中拒绝。配置删除后日志清理失败可能部分成功。

<a id="api-217"></a>

### GET /api/mys-sign/accounts/:id/logs

- URL：`{baseUrl}/api/mys-sign/accounts/:id/logs`。
- 用途：读取账号最近10条日志。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：path.id：string必填，已有账号ID。
- 返回：HTTP200，application/json；`{status:"success",logs:models.MysSignRunLog[]}`，最新在前、summary/output脱敏。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-218"></a>

### POST /api/mys-sign/runs

- URL：`{baseUrl}/api/mys-sign/runs`。
- 用途：异步执行指定或全部启用账号。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json，可空请求体。
- 参数：body.accountIds：string[]可选，缺省/[]选所有启用账号；显式ID须存在且启用，重复去重，实际按名称排序。
- 返回：HTTP202，application/json；`{status:"queued",batchId:string}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：写入批次与账号日志，后台串行签到；仅一个活动批次。

示例（占位值）：
```json
{"accountIds":["<32位账号ID>"]}
```

<a id="api-219"></a>

### GET /api/mys-sign/runs/:batchId

- URL：`{baseUrl}/api/mys-sign/runs/:batchId`。
- 用途：查询批次及各账号执行状态。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：path.batchId：string必填，创建时返回的批次ID。
- 返回：HTTP200，application/json；`{status:"success",batch:MysSignIn.BatchStatus}`。
- 错误：400 参数无效、404 不存在、409 重复UID/账号运行中/批次占用（可含activeBatchId:string）、503 管理器未就绪、500 查询失败；JSON `{status:"error",message:string,activeBatchId?:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="section-13"></a>

## 战斗策略与角色培养

源码：`AutoFightStrategyService/service.go`、`CharacterPlanService/Service.go`、`CharacterPlanService/CharacterInformation.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-220"></a>

### GET /api/autoFightStrategy/characters

- URL：`{baseUrl}/api/autoFightStrategy/characters`。
- 用途：查询内置策略角色。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:characterDevelopment.DefaultAutoFightCharacterOption[]}`。
- 错误：无专用业务错误；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-221"></a>

### GET /api/autoFightStrategy/universal-json

- URL：`{baseUrl}/api/autoFightStrategy/universal-json`。
- 用途：构造内置万能JSON策略。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:{strategy:combatStrategy.JSONDocument,characterCount:integer,actionCount:integer,cooldowns:combatStrategy.CombatCooldown[],profileStats:{shieldCount:integer,healerCount:integer,claymoreCount:integer,attackProfileCount:integer,energyAttackCount:integer,specialRotationCount:integer}}}`；计数分别表示角色、动作、护盾/治疗/大剑/攻击画像/产能攻击/特殊轮转数量。
- 错误：无专用业务错误；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-222"></a>

### GET /api/autoFightStrategy/candidates

- URL：`{baseUrl}/api/autoFightStrategy/candidates`。
- 用途：查询单角色策略候选。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.name：string必填，有效内置角色名。
- 返回：HTTP200，application/json；`{status:"success",data:characterDevelopment.DefaultAutoFightCandidate[]}`。
- 错误：400 `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-223"></a>

### GET /api/autoFightStrategy/generated

- URL：`{baseUrl}/api/autoFightStrategy/generated`。
- 用途：读取已生成策略。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.format：string可选默认txt，仅txt/json；fileName：string，json时必填，无路径和Windows保留名，缺扩展名补.json；txt忽略此值并固定ABGI生成策略.txt。
- 返回：HTTP200，application/json；`{status:"success",data:{exists:boolean,content:string,format:string,fileName:string}}`；文件不存在时exists=false。
- 错误：400 `{status:"error",message:string}` 格式/文件名/读取失败；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-224"></a>

### POST /api/autoFightStrategy/generate

- URL：`{baseUrl}/api/autoFightStrategy/generate`。
- 用途：校验并覆盖保存策略。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.format：string可选默认txt，仅txt/json；fileName：string，json时必填，无路径和Windows保留名，缺扩展名补.json；txt忽略此值并固定ABGI生成策略.txt；content：string必填非空，txt策略文本或JSON序列化文本；characterNames：string[]和characterName：string可选合并去重，txt要求至少1个有效内置角色，json不要求角色。
- 返回：HTTP200，application/json；`{status:"success",message:"生成成功",data:{characterNames:string[],format:string,fileName:string}}`。
- 错误：400 `{status:"error",message:string}` 参数/内容/指令/文件错误；另见公共401鉴权错误。
- 副作用：覆盖BetterGI/User/AutoFight目标文件，不运行战斗。

<a id="api-225"></a>

### GET /api/characterPlan/list

- URL：`{baseUrl}/api/characterPlan/list`。
- 用途：查询培养计划列表。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:models.CharacterPlan[]|null}`，ID倒序。
- 错误：500 `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-226"></a>

### GET /api/characterPlan/detail

- URL：`{baseUrl}/api/characterPlan/detail`。
- 用途：查询培养计划详情。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数，数据库ID。
- 返回：HTTP200，application/json；`{status:"success",data:models.CharacterPlan}`。
- 错误：400 ID无效；404不存在；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-227"></a>

### POST /api/characterPlan/add

- URL：`{baseUrl}/api/characterPlan/add`。
- 用途：新增培养计划并同步脚本。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.characterNames：string[]优先，或characterName：string，至少1名最多3名有效角色，去重后逗号保存；rankedGroup：string必填已有配置组；oneLong：string可选默认空；isRanked、isTalent：boolean可选默认true；id：integer，新增忽略/更新必填正数。
- 返回：HTTP200，application/json；`{status:"success",data:models.CharacterPlan}`。
- 错误：400参数/脚本/同步失败；500数据库错误；JSON `{status:"error",message:string,error?:string}`；另见公共401鉴权错误。
- 副作用：写入计划并同步配置组的养成脚本，失败尝试删除新增记录。

<a id="api-228"></a>

### POST /api/characterPlan/update

- URL：`{baseUrl}/api/characterPlan/update`。
- 用途：修改培养计划及配置组绑定。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.characterNames：string[]优先，或characterName：string，至少1名最多3名有效角色，去重后逗号保存；rankedGroup：string必填已有配置组；oneLong：string可选默认空；isRanked、isTalent：boolean可选默认true；id：integer，新增忽略/更新必填正数。
- 返回：HTTP200，application/json；`{status:"success",data:models.CharacterPlan}`。
- 错误：400参数/同步失败；404不存在；500数据库错误；JSON `{status:"error",message:string,error?:string}`；另见公共401鉴权错误。
- 副作用：修改数据库并同步旧/新组；同步报错不代表数据库未变更。

<a id="api-229"></a>

### DELETE /api/characterPlan/delete

- URL：`{baseUrl}/api/characterPlan/delete`。
- 用途：删除培养计划。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数。
- 返回：HTTP200，application/json；`{status:"success",message:"删除成功"}`。
- 错误：400参数/同步失败；404不存在；500数据库错误；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：删除后重新同步配置组，后续同步失败可能已删除。

<a id="api-230"></a>

### POST /api/characterPlan/run

- URL：`{baseUrl}/api/characterPlan/run`。
- 用途：计算并应用培养计划。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无JSON body。
- 参数：query.id：integer必填正数。
- 返回：HTTP200，application/json；`{status:"success",message:"角色培养计划执行完成",data:{name:string,level:integer,talents:integer[3],savedRanked:boolean,savedTalent:boolean,finalRanked:boolean,finalTalent:boolean,reasons:string[],oneLong:string,rankedGroup:string,defaultTalent:string}}`；saved为保存选项，final为计算实际选项，reasons为调整原因。
- 错误：400计算/执行错误；404不存在；JSON `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：按面板生成实际计划，写入角色养成脚本及关联一条龙配置，并立即调用一条龙启动；不是只读预览。底层部分错误仅写日志并返回空name，HTTP200不保证实际启动成功。

<a id="api-231"></a>

### GET /api/characterPlan/characters

- URL：`{baseUrl}/api/characterPlan/characters`。
- 用途：从最近面板构建角色选择列表。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:CharacterPlanService.characterOption[]}`。
- 错误：400 `{status:"error",message:string}` 缓存/解析失败；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-232"></a>

### GET /api/characterPlan/info

- URL：`{baseUrl}/api/characterPlan/info`。
- 用途：查询角色等级、天赋及材料缺口。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.name：string必填；uid/region：string可选但必须同时提供，缺省用当前角色；bagAccount：string可选，缺省默认扫描用户，显式空串选根目录兼容数据；uid/region/bagAccount禁止重复。
- 返回：HTTP200，application/json；`{status:"success",data:CharacterPlanService.CharacterInformation}`。
- 错误：400 `{status:"error",message:string}` 身份、编码、重复参数、面板/库存缺失或需求计算错误；另见公共401鉴权错误。
- 副作用：只读计算，可能访问或刷新需求缓存。

<a id="section-14"></a>

## 定时任务

源码：`TaskCron/TaskCronService.go`、`TaskCron/TaskCron.go`、`models/models.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-233"></a>

### GET /api/taskCron/list

- URL：`{baseUrl}/api/taskCron/list`。
- 用途：列出定时任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json，直接 `models.TaskCron[]`，无列表时可能HTTP400返回[]。
- 错误：400空列表，响应仍为数组；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-234"></a>

### POST /api/taskCron/add

- URL：`{baseUrl}/api/taskCron/add`。
- 用途：新增定时任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.name：string必填，来自getTasks；spec：string必填有效cron（支持秒字段）；data：string可选，格式依任务，可为JSON文本；mark：string可选备注；其他字段不用于创建。
- 返回：HTTP200 text/plain; charset=utf-8，`任务已添加`。
- 错误：400文本 参数错误/任务名不存在/任务参数错误；500文本 添加失败；另见公共401鉴权错误。
- 副作用：保存数据库并注册未来自动执行任务。

<a id="api-235"></a>

### POST /api/taskCron/remove

- URL：`{baseUrl}/api/taskCron/remove`。
- 用途：删除定时任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无JSON body。
- 参数：query.id：integer必填，数据库主键（不是entry_id）。
- 返回：HTTP200 text/plain；任务已删除。
- 错误：400缺id；500非法id/删除失败；另见公共401鉴权错误。
- 副作用：删除数据库和调度项。

<a id="api-236"></a>

### POST /api/taskCron/pause

- URL：`{baseUrl}/api/taskCron/pause`。
- 用途：暂停定时任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无JSON body。
- 参数：query.id：integer必填，数据库主键（不是entry_id）。
- 返回：HTTP200 text/plain；任务已暂停。
- 错误：400缺id/非法id/暂停失败；另见公共401鉴权错误。
- 副作用：保存暂停状态并移除调度项。

<a id="api-237"></a>

### POST /api/taskCron/resume

- URL：`{baseUrl}/api/taskCron/resume`。
- 用途：恢复定时任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无JSON body。
- 参数：query.id：integer必填，数据库主键（不是entry_id）。
- 返回：HTTP200，application/json；`{msg:"任务已恢复",id:integer}`，id为新的调度器EntryID。
- 错误：400缺id/非法id/恢复失败；另见公共401鉴权错误。
- 副作用：恢复调度并更新EntryID。

<a id="api-238"></a>

### GET /api/taskCron/getTasks

- URL：`{baseUrl}/api/taskCron/getTasks`。
- 用途：列出可调度任务和参数说明。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200 application/json，直接 `TaskCron.TaskCronParamMeta[]`，按name排序。
- 错误：无专用业务错误；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-239"></a>

### GET /api/taskCron/scriptQueues

- URL：`{baseUrl}/api/taskCron/scriptQueues`。
- 用途：列出可定时执行的持久化脚本队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200 application/json，直接 `ScriptManagement.QueueRecord[]`。
- 错误：500 `{code:0,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-240"></a>

### POST /api/taskCron/update

- URL：`{baseUrl}/api/taskCron/update`。
- 用途：修改已启动任务的表达式/参数。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer必填数据库ID；entry_id：integer必填当前调度器ID；status：integer必填且非0；spec：string必填；data/mark：string可选；name为任务参数校验备用值，已有调度记录名称优先。
- 返回：HTTP200，application/json；`{msg:"任务已更新",id:integer}`，id是新EntryID。
- 错误：400文本 参数错误/任务已暂停/缺spec/任务参数错误；500文本 更新失败；另见公共401鉴权错误。
- 副作用：保存任务并替换调度项，EntryID会改变。

<a id="api-241"></a>

### POST /api/taskCron/AtOnceRun

- URL：`{baseUrl}/api/taskCron/AtOnceRun`。
- 用途：立即执行注册任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无JSON body。
- 参数：query.type：string必填，来自getTasks的name（不是typeName）；data：string可选默认空，复杂参数先序列化再URL编码。
- 返回：一般HTTP200 text/plain `任务已执行`；type为Maa Pipeline或启动会话程序时HTTP202 text/plain `<任务名> 已开始执行，请在运行日志中查看结果`。
- 错误：400文本 缺少参数typeName/未找到任务类型；任务内部失败可能只写日志；另见公共401鉴权错误。
- 副作用：实际执行可启动/关闭程序、重启或关机，HTTP成功不保证任务业务成功。

<a id="section-15"></a>

## BetterGI 与 ABGI 更新

源码：`bgiStatus/uploadBgi.go`、`menu/aBgiUpdateService.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-242"></a>

### POST /api/UpdateBgi/Upload

- URL：`{baseUrl}/api/UpdateBgi/Upload`。
- 用途：上传压缩包执行BetterGI更新。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：multipart/form-data。
- 参数：form.file：binary必填，BGI更新包，保存为BetterGI.7z；处理器无独立字段级大小上限。
- 返回：HTTP200，application/json；`{message:"更新成功",filename:string,size:integer}`，size为上传字节数。
- 错误：400 `{error:string}` 缺文件；400 `{message:string,filename:string,size:integer}` 更新失败；500 `{error:string}` 路径错误；另见公共401鉴权错误。
- 副作用：校验解压并备份User后关闭、替换和重启BetterGI；成功后后台更新仓库。

<a id="api-243"></a>

### POST /api/UpdateBgi/Download

- URL：`{baseUrl}/api/UpdateBgi/Download`。
- 用途：启动或复用在线BGI更新任务。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{message:string}`，下载任务已启动/已在进行中/当前已最新。
- 错误：429 `{message:string}` 调用频繁；500 `{error:string}` 版本查询失败；实际节流5分钟，旧提示仍称2分钟；另见公共401鉴权错误。
- 副作用：后台下载、校验、备份并替换BetterGI，可能关闭/重启程序。

<a id="api-244"></a>

### GET /api/UpdateBgi/Download

- URL：`{baseUrl}/api/UpdateBgi/Download`。
- 用途：订阅BGI更新进度，必要时也会发起更新。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200 text/event-stream，每秒progress事件data为下载状态；终态done事件data为success或error事件data为错误文本；无需更新时HTTP200 application/json `{message:string}`。
- 错误：429 JSON `{message:string}`；500 JSON `{error:string}`；流内失败为error事件；节流实际5分钟；另见公共401鉴权错误。
- 副作用：不是纯查询，无活动任务时尝试发起更新，不可用于无副作用探活。

<a id="api-245"></a>

### GET /api/UpdateBgi/DownloadStatus

- URL：`{baseUrl}/api/UpdateBgi/DownloadStatus`。
- 用途：查询在线更新状态快照。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；下载状态对象：status/percent/current/total/error/phase/message，详见结构附录。
- 错误：无专用业务错误；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-246"></a>

### POST /api/aBgiUpdate/GetLastVersion

- URL：`{baseUrl}/api/aBgiUpdate/GetLastVersion`。
- 用途：查询ABGI最新版本。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{version:string,msg:"获取版本成功"}`。
- 错误：502 `{version:string,msg:string}`，version回退当前版本；另见公共401鉴权错误。
- 副作用：访问发布服务，不安装更新。

<a id="api-247"></a>

### POST /api/aBgiUpdate/Update

- URL：`{baseUrl}/api/aBgiUpdate/Update`。
- 用途：更新ABGI并安排重启。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP202 application/json；`{success:true,restartScheduled:true,updateId:string,instanceId:string,targetVersion:string,msg:string}`；instanceId为旧实例ID。
- 错误：409 `{success:false,msg:string}` 已有更新/重启；500 `{success:false,msg:string,restartScheduled?:false,targetVersion?:string}`，可能已写入但未安排重启；另见公共401鉴权错误。
- 副作用：替换程序，响应后安排进程退出，由助手重启；不要自动重试。

<a id="section-16"></a>

## 配置组及地图追踪

源码：`menu/routes_tasks.go`、`ScriptGroup/GroupDetail.go`、`ScriptGroup/Pathing.go`、`ScriptGroup/GroupSettingsSchema.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-248"></a>

### POST /api/scriptGroup/UpdatePathing

- URL：`{baseUrl}/api/scriptGroup/UpdatePathing`。
- 用途：替换地图追踪并更新配置组。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.name：string配置组；folderName：string追踪目录，业务必需但处理器无binding必填校验。
- 返回：HTTP200，application/json；`{status:"success",data:"更新地图追踪成功"}`。
- 错误：400 `{message:"参数格式错误",error:string}`；500 `{status:"error",msg?:string,error?:string}`；另见公共401鉴权错误。
- 副作用：清空并复制本地AutoPathing目录，重写组文件并刷新更新配置。

<a id="api-249"></a>

### POST /api/scriptGroup/UpdatePathingNoConfig

- URL：`{baseUrl}/api/scriptGroup/UpdatePathingNoConfig`。
- 用途：只替换地图追踪目录。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.folderName：string业务必需；name：string可选且未使用；处理器无binding必填校验。
- 返回：HTTP200，application/json；`{status:"success",data:"更新地图追踪成功"}`。
- 错误：400 `{message:"参数格式错误",error:string}`；500 `{status:"error",msg?:string,error?:string}`；另见公共401鉴权错误。
- 副作用：清空并复制AutoPathing目录，不编辑组文件；部分复制错误仅写日志。

<a id="api-250"></a>

### POST /api/scriptGroup/cleanAllPathing

- URL：`{baseUrl}/api/scriptGroup/cleanAllPathing`。
- 用途：移除指定组中某目录的地图追踪项目。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.name、folderName：string业务必需，处理器无binding必填校验。
- 返回：HTTP200，application/json；`{status:"success",data:"清理成功"}`。
- 错误：400 `{message:"参数格式错误",error:string}`；500 `{status:"error",msg?:string,error?:string}`；另见公共401鉴权错误。
- 副作用：修改组文件、重排项目索引并刷新配置，不删除全部地图JSON。

<a id="api-251"></a>

### GET /api/scriptGroup/ConfigPathing

- URL：`{baseUrl}/api/scriptGroup/ConfigPathing`。
- 用途：重新扫描并返回地图更新绑定。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:config.UpdatePathing[]|null}`。
- 错误：500 `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：GET也会扫描并保存main.json中的UpdatePath。

<a id="api-252"></a>

### POST /api/scriptGroup/savePathing

- URL：`{baseUrl}/api/scriptGroup/savePathing`。
- 用途：保存地图追踪更新绑定。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体，根值为数组。
- 参数：body：config.UpdatePathing[]；每项name:string为配置组，folderName:string为追踪目录；不逐项显式必填校验。
- 返回：HTTP200，application/json；`{status:"success",message:"保存成功"}`。
- 错误：400 `{message:string,error:string}`；500 `{status:"error",msg:string}`；另见公共401鉴权错误。
- 副作用：修改main.json。

示例（占位值）：
```json
[{"name":"<配置组>","folderName":"<追踪目录>"}]
```

<a id="api-253"></a>

### GET /api/scriptGroup/listGroups

- URL：`{baseUrl}/api/scriptGroup/listGroups`。
- 用途：递归列出本地配置组名。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200 application/json，直接 `string[]|null`，包含子目录相对名，移除.json。
- 错误：500 `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-254"></a>

### POST /api/scriptGroup/createGroup

- URL：`{baseUrl}/api/scriptGroup/createGroup`。
- 用途：创建空白配置组。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.name：string必填，去空白非空，不能只含点/空格，禁止Windows路径特殊字符，不可重名。
- 返回：HTTP200，application/json；`{status:"success",msg:"创建成功",name:string}`。
- 错误：400 `{status:"error",msg:string}` 参数/重名；500同结构文件错误；另见公共401鉴权错误。
- 副作用：创建User/ScriptGroup/<名称>.json并设置索引与默认配置。

<a id="api-255"></a>

### GET /api/scriptGroup/localJsScripts

- URL：`{baseUrl}/api/scriptGroup/localJsScripts`。
- 用途：查询本地JS脚本，不更新仓库。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:ScriptGroup.LocalJsScriptInfo[]}`，Name/ChineseName/NowVersion为大写字段。
- 错误：目录读取失败返回200空数组；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-256"></a>

### GET /api/scriptGroup/localPathingFiles

- URL：`{baseUrl}/api/scriptGroup/localPathingFiles`。
- 用途：递归列出本地地图JSON。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:ScriptGroup.LocalPathingFileInfo[]}`。
- 错误：遍历失败通常跳过或200空数组；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-257"></a>

### GET /api/scriptGroup/listAllGroups

- URL：`{baseUrl}/api/scriptGroup/listAllGroups`。
- 用途：查询远端仓库地图目录树。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:ScriptGroup.AllPath[]}`，fileNameChild为递归子项。
- 错误：500 `{status:"error",message:string}`；另见公共401鉴权错误。
- 副作用：访问仓库目录服务，不改本地组。

<a id="api-258"></a>

### GET /api/scriptGroup/listPathingUpdatePaths

- URL：`{baseUrl}/api/scriptGroup/listPathingUpdatePaths`。
- 用途：重新扫描并保存地图更新目录绑定。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",data:"ok"}`。
- 错误：500 `{status:"error",error:string}`；另见公共401鉴权错误。
- 副作用：GET会保存main.json的UpdatePath。

<a id="api-259"></a>

### POST /api/scriptGroup/groupDetail

- URL：`{baseUrl}/api/scriptGroup/groupDetail`。
- 用途：读取项目、组设置、标签与并发版本。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.groupName：string必填，配置组相对名，不含.json，允许合法子目录，禁止越界。
- 返回：HTTP200，application/json；`{status:"success",projects:GroupProject[],settingsLabels:object<脚本目录,object<字段名,ScriptGroup.SettingFieldInfo>>,config:原生组设置对象,revision:string}`；项目及设置白名单详见附录。
- 错误：400名称/JSON无效；404不存在；500读取失败；JSON `{status:"error",msg:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-260"></a>

### GET /api/scriptGroup/configOptions

- URL：`{baseUrl}/api/scriptGroup/configOptions`。
- 用途：读取内置和本地战斗策略选项。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{status:"success",combatStrategies:string[]}`，包含内置项与无扩展名相对路径。
- 错误：500 `{status:"error",msg:string}`；根策略目录缺失不算错误；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-261"></a>

### POST /api/scriptGroup/saveProjectSettings

- URL：`{baseUrl}/api/scriptGroup/saveProjectSettings`。
- 用途：增量原子保存项目与组设置。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.groupName：string必填；projects：GroupProject[]可选，数值index标识，缺index跳过，未匹配index追加；config：object可选，仅允许附录白名单；revision：string，改非空config必填匹配读取版本；只改projects可省略，提交时仍须匹配。
- 返回：HTTP200，application/json；`{status:"success",msg:"保存成功",revision:string}`，新版本用于后续保存。
- 错误：400类型/白名单/关联校验错误；404不存在；409版本缺失或冲突；500文件错误；JSON `{status:"error",msg:string}`；另见公共401鉴权错误。
- 副作用：同一次文件事务合并项目和组配置，保留未提交和未知原始字段。

示例（占位值）：
```json
{"groupName":"<配置组>","revision":"<读取版本>","config":{"shellConfig":{"timeout":60}},"projects":[]}
```

<a id="section-17"></a>

## 配置组工作台

源码：`menu/ScriptGroupWorkspace.go`、`models/ScriptGroupWorkspaceModel.go`、`ScriptGroup/GroupSummary.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-262"></a>

### GET /api/scriptGroup/workspace

- URL：`{baseUrl}/api/scriptGroup/workspace`。
- 用途：查询当前用户工作台。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{code:200,data:{groups:ScriptGroup.GroupSummary[],categories:menu.scriptGroupCollectionDTO[],quickSets:menu.scriptGroupCollectionDTO[],settings:object<string,任意JSON>}}`；settings是动态用户设置字典。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-263"></a>

### PUT /api/scriptGroup/workspace/groupPreference

- URL：`{baseUrl}/api/scriptGroup/workspace/groupPreference`。
- 用途：保存单组收藏/排序。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.groupName：string必填且存在；favorite：boolean可选；sort：integer可选；二者至少一个，缺省保留。
- 返回：HTTP200，application/json；`{code:200,data:models.ScriptGroupPreference}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：保存当前用户偏好数据库。

<a id="api-264"></a>

### POST /api/scriptGroup/workspace/groupOrder

- URL：`{baseUrl}/api/scriptGroup/workspace/groupOrder`。
- 用途：保存组显示顺序。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.groupNames：string[]可选默认[]，去空去重，名称须存在，未提交组按原序追加。
- 返回：HTTP200，application/json；`{code:200,data:{groupNames:string[]}}`，完整最终顺序。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：修改用户sort，不改组文件。

<a id="api-265"></a>

### PUT /api/scriptGroup/workspace/groupCategories

- URL：`{baseUrl}/api/scriptGroup/workspace/groupCategories`。
- 用途：批量修改组分类。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.groupName:string或groupNames:string[]至少一组且须存在；categoryIds:integer[]可选默认[]，当前用户category集合ID，去0去重；mode:string默认replace，可replace/add/remove。
- 返回：HTTP200，application/json；`{code:200,data:{groups:{groupName:string,categoryIds:integer[]}[]}}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：修改分类成员，replace+[]清空，remove+[]不变。

<a id="api-266"></a>

### POST /api/scriptGroup/workspace/collections

- URL：`{baseUrl}/api/scriptGroup/workspace/collections`。
- 用途：创建分类/快捷组合。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.kind:string创建必填category/quick_set，更新缺省沿用；name:string创建必填非空，更新空沿用，同用户同类唯一；color:string可选#RRGGBB，category新增默认#d94c8c，quick_set强制空；members:string[]或{groupName:string}[]或null可选，去空去重，允许保存已删除组名，更新缺省保留、[]/null清空。
- 返回：HTTP200，application/json；`{code:200,data:menu.scriptGroupCollectionDTO}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：保存用户集合和成员，不运行。

<a id="api-267"></a>

### PUT /api/scriptGroup/workspace/collections/:id

- URL：`{baseUrl}/api/scriptGroup/workspace/collections/:id`。
- 用途：修改集合及完整成员序列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：path.id：integer必填>0且属于当前用户；body.kind:string创建必填category/quick_set，更新缺省沿用；name:string创建必填非空，更新空沿用，同用户同类唯一；color:string可选#RRGGBB，category新增默认#d94c8c，quick_set强制空；members:string[]或{groupName:string}[]或null可选，去空去重，允许保存已删除组名，更新缺省保留、[]/null清空。
- 返回：HTTP200，application/json；`{code:200,data:menu.scriptGroupCollectionDTO}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：更新集合，提交members时替换全部成员。

<a id="api-268"></a>

### DELETE /api/scriptGroup/workspace/collections/:id

- URL：`{baseUrl}/api/scriptGroup/workspace/collections/:id`。
- 用途：删除当前用户集合。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：path.id：integer必填>0且属于当前用户。
- 返回：HTTP200，application/json；`{code:200,data:{id:integer}}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：删除集合与成员，不删组文件。

<a id="api-269"></a>

### POST /api/scriptGroup/workspace/collections/reorder

- URL：`{baseUrl}/api/scriptGroup/workspace/collections/reorder`。
- 用途：修改同类集合顺序。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.kind：string必填category/quick_set；ids：integer[]默认[]，只允许当前用户该类集合，忽略0/重复，未提交项追加。
- 返回：HTTP200，application/json；`{code:200,data:{kind:string,ids:integer[]}}`，ids为最终顺序。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：保存集合sort。

<a id="api-270"></a>

### PUT /api/scriptGroup/workspace/settings

- URL：`{baseUrl}/api/scriptGroup/workspace/settings`。
- 用途：合并保存动态工作台设置。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body根值：object必填不可null；任意键覆盖同名值；sortMode若存在须string且为nameAsc/recentDesc/updatedDesc/projectCountDesc/custom；其他键接受任意JSON。
- 返回：HTTP200，application/json；`{code:200,data:object<string,任意JSON>}`，完整合并结果。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：合并数据库用户设置。

<a id="api-271"></a>

### POST /api/scriptGroup/workspace/migrate

- URL：`{baseUrl}/api/scriptGroup/workspace/migrate`。
- 用途：迁移旧快捷组合和启动时间。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.quickSets：{name:string,members:string[]}[]可选；lastStartTimes：object<组名,integer>可选，正Unix毫秒；空名/非法时间跳过，同名组合不覆盖，时间只取更新值。
- 返回：HTTP200，application/json；`{code:200,data:{createdQuickSets:string[],skippedQuickSets:string[],updatedRuntimeCount:integer}}`。
- 错误：400 参数无效；404 配置组/集合不存在；409 同类型集合重名；500 数据库/文件错误；JSON `{code:HTTP状态码,error:string}`；另见公共401鉴权错误。
- 副作用：新建用户快捷组合，更新共享组启动时间，不运行组。

<a id="section-18"></a>

## 本地脚本管理与工坊队列

源码：`ScriptManagement/http.go`、`ScriptManagement/service.go`、`models/ScriptEntryModel.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-272"></a>

### GET /api/scriptManagement/list

- URL：`{baseUrl}/api/scriptManagement/list`。
- 用途：查询全部脚本。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{code:1,msg:string,data:models.ScriptEntry[]}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读。

<a id="api-273"></a>

### GET /api/scriptManagement/detail

- URL：`{baseUrl}/api/scriptManagement/detail`。
- 用途：查询脚本详情。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数。
- 返回：HTTP200，application/json；`{code:1,msg:string,data:models.ScriptEntry}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读。

<a id="api-274"></a>

### POST /api/scriptManagement/create

- URL：`{baseUrl}/api/scriptManagement/create`。
- 用途：创建脚本记录。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer创建忽略/更新必填正数；nickname：string必填1..128字符，不能+开头或含控制字符，唯一；path：string必填存在的带盘符绝对本地普通文件，仅.bat/.vbs/.py/.exe，禁止UNC/设备路径/%变量%；arguments：string可选默认空，最多4096字符，Windows引号规则；successExitCodes：string可选，逗号分隔额外成功非零码最多16项；queueMode：string默认wait_exit，仅wait_exit/after_start。
- 返回：HTTP200，application/json；`{code:1,msg:string,data:models.ScriptEntry}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：写数据库，不执行。

<a id="api-275"></a>

### POST /api/scriptManagement/update

- URL：`{baseUrl}/api/scriptManagement/update`。
- 用途：完整更新脚本记录。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer创建忽略/更新必填正数；nickname：string必填1..128字符，不能+开头或含控制字符，唯一；path：string必填存在的带盘符绝对本地普通文件，仅.bat/.vbs/.py/.exe，禁止UNC/设备路径/%变量%；arguments：string可选默认空，最多4096字符，Windows引号规则；successExitCodes：string可选，逗号分隔额外成功非零码最多16项；queueMode：string默认wait_exit，仅wait_exit/after_start。
- 返回：HTTP200，application/json；`{code:1,msg:string,data:models.ScriptEntry}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：覆盖可编辑字段，不执行。

<a id="api-276"></a>

### DELETE /api/scriptManagement/delete

- URL：`{baseUrl}/api/scriptManagement/delete`。
- 用途：删除脚本记录。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数。
- 返回：HTTP200，application/json；`{code:1,msg:string}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：同一数据库事务中删除脚本记录及持久化队列中的该脚本引用，不删除磁盘文件。

<a id="api-277"></a>

### POST /api/scriptManagement/run

- URL：`{baseUrl}/api/scriptManagement/run`。
- 用途：后台启动单脚本。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer必填正数；arguments：string或string[]可选，缺省/null沿用默认参数，空串/[]为本次无参数覆盖，按Windows规则校验。
- 返回：HTTP200，application/json；`{code:1,msg:"脚本已启动",data:{id:integer,nickname:string}}`，只保证进程创建。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：启动本地程序，本次参数不写回记录。

示例（占位值）：
```json
{"id":1,"arguments":["<参数一>","<参数二>"]}
```

<a id="api-278"></a>

### POST /api/scriptManagement/queue

- URL：`{baseUrl}/api/scriptManagement/queue`。
- 用途：创建并启动一次性脚本队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.ids：integer[]，按顺序执行且不得重复，每项须存在；delaySeconds：integer默认0，0..86400；处理器未强制至少2项（该限制仅持久化队列）。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列已启动",data:ScriptManagement.QueueStatus}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：后台执行本地程序，仅允许一条queued/running队列。

<a id="api-279"></a>

### GET /api/scriptManagement/queue/status

- URL：`{baseUrl}/api/scriptManagement/queue/status`。
- 用途：查询运行队列状态。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：string必填非空，运行ID如script-queue-<序号>，不是数据库数字ID。
- 返回：HTTP200，application/json；`{code:1,msg:"查询成功",data:ScriptManagement.QueueStatus}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-280"></a>

### POST /api/scriptManagement/queue/abort

- URL：`{baseUrl}/api/scriptManagement/queue/abort`。
- 用途：请求中止运行队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：string必填非空，运行任务ID。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列中止请求已发送"}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：发送取消信号，需轮询终态。

<a id="api-281"></a>

### GET /api/scriptManagement/queues

- URL：`{baseUrl}/api/scriptManagement/queues`。
- 用途：查询持久化队列列表。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{code:1,msg:"查询成功",data:ScriptManagement.QueueRecord[]}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-282"></a>

### GET /api/scriptManagement/queue/detail

- URL：`{baseUrl}/api/scriptManagement/queue/detail`。
- 用途：查询持久化队列详情。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数，数据库队列ID。
- 返回：HTTP200，application/json；`{code:1,msg:"查询成功",data:ScriptManagement.QueueRecord}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-283"></a>

### POST /api/scriptManagement/queue/create

- URL：`{baseUrl}/api/scriptManagement/queue/create`。
- 用途：保存可复用队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer创建忽略/更新必填正数；name：string必填非空、最多128字符、不能控制字符且唯一；scriptIds：integer[]必填，至少2项，正数不得重复，每脚本文件和解释器须可用；delaySeconds：integer默认0，0..86400。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列已创建",data:ScriptManagement.QueueRecord}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：保存队列和脚本顺序，不运行。

<a id="api-284"></a>

### POST /api/scriptManagement/queue/update

- URL：`{baseUrl}/api/scriptManagement/queue/update`。
- 用途：更新可复用队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer创建忽略/更新必填正数；name：string必填非空、最多128字符、不能控制字符且唯一；scriptIds：integer[]必填，至少2项，正数不得重复，每脚本文件和解释器须可用；delaySeconds：integer默认0，0..86400。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列已更新",data:ScriptManagement.QueueRecord}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：替换名称、顺序、间隔，不运行。

<a id="api-285"></a>

### DELETE /api/scriptManagement/queue/delete

- URL：`{baseUrl}/api/scriptManagement/queue/delete`。
- 用途：删除持久化队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.id：integer必填正数，数据库ID。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列已删除"}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：删除队列和关联，不删脚本记录/文件。

<a id="api-286"></a>

### POST /api/scriptManagement/queue/run

- URL：`{baseUrl}/api/scriptManagement/queue/run`。
- 用途：启动持久化队列。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.id：integer必填正数，数据库队列ID。
- 返回：HTTP200，application/json；`{code:1,msg:"工坊队列已启动",data:ScriptManagement.QueueStatus}`。
- 错误：400 参数无效、404 记录不存在、409 名称或运行冲突、500 内部错误；JSON `{code:0,msg:string,error:string}`；另见公共401鉴权错误。
- 副作用：按快照后台执行；运行状态使用另一个字符串ID。

<a id="section-19"></a>

## 绝区零一条龙

源码：`ZenlessZoneZero-OneDragon/http.go`、`ZenlessZoneZero-OneDragon/types.go`、`ZenlessZoneZero-OneDragon/integration/abgi.go`。本组使用普通认证中间件：请求头 `apiKey: <ABGI_API_KEY>`，或原始JWT认证；详见总则。

<a id="api-287"></a>

### GET /api/zzz-one-dragon/config

- URL：`{baseUrl}/api/zzz-one-dragon/config`。
- 用途：读取独立ZZZ配置。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200 application/json，直接 `zzz.Config`。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-288"></a>

### POST /api/zzz-one-dragon/config

- URL：`{baseUrl}/api/zzz-one-dragon/config`。
- 用途：校验并保存独立ZZZ配置。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.installDir：string首次必需，实际安装目录不能中文/空格；launcher：string可选，空则优先OneDragon-RuntimeLauncher.exe其次OneDragon-Launcher.exe；instanceIds：integer[]可选，须属于官方实例，[]沿用官方选择；closeGame/notify：boolean可选；未提交字段保留原值。
- 返回：HTTP200 application/json，直接 `zzz.Config`。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：保存ABGI配置，运行中不允许修改，不改官方YAML。

<a id="api-289"></a>

### POST /api/zzz-one-dragon/detect

- URL：`{baseUrl}/api/zzz-one-dragon/detect`。
- 用途：只读检测待保存安装。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.installDir：string必填，存在的官方目录且不能中文/空格；launcher：string可选，仅两种官方启动器名。
- 返回：HTTP200 application/json，直接 `zzz.Detection`。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：仅读文件，不保存或启动程序。

<a id="api-290"></a>

### GET /api/zzz-one-dragon/instances

- URL：`{baseUrl}/api/zzz-one-dragon/instances`。
- 用途：读取已配置目录官方实例。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{instances:zzz.Instance[]}`。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-291"></a>

### POST /api/zzz-one-dragon/start

- URL：`{baseUrl}/api/zzz-one-dragon/start`。
- 用途：异步启动官方一条龙。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json，可空请求体或null。
- 参数：body.instanceIds：integer[]可选，[]沿用官方选择；closeGame：boolean可选；空body/null沿用已保存默认RunOptions，提交对象时使用对象自身布尔零值。
- 返回：HTTP202 application/json；`{run:zzz.RunRecord}`，为starting/running受理结果，不代表执行完成。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：创建受管进程并保存状态，后台跟踪日志和通知。

示例（占位值）：
```json
{"instanceIds":[1],"closeGame":false}
```

<a id="api-292"></a>

### POST /api/zzz-one-dragon/stop

- URL：`{baseUrl}/api/zzz-one-dragon/stop`。
- 用途：停止指定受管运行。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：application/json 请求体。
- 参数：body.runId：string必填非空，当前运行ID，禁止过期ID。
- 返回：HTTP200，application/json；`{run:zzz.RunRecord}`，可能stopping，重复停止同一终态可幂等返回。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：核对进程身份后请求停止，需轮询确认退出。

<a id="api-293"></a>

### GET /api/zzz-one-dragon/status

- URL：`{baseUrl}/api/zzz-one-dragon/status`。
- 用途：查询最近运行和存储状态。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：无业务参数，不需要请求体。
- 返回：HTTP200，application/json；`{configured:boolean,busy:boolean,run:zzz.RunRecord|null,storageError:string}`；busy为starting/running/stopping，无记录run=null。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-294"></a>

### GET /api/zzz-one-dragon/logs

- URL：`{baseUrl}/api/zzz-one-dragon/logs`。
- 用途：游标查询指定运行日志。
- 认证：请求头 `apiKey`，值为 `abgiUser.yaml` 中的 `auth.api_key`；鉴权失败 HTTP401 JSON。
- 请求格式：URL query，无请求体。
- 参数：query.runId：string必填当前ID；cursor：unsigned integer可选默认0，不能负数/非法数字。
- 返回：HTTP200，application/json；`{runId:string,entries:zzz.LogEntry[],cursor:unsigned integer,reset:boolean}`；最多500条，cursor到实际交付末条，reset表示超出保留窗口。
- 错误：400 参数/安装环境错误；409 运行冲突、过期runId或不能执行；JSON `{error:string}`。写请求最多64KiB，只允许单个JSON值，拒绝未知字段；另见公共401鉴权错误。
- 副作用：只读，无业务写入。

<a id="api-295"></a>

### ANY /api/zzz-one-dragon/*path

- URL：`{baseUrl}/api/zzz-one-dragon/*path`。
- 用途：仅ZZZ模块初始化失败时注册的兜底，不与正常8个接口同时存在，不计额外正常业务接口。
- 认证：请求头 `apiKey`。
- 请求格式：任意HTTP方法。
- 参数：path.path：string通配剩余路径；请求体不读取。
- 返回：HTTP503 application/json，`{error:"ZZZ 模块初始化失败，请查看 ABGI 日志"}`。
- 错误：401鉴权失败；503模块不可用。
- 副作用：无。

<a id="section-20"></a>

## 任务模块数据结构附录

字段名均为实际JSON名，非Go内部名；可省略对应omitempty，date-time为RFC3339。旧处理器的未初始化数组可能为null。MysSignIn状态：queued/running/success/partial/captcha/failed/interrupted；ScriptManagement队列：queued/running/completed/failed/aborted。ZZZ普通运行接口裁掉内部logs/logOffsets/logCheckpoints，不作为公开返回字段。

#### 数据结构：bgiStatus.OneDragonGroupProgress

源码：`bgiStatus/OneDragonGroupProgress.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `configName` | string | 一条龙名 |
| `runFound` | boolean | 找到运行区间 |
| `runCompleted` | boolean | 整轮完成标记 |
| `logFile` | string | 日志文件名 |
| `groups` | bgiStatus.OneDragonGroupProgressItem[] | 配置组进度 |

#### 数据结构：bgiStatus.OneDragonGroupProgressItem

源码：`bgiStatus/OneDragonGroupProgress.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `completed` | boolean | 完成标记或数量（见类型） |

#### 数据结构：GachaLog.SyncResult

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `uid` | string | 游戏UID |
| `region` | string | 区服 |
| `added` | integer | 新增数量 |
| `total` | integer | 总数 |
| `partial` | boolean | 部分成功标记 |
| `pools` | GachaLog.PoolSyncResult[] | 分卡池结果 |
| `synced_at` | string(date-time) | 同步时间 |

#### 数据结构：GachaLog.PoolSyncResult

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `pool` | integer | 卡池编号 |
| `pool_name` | string | 卡池名 |
| `added` | integer | 新增数量 |
| `success` | boolean | 成功标记 |
| `error_code`（可省略） | string | 错误标识 |
| `message`（可省略） | string | 提示 |

#### 数据结构：GachaLog.AccountDTO

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `uid` | string | 游戏UID |
| `region` | string | 区服 |
| `first_synced_at` | string(date-time) | 首次同步时间 |
| `last_synced_at` | string(date-time) | 最后同步时间 |
| `total` | integer | 总数 |

#### 数据结构：GachaLog.Summary

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `uid` | string | 游戏UID |
| `pool` | integer | 卡池编号 |
| `pool_name` | string | 卡池名 |
| `total` | integer | 总数 |
| `five_count` | integer | 五星数量 |
| `four_count` | integer | 四星数量 |
| `three_count` | integer | 三星数量 |
| `current_pity` | integer 或 null | 当前垫抽，未知为null |
| `average_five_interval` | number | 五星平均间隔抽数 |
| `first_time` | string 或 null | 首条祈愿时间 |
| `last_time` | string 或 null | 末条祈愿时间 |
| `five_stars` | GachaLog.FiveStarEntry[] | 五星列表 |
| `pools` | GachaLog.PoolSummary[] | 分卡池结果 |

#### 数据结构：GachaLog.FiveStarEntry

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `gacha_id` | string | 祈愿字符串ID |
| `name` | string | 名称 |
| `item_type` | string | 物品类型 |
| `rank_type` | integer | 星级 |
| `gacha_type` | integer | 原始祈愿类型 |
| `pool` | integer | 卡池编号 |
| `pool_name` | string | 卡池名 |
| `time` | string | 时间 |
| `pulls_since_last` | integer | 距上个五星抽数 |

#### 数据结构：GachaLog.PoolSummary

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `pool` | integer | 卡池编号 |
| `pool_name` | string | 卡池名 |
| `total` | integer | 总数 |
| `five_count` | integer | 五星数量 |
| `four_count` | integer | 四星数量 |
| `three_count` | integer | 三星数量 |
| `current_pity` | integer 或 null | 当前垫抽，未知为null |
| `average_five_interval` | number | 五星平均间隔抽数 |
| `last_time` | string 或 null | 末条祈愿时间 |

#### 数据结构：GachaLog.RecordPage

源码：`GachaLog/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `list` | models.GachaRecord[] | 记录列表 |
| `total` | integer | 总数 |
| `page` | integer | 当前页 |
| `page_size` | integer | 每页条数 |

#### 数据结构：models.GachaRecord

源码：`models/GachaLogModel.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `uid` | string | 游戏UID |
| `gacha_id` | string | 祈愿字符串ID |
| `gacha_type` | integer | 原始祈愿类型 |
| `item_id` | string | 物品ID |
| `name` | string | 名称 |
| `item_type` | string | 物品类型 |
| `rank_type` | integer | 星级 |
| `count` | integer | 数量 |
| `time` | string | 祈愿时间文本 |
| `lang` | string | 语言 |
| `created_at` | string(date-time) | 创建时间 |

#### 数据结构：Mihoyo.CharacterInfoResult

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `role` | Mihoyo.GameRole | 游戏账号角色 |
| `avatars` | Mihoyo.GenshinAvatar[] | 角色详情列表 |
| `raw_data`（可省略） | 任意有效JSON（上游原文） | 上游原始有效JSON，随上游变化 |
| `updated_at`（可省略） | string(date-time) | 更新时间 |

#### 数据结构：Mihoyo.GameRole

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `game_uid` | string | 游戏UID |
| `region` | string | 区服 |
| `game_role_id` | string | 角色ID |
| `game_biz` | string | 游戏业务 |
| `nickname` | string | 昵称 |
| `level` | integer | 等级 |
| `is_chosen` | boolean | 是否选择 |
| `region_name` | string | 区服名 |

#### 数据结构：Mihoyo.GenshinAvatar

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `name` | string | 名称 |
| `element` | string | 角色元素 |
| `image` | string | 角色立绘或头像地址 |
| `rarity` | integer | 稀有度/星级 |
| `level` | integer | 等级 |
| `actived_constellation_num` | integer | 已激活命座数 |
| `stats`（可省略） | object<string,number> | 内部属性键到数值的映射 |
| `panelStats`（可省略） | Mihoyo.GenshinPanelStat[] | Panel内部属性键到数值的映射 |
| `skills`（可省略） | Mihoyo.GenshinAvatarSkill[] | 技能名称与等级列表 |
| `constellations`（可省略） | Mihoyo.GenshinAvatarConstellation[] | 保存命座效果供策略分析。 |
| `weapon`（可省略） | Mihoyo.GenshinWeapon | 武器详情 |
| `artisMark`（可省略） | Mihoyo.ArtifactScoreResult 或 null | 圣遗物评分摘要 |
| `dmgCalc`（可省略） | Mihoyo.GenshinDmgCalc 或 null | 伤害计算展示结果 |
| `build`（可省略） | Mihoyo.GenshinAvatarBuild 或 null | 规范化角色配装 |

#### 数据结构：Mihoyo.GenshinPanelStat

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `key` | string | 内部属性键 |
| `name` | string | 名称 |
| `value` | string | 格式化属性值 |
| `base`（可省略） | string | 基础值显示文本 |
| `add`（可省略） | string | 加成值显示文本 |
| `rawValue`（可省略） | number | Raw格式化属性值 |
| `rawBase`（可省略） | number | Raw基础值显示文本 |
| `rawAdd`（可省略） | number | Raw加成值显示文本 |
| `isPercent` | boolean | 是否百分比属性 |

#### 数据结构：Mihoyo.GenshinAvatarSkill

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 保存技能名称。 |
| `level` | integer | 保存技能等级。 |
| `desc`（可省略） | string | 保存技能说明。 |
| `enhanced_desc`（可省略） | string | 保存强化技能说明。 |
| `skill_affix_list`（可省略） | object<string,任意JSON>[] | 保存技能附加说明原始条目。 |

#### 数据结构：Mihoyo.GenshinAvatarConstellation

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `pos` | integer | 保存命座位置。 |
| `name` | string | 保存命座名称。 |
| `effect` | string | 保存命座效果。 |
| `enhanced_effect`（可省略） | string | 保存强化命座效果。 |
| `is_actived` | boolean | 保存当前是否已激活。 |

#### 数据结构：Mihoyo.GenshinWeapon

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `name` | string | 名称 |
| `level` | integer | 等级 |
| `promote_level` | integer | 武器突破等级 |
| `affix_level` | integer | 武器精炼等级 |
| `rarity`（可省略） | integer | 稀有度/星级 |
| `icon`（可省略） | string | 图标地址 |
| `type_name`（可省略） | string | 武器类型名称 |
| `desc`（可省略） | string | 保存武器说明。 |
| `wiki_url`（可省略） | string | 百科链接 |
| `mainStat`（可省略） | Mihoyo.GenshinWeaponStat 或 null | 保存喵喵展开后的基础攻击力。 |
| `subStat`（可省略） | Mihoyo.GenshinWeaponStat 或 null | 保存喵喵展开后的副属性。 |
| `passiveTitle`（可省略） | string | 保存当前武器被动名称。 |
| `passiveEffect`（可省略） | string | 保存当前精炼等级被动文本。 |
| `passiveDescriptionAvailable` | boolean | 标记被动文本是否真实可用。 |

#### 数据结构：Mihoyo.GenshinWeaponStat

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `key`（可省略） | string | 保存内部属性键。 |
| `name`（可省略） | string | 保存属性显示名称。 |
| `value`（可省略） | string | 保存格式化后的属性值。 |
| `rawValue`（可省略） | number | 保存未带单位的属性数值。 |
| `isPercent` | boolean | 标记属性是否按百分比显示。 |

#### 数据结构：Mihoyo.ArtifactScoreResult

源码：`Mihoyo/artifact_score.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `mark` | string | 备注或评分显示值 |
| `_mark` | number | 评分原始数值 |
| `markClass` | string | 评分等级样式标识 |
| `classTitle`（可省略） | string | 评分等级标题 |
| `sets`（可省略） | string[] | 套装名称列表 |
| `names`（可省略） | string[] | 圣遗物名称列表 |
| `imgs`（可省略） | string[] | 圣遗物图片列表 |
| `charWeight`（可省略） | object<string,number> | 角色属性评分权重字典 |
| `artis`（可省略） | Mihoyo.ArtifactPieceScoreResult[] | 逐件圣遗物评分 |

#### 数据结构：Mihoyo.ArtifactPieceScoreResult

源码：`Mihoyo/artifact_score.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name`（可省略） | string | 名称 |
| `set`（可省略） | string | 圣遗物套装名 |
| `level`（可省略） | integer | 等级 |
| `mark` | string | 备注或评分显示值 |
| `_mark` | number | 评分原始数值 |
| `markClass` | string | 评分等级样式标识 |
| `main` | Mihoyo.ArtifactAttributeScore | 主属性评分 |
| `attrs`（可省略） | Mihoyo.ArtifactAttributeScore[] | 副属性评分列表 |
| `img`（可省略） | string | 图片地址 |

#### 数据结构：Mihoyo.ArtifactAttributeScore

源码：`Mihoyo/artifact_score.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `key`（可省略） | string | 内部属性键 |
| `value`（可省略） | string | 格式化属性值 |
| `mark`（可省略） | string | 备注或评分显示值 |
| `_mark`（可省略） | number | 评分原始数值 |

#### 数据结构：Mihoyo.GenshinDmgCalc

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `rows`（可省略） | Mihoyo.GenshinDmgCalcRow[] | 伤害展示行 |
| `enemyLevel` | integer | 敌人等级 |
| `enemyName`（可省略） | string | 敌人名称 |
| `createdBy`（可省略） | string | 伤害结果来源 |
| `message`（可省略） | string | 提示 |

#### 数据结构：Mihoyo.GenshinDmgCalcRow

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `title` | string | 展示标题 |
| `dmg`（可省略） | string | 伤害显示文本 |
| `avg` | string | 平均伤害显示文本 |
| `unit`（可省略） | string | 显示单位 |

#### 数据结构：Mihoyo.GenshinAvatarBuild

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `aeq`（可省略） | string | 普攻/战技/爆发等级展示文本 |
| `level`（可省略） | integer | 等级 |
| `cons`（可省略） | integer | 命座数 |
| `fetter`（可省略） | integer | 好感度等级 |
| `talent` | Mihoyo.GenshinTalentBreakdown | 普攻/战技/爆发分级详情 |
| `weapon`（可省略） | Mihoyo.GenshinBuildWeapon 或 null | 武器详情 |
| `artisMark`（可省略） | Mihoyo.ArtifactBuildSummary 或 null | 圣遗物评分摘要 |
| `reliquaries`（可省略） | Mihoyo.GenshinReliquary[] | 圣遗物列表 |

#### 数据结构：Mihoyo.GenshinTalentBreakdown

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `a` | Mihoyo.GenshinTalentLevel | 普通攻击等级 |
| `e` | Mihoyo.GenshinTalentLevel | 元素战技等级 |
| `q` | Mihoyo.GenshinTalentLevel | 元素爆发等级 |

#### 数据结构：Mihoyo.GenshinTalentLevel

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `level` | integer | 等级 |
| `original` | integer | 原始基础等级 |

#### 数据结构：Mihoyo.GenshinBuildWeapon

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name`（可省略） | string | 名称 |
| `level`（可省略） | integer | 等级 |
| `affix`（可省略） | integer | 精炼等级 |
| `star`（可省略） | integer | 星级 |

#### 数据结构：Mihoyo.ArtifactBuildSummary

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `mark` | string | 备注或评分显示值 |
| `_mark` | number | 评分原始数值 |
| `markClass` | string | 评分等级样式标识 |

#### 数据结构：Mihoyo.GenshinReliquary

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id`（可省略） | integer | 编号 |
| `name`（可省略） | string | 名称 |
| `pos`（可省略） | integer | 圣遗物部位编号 |
| `rarity`（可省略） | integer | 稀有度/星级 |
| `level`（可省略） | integer | 等级 |
| `icon`（可省略） | string | 图标地址 |
| `set_name`（可省略） | string | 套装名称 |
| `main_prop_id`（可省略） | integer | 主属性ID |
| `append_prop_id_list`（可省略） | integer[] | 副属性ID列表 |
| `append_props`（可省略） | Mihoyo.GenshinReliquarySubStat[] | 副属性数值列表 |
| `set_affixes`（可省略） | Mihoyo.GenshinArtifactSetAffix[] | 保存套装效果。 |

#### 数据结构：Mihoyo.GenshinReliquarySubStat

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `prop_id`（可省略） | integer | 属性ID |
| `value`（可省略） | number | 格式化属性值 |

#### 数据结构：Mihoyo.GenshinArtifactSetAffix

源码：`Mihoyo/character_info.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `activation_number` | integer | 保存套装生效件数。 |
| `effect` | string | 保存套装效果文本。 |

#### 数据结构：MysSignIn.Account

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 编号 |
| `name` | string | 名称 |
| `uidMasked` | string | 脱敏UID |
| `cookieMasked` | string | 脱敏Cookie |
| `stokenMasked` | string | 脱敏SToken |
| `credentialStatus` | string | 凭据状态 |
| `enabled` | boolean | 启用标记 |
| `running` | boolean | 执行中标记 |
| `settings` | MysSignIn.AccountSettings | 签到设置 |
| `lastRun`（可省略） | MysSignIn.RunSummary 或 null | 最近运行摘要 |

#### 数据结构：MysSignIn.AccountSettings

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `community` | MysSignIn.CommunitySettings | 社区签到设置 |
| `games` | MysSignIn.GameSettings | 游戏签到设置 |

#### 数据结构：MysSignIn.CommunitySettings

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `enabled` | boolean | 启用标记 |
| `checkin` | boolean | 签到标记 |
| `forumIds` | integer[] | 讨论区ID，支持1/2/3/4/5/6/8 |
| `read` | boolean | 阅读标记 |
| `like` | boolean | 点赞标记 |
| `cancelLike` | boolean | 取消点赞 |
| `share` | boolean | 分享 |

#### 数据结构：MysSignIn.GameSettings

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `cnEnabled` | boolean | 启用国服签到 |
| `genshin` | boolean | 原神签到 |
| `honkai2` | boolean | 崩坏学园2签到 |
| `honkai3rd` | boolean | 崩坏3签到 |
| `tearsOfThemis` | boolean | 未定事件簿签到 |
| `honkaiStarRail` | boolean | 星铁签到 |
| `zenlessZoneZero` | boolean | 绝区零签到 |

#### 数据结构：MysSignIn.RunSummary

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `batchId` | string | 批次ID |
| `status` | string | 状态 |
| `summary` | string | 结果摘要 |
| `createdAt` | string(date-time) | 创建时间 |
| `startedAt`（可省略） | string(date-time) 或 null | 开始时间 |
| `finishedAt`（可省略） | string(date-time) 或 null | 结束时间 |

#### 数据结构：MysSignIn.BatchStatus

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 编号 |
| `trigger` | string | 触发来源 |
| `status` | string | 状态 |
| `accountCount` | integer | 总账号数 |
| `successCount` | integer | 成功账号数 |
| `failedCount` | integer | 失败账号数 |
| `summary` | string | 结果摘要 |
| `createdAt` | string(date-time) | 创建时间 |
| `startedAt`（可省略） | string(date-time) 或 null | 开始时间 |
| `finishedAt`（可省略） | string(date-time) 或 null | 结束时间 |
| `accounts` | MysSignIn.BatchAccountStatus[] | 账号状态列表 |

#### 数据结构：MysSignIn.BatchAccountStatus

源码：`MysSignIn/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `accountId` | string | 账号ID |
| `accountName` | string | 账号名称 |
| `status` | string | 状态 |
| `summary` | string | 结果摘要 |
| `startedAt`（可省略） | string(date-time) 或 null | 开始时间 |
| `finishedAt`（可省略） | string(date-time) 或 null | 结束时间 |

#### 数据结构：models.MysSignRunLog

源码：`models/MysSignInModel.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `batchId` | string | 批次ID |
| `accountId` | string | 账号ID |
| `accountName` | string | 账号名称 |
| `trigger` | string | 触发来源 |
| `status` | string | 状态 |
| `summary` | string | 结果摘要 |
| `output` | string | 脱敏输出日志 |
| `createdAt` | string(date-time) | 创建时间 |
| `startedAt`（可省略） | string(date-time) 或 null | 开始时间 |
| `finishedAt`（可省略） | string(date-time) 或 null | 结束时间 |

#### 数据结构：characterDevelopment.DefaultAutoFightCharacterOption

源码：`characterDevelopment/auto_fight.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `strategyCount` | integer | 候选策略数 |

#### 数据结构：characterDevelopment.DefaultAutoFightCandidate

源码：`characterDevelopment/auto_fight.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `index` | integer | 顺序/动作索引 |
| `actions` | string[] | 动作列表 |
| `content` | string | 内容文本 |

#### 数据结构：combatStrategy.JSONDocument

源码：`combatStrategy/universal_json.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Info` | combatStrategy.JSONInfo | 策略元信息 |
| `Actions` | combatStrategy.JSONAction[] | 动作列表 |

#### 数据结构：combatStrategy.JSONInfo

源码：`combatStrategy/universal_json.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Name` | string | 名称 |
| `Author` | string | 作者 |
| `Description`（可省略） | string | 保留策略说明，供编辑器和 BetterGI 文档使用。 |
| `Config` | object<string,任意JSON> | 原生策略自定义设置键值 |
| `PreActions` | string[] | 前置指令文本列表 |

#### 数据结构：combatStrategy.JSONAction

源码：`combatStrategy/universal_json.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Name` | string | 名称 |
| `Character` | string | 角色名 |
| `Action` | string | BGI指令文本 |
| `Condition` | combatStrategy.JSONCondition | 触发条件 |
| `Index` | integer | 顺序/动作索引 |
| `EnsureCast` | boolean | 确保施放 |
| `redArrowAim`（可省略） | boolean | 保留红箭头瞄准辅助字段。 |
| `MorePriorities` | combatStrategy.JSONMorePriority[] | 额外触发优先级 |

#### 数据结构：combatStrategy.JSONCondition

源码：`combatStrategy/universal_json.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Expression` | string | 条件表达式 |

#### 数据结构：combatStrategy.JSONMorePriority

源码：`combatStrategy/universal_json.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Expression` | string | 条件表达式 |
| `Priority` | integer | 优先级 |

#### 数据结构：combatStrategy.CombatCooldown

源码：`combatStrategy/cooldowns.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `skillCD` | number | 战技冷却秒 |
| `skillHoldCD`（可省略） | number | 重e cd |
| `burstCD` | number | q cd |

#### 数据结构：models.CharacterPlan

源码：`models/CharacterPlanModel.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `characterName` | string | 角色名，多名逗号保存 |
| `oneLong` | string | 绑定一条龙 |
| `rankedGroup` | string | 绑定养成组 |
| `defaultTalent` | string | 旧默认天赋配置字段 |
| `isRanked` | boolean | 启用突破材料 |
| `isTalent` | boolean | 启用天赋材料 |
| `createdAt` | string(date-time) | 创建时间 |
| `updatedAt` | string(date-time) | 更新时间 |

#### 数据结构：CharacterPlanService.characterOption

源码：`CharacterPlanService/Service.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `level` | integer | 等级 |
| `talents` | integer[] | 普攻/战技/爆发等级 |
| `development` | characterDevelopment.ScriptSettingsPreview | 养成材料预览 |
| `uid` | string | 游戏UID |
| `region` | string | 区服 |
| `updatedAt` | string(date-time) | 更新时间 |

#### 数据结构：characterDevelopment.ScriptSettingsPreview

源码：`characterDevelopment/materials.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `talentBookName` | string | 天赋书名 |
| `talentBookRequireCounts` | string | 天赋书需求文本 |
| `bossName` | string | 突破Boss名 |
| `bossRequireCounts` | string | Boss材料需求文本 |
| `needsRanked` | boolean | 需要突破材料 |
| `needsTalent` | boolean | 需要天赋材料 |

#### 数据结构：CharacterPlanService.CharacterInformation

源码：`CharacterPlanService/CharacterInformation.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `level` | integer | 等级 |
| `talents` | integer[] | 普攻/战技/爆发等级 |
| `materials` | CharacterPlanService.CharacterMaterial[] | 需求与缺口 |

#### 数据结构：CharacterPlanService.CharacterMaterial

源码：`CharacterPlanService/CharacterInformation.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `name` | string | 名称 |
| `icon`（可省略） | string | 图标地址 |
| `required` | integer | 需求数量 |
| `owned` | integer | 库存数量 |
| `missing` | integer | 缺少数量 |

#### 数据结构：models.TaskCron

源码：`models/models.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 数据库主键 ID |
| `entry_id` | integer | cron 调度器的 EntryID |
| `name` | string | 名称 |
| `spec` | string | Cron表达式 |
| `next` | string | 下次时间文本 |
| `data` | string | 任务参数 |
| `status` | integer | 1=运行中, 0=暂停（持久化在 DB） |
| `mark` | string | 备注 |

#### 数据结构：TaskCron.TaskCronParamMeta

源码：`TaskCron/TaskCron.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `param_required` | boolean | 参数必填标记 |
| `param_placeholder` | string | 占位提示 |
| `param_format` | string | 格式说明 |
| `param_example` | string | 参数示例 |
| `param_tip` | string | 参数帮助 |

#### 数据结构：config.UpdatePathing

源码：`config/Config.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `folderName` | string | 脚本/追踪目录名 |

#### 数据结构：ScriptGroup.LocalJsScriptInfo

源码：`ScriptGroup/LocalJsScript.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `Name` | string | 名称 |
| `ChineseName` | string | 中文名 |
| `NowVersion` | string | 本地版本 |

#### 数据结构：ScriptGroup.LocalPathingFileInfo

源码：`ScriptGroup/LocalPathing.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `folderName` | string | 脚本/追踪目录名 |
| `relativePath` | string | 相对路径 |

#### 数据结构：ScriptGroup.AllPath

源码：`ScriptGroup/ScriptGroup.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `fileName` | string | 目录/文件名 |
| `fileNameChild` | ScriptGroup.AllPath[] | 改成嵌套结构 |

#### 数据结构：ScriptGroup.SettingFieldInfo

源码：`ScriptGroup/GroupDetail.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `label` | string | 字段标签 |
| `type` | string | 字段类型 |
| `options`（可省略） | string[] | 选项/本次参数（见类型） |

#### 数据结构：ScriptGroup.GroupSummary

源码：`ScriptGroup/GroupSummary.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `projectCount` | integer | 项目数 |
| `enabledProjectCount` | integer | 启用项目数 |
| `disabledProjectCount` | integer | 禁用项目数 |
| `updatedAt` | string(date-time) | 更新时间 |
| `readError` | string | 文件读取错误，空为正常 |
| `favorite` | boolean | 是否收藏 |
| `sort` | integer | 排序值 |
| `categoryIds` | integer[] | 分类ID |
| `lastStartedAt` | string(date-time) 或 null | 最近启动时间，未启动null |

#### 数据结构：menu.scriptGroupCollectionDTO

源码：`menu/ScriptGroupWorkspace.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `kind` | string | category/quick_set |
| `name` | string | 名称 |
| `color` | string | 分类颜色 |
| `sort` | integer | 排序值 |
| `members` | menu.scriptGroupCollectionMemberDTO[] | 配置组成员 |
| `createdAt` | string(date-time) | 创建时间 |
| `updatedAt` | string(date-time) | 更新时间 |

#### 数据结构：menu.scriptGroupCollectionMemberDTO

源码：`menu/ScriptGroupWorkspace.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `groupName` | string | 配置组名 |
| `sort` | integer | 排序值 |
| `exists` | boolean | 组文件是否存在 |

#### 数据结构：models.ScriptGroupPreference

源码：`models/ScriptGroupWorkspaceModel.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 编号 |
| `userKey` | string | 认证用户标识 |
| `groupName` | string | 配置组名 |
| `favorite` | boolean | 是否收藏 |
| `sort` | integer | 排序值 |
| `createdAt` | string(date-time) | 创建时间 |
| `updatedAt` | string(date-time) | 更新时间 |

#### 数据结构：models.ScriptEntry

源码：`models/ScriptEntryModel.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 保存数据库主键。 |
| `nickname` | string | 保存机器人使用的唯一昵称。 |
| `path` | string | 保存脚本绝对路径。 |
| `arguments` | string | 保存启动时使用的命令行参数文本。 |
| `successExitCodes` | string | 保存额外视为成功的非零退出码。 |
| `queueMode` | string | 保存入队后等待退出或启动后继续的方式。 |
| `createdAt` | string(date-time) | 保存创建时间。 |
| `updatedAt` | string(date-time) | 保存更新时间。 |

#### 数据结构：ScriptManagement.QueueRecord

源码：`ScriptManagement/service.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 保存队列编号。 |
| `name` | string | 保存队列名称。 |
| `scripts` | models.ScriptEntry[] | 保存按执行顺序排列的脚本记录。 |
| `delaySeconds` | integer | 保存相邻脚本之间等待的秒数。 |
| `createdAt` | string(date-time) | 保存队列创建时间。 |
| `updatedAt` | string(date-time) | 保存队列更新时间。 |

#### 数据结构：ScriptManagement.QueueStatus

源码：`ScriptManagement/service.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 保存本次运行任务编号。 |
| `name` | string | 保存持久化队列名称。 |
| `status` | string | 保存队列状态：queued、running、completed、failed 或 aborted。 |
| `total` | integer | 保存队列中的脚本总数。 |
| `currentIndex` | integer | 保存当前脚本在队列中的顺序索引。 |
| `currentId` | integer | 保存当前运行脚本编号。 |
| `currentNickname` | string | 保存当前运行脚本昵称。 |
| `lastCompletedNickname` | string | 保存最近完成脚本昵称，便于队列结束后仍可展示。 |
| `completed` | integer | 保存已经完成的脚本数量。 |
| `delaySeconds` | integer | 保存本次运行使用的相邻脚本间隔秒数。 |
| `nextStartAt`（可省略） | string(date-time) 或 null | 等待间隔时保存下一脚本最早启动时间。 |
| `error` | string | 保存失败或中止时的错误信息。 |
| `startedAt` | string(date-time) | 保存队列开始运行时间。 |
| `finishedAt`（可省略） | string(date-time) 或 null | 保存队列结束时间。 |

#### 数据结构：zzz.Config

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `installDir` | string | 官方程序安装目录。 |
| `launcher` | string | 启动器文件名，空值表示自动选择。 |
| `instanceIds` | integer[] | 默认指定的账号实例。 |
| `closeGame` | boolean | 默认关闭游戏选项。 |
| `notify` | boolean | 是否使用现有通知渠道。 |

#### 数据结构：zzz.RunOptions

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `instanceIds` | integer[] | 空列表表示沿用官方账号配置。 |
| `closeGame` | boolean | 完成流程后通过官方参数关闭游戏。 |

#### 数据结构：zzz.Instance

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 官方实例编号。 |
| `name` | string | 官方实例名称。 |
| `active` | boolean | 是否为当前实例。 |
| `activeInOd` | boolean | 是否参与默认一条龙。 |

#### 数据结构：zzz.Detection

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `installDir` | string | 规范化后的安装目录。 |
| `launcher` | string | 当前选中的启动器文件名。 |
| `launchers` | string[] | 实际存在的官方启动器。 |
| `instances` | zzz.Instance[] | 安装目录中配置的账号实例。 |

#### 数据结构：zzz.RunRecord

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 关联启动、停止、日志和通知的运行编号。 |
| `status` | string | 当前运行阶段或最终状态。 |
| `source` | string | 手动或定时任务触发来源。 |
| `startedAt` | string(date-time) | 本轮运行开始时间。 |
| `endedAt`（可省略） | string(date-time) 或 null | 本轮运行结束时间。 |
| `installDir` | string | 本轮固定使用的安装目录。 |
| `launcher` | string | 本轮固定使用的启动器。 |
| `options` | zzz.RunOptions | 本轮启动参数快照。 |
| `process` | zzz.ProcessIdentity | 核对和恢复受管进程所需的身份。 |
| `exitCode`（可省略） | integer 或 null | 已知的受管进程退出码。 |
| `completionSeen` | boolean | 是否看到本轮顶层完整结束信号。 |
| `failureSeen` | boolean | 是否看到本轮顶层失败信号。 |
| `stopRequested` | boolean | 是否已请求停止本轮运行。 |
| `message` | string | 面向用户展示的状态说明。 |
| `failedTasks` | zzz.TaskFailure[] | 本轮更新的失败任务。 |
| `notified` | boolean | 是否已登记本轮最终通知。 |
| `notify` | boolean | 本轮通知开关快照。 |
| `logCursor` | integer | 已分配的最后一个日志游标。 |

#### 数据结构：zzz.ProcessIdentity

源码：`ZenlessZoneZero-OneDragon/process_windows.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `pid` | integer | 保存系统分配的进程编号。 |
| `createdAt` | integer | 保存进程创建时间的毫秒时间戳。 |
| `executable` | string | 保存进程实际执行文件的完整路径。 |

#### 数据结构：zzz.TaskFailure

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `instanceId` | integer | 失败记录所属实例。 |
| `task` | string | 官方任务记录文件名。 |

#### 数据结构：zzz.LogEntry

源码：`ZenlessZoneZero-OneDragon/types.go`。

| JSON字段 | 类型 | 含义 |
| --- | --- | --- |
| `cursor` | integer | 单次运行内递增的日志序号。 |
| `time` | string(date-time) | 接收到该日志的时间。 |
| `source` | string | 日志来源。 |
| `text` | string | UTF-8 日志文本。 |

#### 数据结构：下载状态

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| status | string | idle/starting/downloading/updating/backup/validating/replacing/done/error等更新状态 |
| percent | string | 保留两位小数的百分比，不是number |
| current | integer | 已下载字节 |
| total | integer | 总字节，未知可为0 |
| error | string | 错误说明，正常空 |
| phase | string | 下载/校验/备份/替换阶段 |
| message | string | 阶段提示 |

#### 数据结构：GroupProject及原生组设置

GroupProject保留原始BGI字段值，响应中缺失的属性可能null：`name:string`项目名、`folderName:string`目录、`index:number`标识、`type:string`如Pathing/JavaScript、`status:string`如Enabled/Disabled、`schedule:string`周期、`runNum:number`次数、`allowJsNotification:boolean`通知、`jsScriptSettingsObject:object<string,任意JSON>`动态脚本设置。保存已有项目仅覆盖提交的基础键，脚本设置按键增量合并；动态键含义来自对应脚本settings.json，不能统一枚举。settingsLabels是目录→字段名→SettingFieldInfo字典。

详情config原样返回原生设置并保留未知键，但不代表任意键可写。保存config必须是嵌套对象，以下点路径列出全部可修改叶子，均可选、未提交保留；不要传扁平点路径键。默认值用于缺失关联字段兼容，不表示自动覆盖。

| config内叶子路径 | 类型 | 含义 | 缺失兼容默认 | 约束 |
| --- | --- | --- | --- | --- |
| `pathingConfig.enabled` | boolean | 启用地图追踪行走配置 | `true` | 原生类型 |
| `pathingConfig.autoPickEnabled` | boolean | 自动拾取 | `true` | 原生类型 |
| `pathingConfig.autoEatEnabled` | boolean | 自动吃药 | `false` | 原生类型 |
| `pathingConfig.partyName` | string | 切换到队伍名称 | `""` | 原生类型 |
| `pathingConfig.isVisitStatueBeforeSwitchParty` | boolean | 切换队伍前前往安全神像 | `false` | 原生类型 |
| `pathingConfig.guardianAvatarIndex` | string | 生存位角色编号 | `""` | 枚举 ""、"1"、"2"、"3"、"4" |
| `pathingConfig.guardianElementalSkillSecondInterval` | string | 生存位元素战技间隔 | `""` | 非负数字字符串；>=0 |
| `pathingConfig.guardianElementalSkillLongPress` | boolean | 生存位元素战技长按 | `false` | 原生类型 |
| `pathingConfig.mainAvatarIndex` | string | 主要行走角色编号 | `""` | 枚举 ""、"1"、"2"、"3"、"4" |
| `pathingConfig.useGadgetIntervalMs` | number | 使用小道具间隔 | `0` | 32位整数；>=0 |
| `pathingConfig.recoverTiming` | number | 低血量回复时机 | `0` | 32位整数；枚举 0、1、2 |
| `pathingConfig.skipDuring` | string | 不在某时执行 | `""` | 小时字符串0..23或空 |
| `pathingConfig.hideOnRepeat` | boolean | 不在连续任务中显示 | `false` | 原生类型 |
| `pathingConfig.hurryOnAvatar` | string | 赶路角色 | `""` | 枚举 ""、"自动"、"玛薇卡"、"闲云"、"桑多涅"、"恰斯卡"、"流浪者"、"伊法"、"希诺宁"、"法尔伽"、"夜兰" |
| `pathingConfig.hurryOnFrameInterval` | number | 赶路帧间隔 | `100` | 32位整数；>=1；<=150 |
| `pathingConfig.travelMode` | string | 赶路模式 | `"精准靠近"` | 枚举 "精准靠近"、"连续赶路" |
| `pathingConfig.distance` | number | 赶路临界距离 | `45` | 32位整数；>=0 |
| `pathingConfig.approachStopDistance` | number | 接近停车距离 | `25` | 32位整数；>=0 |
| `pathingConfig.switchToWalkEnabled` | boolean | 接近节点时切人步行 | `false` | 原生类型 |
| `pathingConfig.mwkJumpFlyEnabled` | boolean | 玛薇卡跳飞 | `true` | 原生类型 |
| `pathingConfig.mwkJumpFlyDistance` | number | 跳飞启用距离 | `75` | 32位整数；>=1 |
| `pathingConfig.mwkJumpFlyIntervalSeconds` | number | 跳飞间隔 | `1` | >=0 |
| `pathingConfig.mwkDisableSprintEnabled` | boolean | 玛薇卡车上禁用冲刺 | `false` | 原生类型 |
| `pathingConfig.mwkJumpFlySprintCount` | number | 跳飞前额外冲刺次数 | `0` | 32位整数；>=0 |
| `pathingConfig.taskCycleConfig.enable` | boolean | 启用执行周期 | `false` | 原生类型 |
| `pathingConfig.taskCycleConfig.boundaryTime` | number | 分界时间 | `0` | 32位整数；>=0；<=23 |
| `pathingConfig.taskCycleConfig.isBoundaryTimeBasedOnServerTime` | boolean | 分界时间参考服务器时区 | `false` | 原生类型 |
| `pathingConfig.taskCycleConfig.cycle` | number | 周期天数 | `1` | 32位整数；>=1 |
| `pathingConfig.taskCycleConfig.index` | number | 执行序号 | `1` | 32位整数；>=1 |
| `pathingConfig.taskCompletionSkipRuleConfig.enable` | boolean | 跳过已完成任务 | `false` | 原生类型 |
| `pathingConfig.taskCompletionSkipRuleConfig.skipPolicy` | string | 跳过策略 | `"GroupPhysicalPathSkipPolicy"` | 枚举 "GroupPhysicalPathSkipPolicy"、"PhysicalPathSkipPolicy"、"SameNameSkipPolicy" |
| `pathingConfig.taskCompletionSkipRuleConfig.boundaryTime` | number | 分界时间 | `4` | 32位整数 |
| `pathingConfig.taskCompletionSkipRuleConfig.isBoundaryTimeBasedOnServerTime` | boolean | 分界时间参考服务器时区 | `false` | 原生类型 |
| `pathingConfig.taskCompletionSkipRuleConfig.lastRunGapSeconds` | number | 上一次执行间隔 | `-1` | 32位整数 |
| `pathingConfig.taskCompletionSkipRuleConfig.referencePoint` | string | 间隔计算参考时间 | `"EndTime"` | 枚举 "StartTime"、"EndTime" |
| `pathingConfig.preExecutionPriorityConfig.enabled` | boolean | 启用优先执行 | `false` | 原生类型 |
| `pathingConfig.preExecutionPriorityConfig.groupNames` | string | 优先配置组名称 | `""` | 原生类型 |
| `pathingConfig.preExecutionPriorityConfig.maxRetryCount` | number | 失败重试次数 | `1` | 32位整数；>=0 |
| `pathingConfig.autoFightEnabled` | boolean | 启用自动战斗配置 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.strategyName` | string | 战斗策略 | `"根据队伍自动选择"` | 原生类型 |
| `pathingConfig.autoFightConfig.actionSchedulerByCd` | string | 按技能冷却优化出招 | `""` | 原生类型 |
| `pathingConfig.autoFightConfig.timeout` | number | 自动战斗超时 | `120` | 32位整数；>=0 |
| `pathingConfig.autoFightConfig.swimmingEnabled` | boolean | 战斗中检测游泳 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.fightFinishDetectEnabled` | boolean | 检测战斗结束 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.fastCheckEnabled` | boolean | 更快触发战斗结束检查 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.fastCheckParams` | string | 快速检查参数 | `""` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.checkAfterSwitchAvatar` | boolean | 切人后执行结束检查 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.skipFightEndCheckWhenEnemyVisible` | boolean | 敌人可见时跳过结束检查 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.blockCheckBeforeBattleSeconds` | number | 开战后阻断结束检查 | `0` | >=0；<=10 |
| `pathingConfig.autoFightConfig.finishDetectConfig.paimonEndCheckEnabled` | boolean | 派蒙辅助检测 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.paimonEndCheckDelay` | number | 派蒙检测延时 | `0.2` | >=0.05；<=0.4 |
| `pathingConfig.autoFightConfig.finishDetectConfig.rotateFindEnemyEnabled` | boolean | 旋转寻找敌人位置 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.rotaryFactor` | number | 旋转速度因子 | `12` | 32位整数；>=1；<=13 |
| `pathingConfig.autoFightConfig.finishDetectConfig.checkBeforeBurst` | boolean | 释放元素爆发前检查 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.isFirstCheck` | boolean | 开战时尝试面向敌人 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.checkEndDelay` | string | 检查战斗结束的延时 | `"0.4;钟离,1.4;"` | 原生类型 |
| `pathingConfig.autoFightConfig.finishDetectConfig.beforeDetectDelay` | string | 按键触发后检查延时 | `"0.4"` | 非负数字字符串；>=0 |
| `pathingConfig.autoFightConfig.enableCombatTargeting` | boolean | 战斗中持续索敌 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.lockLostWaitTime` | number | 脱锁等待时间 | `0.5` | >=0 |
| `pathingConfig.autoFightConfig.damageNumberRecognitionMode` | number | 伤害数字识别模式 | `2` | 32位整数；枚举 0、1、2 |
| `pathingConfig.autoFightConfig.targetingDetectionInterval` | number | 索敌识别间隔 | `50` | 32位整数；>=0 |
| `pathingConfig.autoFightConfig.drawRecognitionResults` | boolean | 绘制识别结果位置 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.guardianAvatar` | string | 盾奶位角色编号 | `""` | 枚举 ""、"1"、"2"、"3"、"4" |
| `pathingConfig.autoFightConfig.guardianCombatSkip` | boolean | 禁用该角色策略并自动释放元素战技 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.burstEnabled` | boolean | 自动释放元素爆发 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.guardianAvatarHold` | boolean | 盾奶位元素战技长按 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.pickDropsAfterFightEnabled` | boolean | 扫描掉落物光柱 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.pickDropsAfterFightSeconds` | number | 光柱扫描最大时长 | `15` | 32位整数；>=0 |
| `pathingConfig.autoFightConfig.kazuhaPickupEnabled` | boolean | 使用万叶或琴聚集材料 | `true` | 原生类型 |
| `pathingConfig.autoFightConfig.qinDoublePickUp` | boolean | 二次拾取 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.expBasedPickupEnabled` | boolean | 按精英怪经验值判断拾取 | `false` | 原生类型 |
| `pathingConfig.autoFightConfig.battleThresholdForLoot` | number | 拾取战斗人次阈值 | `null` | 32位整数；允许null |
| `pathingConfig.autoFightConfig.onlyPickEliteDropsMode` | string | 只拾取精英掉落模式 | `"Closed"` | 枚举 "Closed"、"AllowAutoPickupForNonElite"、"DisableAutoPickupForNonElite" |
| `pathingConfig.autoFightConfig.kazuhaPartyName` | string | 万叶拾取队伍名 | `""` | 原生类型 |
| `pathingConfig.autoEatConfig.defaultAtkBoostingDishName` | string | 默认攻击类料理 | `"炸萝卜丸子"` | 允许null |
| `pathingConfig.autoEatConfig.defaultAdventurersDishName` | string | 默认冒险类料理 | `null` | 允许null |
| `pathingConfig.autoEatConfig.defaultDefBoostingDishName` | string | 默认防御类料理 | `null` | 允许null |
| `enableShellConfig` | boolean | 覆盖 Shell 执行配置 | `false` | 原生类型 |
| `shellConfig.disable` | boolean | 禁用 Shell 任务 | `false` | 原生类型 |
| `shellConfig.timeout` | number | Shell 执行超时 | `60` | 32位整数 |
| `shellConfig.noWindow` | boolean | 隐藏命令执行窗口 | `true` | 原生类型 |
| `shellConfig.output` | boolean | 在日志中记录命令输出 | `true` | 原生类型 |

白名单之外的原生字段仅可读取。合并修改后对本次涉及的字段执行关联校验，错误由400返回：`taskCycleConfig.index <= taskCycleConfig.cycle`、`approachStopDistance <= distance`、`distance < mwkJumpFlyDistance`（这些字段均位于`pathingConfig`）；`pathingConfig.autoFightConfig.finishDetectConfig.rotateFindEnemyEnabled`与`skipFightEndCheckWhenEnemyVisible`不能同时为true。与本次改动无关的历史异常不阻止保存。

<a id="section-21"></a>

## 版本与更新说明

源码：`menu/routes_integrations.go`、`menu/aBgiUpdateService.go`。

<a id="api-296"></a>

### GET /api/aBgiUpdate/version

- 用途：查询当前 ABGI 版本及运行实例，用于确认重启是否完成
- URL：`{baseUrl}/api/aBgiUpdate/version`
- 认证：公开，无需密钥。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json；`version:string` 去空白后的版本，`pid:int` 进程号，`instanceId:string` 实例标识；Cache-Control: no-store。
- 错误：本处理器无显式错误分支。
- 副作用：无。

<a id="api-297"></a>

### GET /api/aBgiUpdate/GetBgiVersion

- 用途：查询当前与最新 BetterGI 版本
- URL：`{baseUrl}/api/aBgiUpdate/GetBgiVersion`
- 认证：公开，无需密钥。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json；`currentVersion:string` 当前版本，`lastVersion:string` 最新版本，`msg:string` 提示；公版额外 `canUpdate:bool` 是否有新版本。
- 错误：公版上游查询失败为 502 `{msg:"获取版本失败"}`；lcb 分支查询失败仍为 200 `{msg:"获取版本失败"}`。
- 副作用：会查询远端版本；不执行更新。

<a id="api-298"></a>

### GET /api/updateNotes

- 用途：读取内置更新说明
- URL：`{baseUrl}/api/updateNotes`
- 认证：公开，无需密钥。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:UpdateNote[]}`；UpdateNote 为 `{version:string,date:string,items:string[]}`，分别是版本、日期、说明条目。
- 错误：无显式错误分支。
- 副作用：无。


<a id="section-22"></a>

## CD 感知采集

源码：`menu/routes_integrations.go`、`CDAwareAutoGather/CDAwareAutoGather.go`。

本组结果保留 Go 默认字段大小写；读取失败可能是 HTTP 200 + null，并非统一错误包络。

<a id="api-299"></a>

### GET /api/CD-Aware-AutoGather/ReadInfo

- 用途：按账户读取采集路线 CD
- URL：`{baseUrl}/api/CD-Aware-AutoGather/ReadInfo`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `status:string` 可选；`1` 仅已到期、`2` 仅未到期、`3` 全部；未传或其他值时明细为空。
- 返回：200 application/json；`UidInfo[] | null`，见集成模型 UidInfo。
- 错误：鉴权失败为公共 401；文件异常记日志，可能返回 null。
- 副作用：无业务写入。

<a id="api-300"></a>

### POST /api/CD-Aware-AutoGather/CDAllMaterial

- 用途：提取 CD 采集材料并加入背包关注列表
- URL：`{baseUrl}/api/CD-Aware-AutoGather/CDAllMaterial`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json；去重材料名称 `string[] | null`。
- 错误：鉴权失败为公共 401；读取或保存异常只记日志，不能仅依赖 HTTP 判断持久化成功。
- 副作用：改写 BagStatistics 并保存配置。

<a id="api-301"></a>

### POST /api/CD-Aware-AutoGather/UpdateAllCD

- 用途：将本地脚本仓库的更新同步到 CD 管理对应路线目录
- URL：`{baseUrl}/api/CD-Aware-AutoGather/UpdateAllCD`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",message:"更新成功,具体请看日志:logs/"}`。
- 错误：鉴权失败为公共 401；内部更新失败以日志为准。
- 副作用：更新本地 CD 采集配置及相关文件。


<a id="section-23"></a>

## 采集管理与路线包

源码：`menu/routes_integrations.go`、`CDCollectionManagement/CDCollectionService.go`、`CDCollectionManagement/BlacklistService.go`。

本组依赖已安装的“采集cd管理”脚本。路径必须在该脚本的 pathing 根目录内，不接受任意文件系统目标。JSON 与返回字段区分大小写。

<a id="api-302"></a>

### GET /api/CDCollectionManagement/AllUserFile

- 用途：枚举采集记录账户
- URL：`{baseUrl}/api/CDCollectionManagement/AllUserFile`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json；`string[] | null` 账户目录名。
- 错误：鉴权失败为公共 401；目录读取失败记日志并返回 null。
- 副作用：无业务写入。

<a id="api-303"></a>

### GET /api/CDCollectionManagement/list

- 用途：读取账户路线树及运行记录
- URL：`{baseUrl}/api/CDCollectionManagement/list`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `name:string` 账户目录名；不传按空目录名读取，不自动选择账户。
- 返回：200 application/json；`CollectionFileNode | null`，记录字段为 FileName/CdTime/Status/History。
- 错误：鉴权失败为公共 401；记录读取失败可返回 null。
- 副作用：无业务写入。

<a id="api-304"></a>

### GET /api/CDCollectionManagement/ReadPickup

- 用途：读取账户拾取统计记录
- URL：`{baseUrl}/api/CDCollectionManagement/ReadPickup`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `name:string` 账户目录名；不传按空目录名读取。
- 返回：200 application/json；`PickupRecord[] | null`；Date 是记录日期，Item 为分类→材料→数量映射。
- 错误：鉴权失败为公共 401；文件失败记日志并返回 null。
- 副作用：无业务写入。

<a id="api-305"></a>

### GET /api/CDCollectionManagement/materialRoutes

- 用途：按材料分析命中路线和效率
- URL：`{baseUrl}/api/CDCollectionManagement/materialRoutes`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `name:string` 账户；`material:string` 可选查询词，空值返回空分析；`exact:bool` 可选默认 false，`latestOnly:bool` 可选默认 true，二者仅字符串 `true` 视为真。
- 返回：200 application/json；`MaterialRouteAnalysis`，含 query/exact/latestOnly/total/routes/summary。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`。
- 副作用：无业务写入。

<a id="api-306"></a>

### GET /api/CDCollectionManagement/backpackMaterial

- 用途：从材料文件夹名识别材料并查背包数量
- URL：`{baseUrl}/api/CDCollectionManagement/backpackMaterial`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `folderName:string` 必填，去首尾空白后不能为空。
- 返回：200 application/json；`BackpackMaterialResult`：folderName 原文件夹、materialName 识别材料、count 当前数量。
- 错误：鉴权失败为公共 401；400 invalid_folder_name；404 material_not_found；409 material_ambiguous；502 bag_unavailable；500 backpack_material_error；错误 `{code:string,message:string}`。
- 副作用：无业务写入。

<a id="api-307"></a>

### GET /api/CDCollectionManagement/cookingRecipes

- 用途：读取料理与食材目录
- URL：`{baseUrl}/api/CDCollectionManagement/cookingRecipes`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json；`CookingRecipeCatalog`，dataUpdated/sourceUrl/recipes。
- 错误：鉴权失败为公共 401；无其他显式错误分支。
- 副作用：无业务写入。

<a id="api-308"></a>

### POST /api/CDCollectionManagement/addCookingRoutes

- 用途：按料理份数计算食材并安装可采集材料路线
- URL：`{baseUrl}/api/CDCollectionManagement/addCookingRoutes`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `recipeName:string` 必填；`servings:int` 必填，1–999。
- 返回：200 application/json；`AddCookingPathingResult`，包括 recipe、servings、ingredients 与 addedCount/existingCount/skippedCount/failedCount；食材单项失败也在 200 内报告。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`。
- 副作用：下载并写入材料路线；商店或加工食材跳过。

<a id="api-309"></a>

### POST /api/CDCollectionManagement/createFolder

- 用途：在采集路线目录内创建文件夹
- URL：`{baseUrl}/api/CDCollectionManagement/createFolder`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `parentPath:string` 必填采集目录内已有父目录绝对路径；`folderName:string` 必填，禁止 .、.. 和 Windows 非法字符。
- 返回：200 application/json `{message:"创建成功",path:string}`，path 为创建路径。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`。
- 副作用：新建文件夹；已存在不覆盖。

<a id="api-310"></a>

### POST /api/CDCollectionManagement/createScript

- 用途：将远端地图追踪路线加入采集目录
- URL：`{baseUrl}/api/CDCollectionManagement/createScript`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `parentPath:string` 采集目录内目标父目录；`scriptPath:string` 必填，远端仓库相对路线位置。
- 返回：200 application/json `{message:"新增成功",path:string}`。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`。
- 副作用：下载并写入路线文件；不覆盖同名文件。

<a id="api-311"></a>

### POST /api/CDCollectionManagement/deleteNode

- 用途：删除采集目录中的文件或节点
- URL：`{baseUrl}/api/CDCollectionManagement/deleteNode`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `targetPath:string` 必填，采集目录内目标路径。
- 返回：200 application/json `{message:"删除成功",path:string}`。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`。
- 副作用：删除选中节点，可能包含文件夹及其内容；调用前应确认。

<a id="api-312"></a>

### POST /api/CDCollectionManagement/deleteRoutes

- 用途：批量删除路线并返回逐项结果
- URL：`{baseUrl}/api/CDCollectionManagement/deleteRoutes`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `paths:string[]` 必填，1–5000 条；2MB 请求上限；每项为采集目录内 .json 或 .json.不跑 普通文件的绝对路径。
- 返回：200 application/json；`RouteDeleteResult`：message、paths 成功路径、successCount、failedCount、failures?，失败项 {path,message}。
- 错误：鉴权失败为公共 401；400 invalid_request/empty_paths/too_many_paths；413 request_too_large；单条失败不改变 200。
- 副作用：永久删除成功项；不能删除文件夹或符号链接。

<a id="api-313"></a>

### POST /api/CDCollectionManagement/toggleRoutes

- 用途：批量禁用或恢复路线
- URL：`{baseUrl}/api/CDCollectionManagement/toggleRoutes`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `paths:string[]` 必填，1–5000 条、绝对路径；`disabled:bool` 可选默认 false；2MB 上限。
- 返回：200 application/json；`RouteToggleResult`：message、paths、successCount、failedCount、failures?。
- 错误：鉴权失败为公共 401；400 invalid_request/empty_paths/too_many_paths；413 request_too_large；单条失败在 failures 内。
- 副作用：通过添加或移除 `.不跑` 后缀重命名路线；不覆盖目标。

<a id="api-314"></a>

### POST /api/CDCollectionManagement/resetCooldown

- 用途：清除指定账户的选中路线冷却时间
- URL：`{baseUrl}/api/CDCollectionManagement/resetCooldown`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填，已存在合法账户名；`paths:string[]` 必填，1–5000 条采集目录内路线绝对路径；2MB 上限。
- 返回：200 application/json；`RouteCooldownResult`，成功、跳过及失败数量和路径列表。
- 错误：鉴权失败为公共 401；400 非法账户/请求/路径数；413 请求过大；503 脚本不可用；存储故障按 code/message 返回。
- 副作用：只将匹配记录的 CD 时间改为 1970-01-01T00:00:00.000Z；无记录的项跳过。

<a id="api-315"></a>

### POST /api/CDCollectionManagement/exportRoutes

- 用途：导出选中路线的标准 ZIP 包
- URL：`{baseUrl}/api/CDCollectionManagement/exportRoutes`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `basePath:string` 根目录内已有基准目录；`filePaths:string[]` 必填非空，选中路线；含 manifest 的条目数不超过 10000。
- 返回：200 application/zip，Content-Disposition: attachment；ZIP 含路线和 manifest.json，见 RoutePackageManifest。
- 错误：鉴权失败为公共 401；400 路径或空选择；413 条目/文件/总大小超限；500 打包失败；JSON {code,message,error?}。
- 副作用：创建并清理服务端临时压缩包，不修改源路线。

<a id="api-316"></a>

### POST /api/CDCollectionManagement/importRoutes

- 用途：将标准路线包导入采集目录
- URL：`{baseUrl}/api/CDCollectionManagement/importRoutes`
- 认证：apiKey（请求头）。
- 请求格式：multipart/form-data。
- 参数：Form `targetPath:string` 必填，采集目录内目标目录；File `file` 必填 ZIP ≤50MB；单文件≤10MB，展开总量≤200MB，条目≤10000。
- 返回：200 application/json `{message:string,targetPath:string,importedCount:int,skippedCount:int,importedFiles:string[],skippedFiles:string[]}`。
- 错误：鉴权失败为公共 401；400 包格式/路径/安全校验失败；413 archive_too_large 等大小错误；500 文件操作失败；{code,message,error?}。
- 副作用：写入导入路线；同名冲突按实现跳过并在 skippedFiles 返回。

<a id="api-317"></a>

### GET /api/CDCollectionManagement/blacklists

- 用途：读取账户材料黑名单及并发版本
- URL：`{baseUrl}/api/CDCollectionManagement/blacklists`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `name:string` 可选，未传取排序首个账户；无账户时 items 为空、version="missing"。
- 返回：200 application/json；`CollectionBlacklistData`：name、accounts、items、version。
- 错误：鉴权失败为公共 401；400 非法账户；503 脚本不可用；500 存储失败；{code,message}。
- 副作用：首次访问可能创建 blacklists 目录。

<a id="api-318"></a>

### POST /api/CDCollectionManagement/blacklists

- 用途：新增账户黑名单条目
- URL：`{baseUrl}/api/CDCollectionManagement/blacklists`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填合法账户名；`item:string` 必填条目；`version:string` 必填上次读取版本；请求≤64KB；账户≤120字符、不得含路径或 .json 后缀；条目去空白后1–200字符，禁控制字符；拒绝未知/重复字段。
- 返回：201 application/json `{message:"新增成功",data:CollectionBlacklistData}`。
- 错误：鉴权失败为公共 401；400 参数/条目错误；409 版本冲突或条目冲突；413 请求过大；503 脚本不可用；{code,message}。
- 副作用：新增账户黑名单，最多5000条；使用版本校验避免覆盖并发修改。

<a id="api-319"></a>

### PUT /api/CDCollectionManagement/blacklists

- 用途：修改账户黑名单条目
- URL：`{baseUrl}/api/CDCollectionManagement/blacklists`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填；`oldItem:string` 原条目；`newItem:string` 新条目；`version:string` 必填上次读取版本；请求≤64KB；账户≤120字符、不得含路径或 .json 后缀；条目去空白后1–200字符，禁控制字符；拒绝未知/重复字段。
- 返回：200 application/json `{message:"修改成功",data:CollectionBlacklistData}`。
- 错误：鉴权失败为公共 401；400 参数/条目错误；409 版本冲突或条目冲突；413 请求过大；503 脚本不可用；{code,message}。
- 副作用：修改账户黑名单，最多5000条；使用版本校验避免覆盖并发修改。

<a id="api-320"></a>

### DELETE /api/CDCollectionManagement/blacklists

- 用途：删除账户黑名单条目
- URL：`{baseUrl}/api/CDCollectionManagement/blacklists`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填；`item:string` 必填待删除条目；`version:string` 必填上次读取版本；请求≤64KB；账户≤120字符、不得含路径或 .json 后缀；条目去空白后1–200字符，禁控制字符；拒绝未知/重复字段。
- 返回：200 application/json `{message:"删除成功",data:CollectionBlacklistData}`。
- 错误：鉴权失败为公共 401；400 参数/条目错误；409 版本冲突或条目冲突；413 请求过大；503 脚本不可用；{code,message}。
- 副作用：删除账户黑名单，最多5000条；使用版本校验避免覆盖并发修改。


<a id="section-24"></a>

## BetterGI 黑名单、日志、OCR 与快捷键

源码：`menu/routes_integrations.go`、`bgiStatus/PickBlackLists.go`、`menu/UserHotKeyShortcut.go`、`JsAPI/JsAPI.go`、`JsAPI/service.go`。

<a id="api-321"></a>

### GET /api/betterGi/blackList

- 用途：读取 BetterGI 旧版拾取黑名单
- URL：`{baseUrl}/api/betterGi/blackList`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:{BlackLists:string[]}}`。
- 错误：鉴权失败为公共 401；500 {status:"error",msg:string}。
- 副作用：缺少文件时创建空 pick_black_lists.txt。

<a id="api-322"></a>

### POST /api/betterGi/addBlackList

- 用途：追加 BetterGI 拾取黑名单
- URL：`{baseUrl}/api/betterGi/addBlackList`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON 根值为 `string[]`，不是对象，如 `["材料A","材料B"]`。
- 返回：200 application/json `{status:"success",message:"添加成功"}`。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`；写入失败500 {status:"error",msg:string}。
- 副作用：改写 BetterGI 拾取黑名单。

<a id="api-323"></a>

### POST /api/betterGi/deleteBlackList

- 用途：删除单个 BetterGI 拾取黑名单项
- URL：`{baseUrl}/api/betterGi/deleteBlackList`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON 根值为 `string`，如 `"材料A"`。
- 返回：200 空响应体；成功不返回 JSON。
- 错误：鉴权失败为公共 401；处理器参数或业务失败为 400，JSON `{message:string,error?:string}`；删除失败500 {status:"error",msg:string}。
- 副作用：删除黑名单条目。

<a id="api-324"></a>

### GET /api/autoLog

- 用途：按日期读取 ABGI 日志
- URL：`{baseUrl}/api/autoLog`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `data:string` 可选，格式 YYYY-MM-DD，默认当天（字段确为 data）。
- 返回：200 application/json `{status:"success",msg:string[]}`，msg 为日志行数组。
- 错误：鉴权失败为公共 401；500 {status:"error",msg:string}，包括日期错误或日志不存在。
- 副作用：无业务写入。

<a id="api-325"></a>

### POST /api/ocr/dogFood

- 用途：通过百度 OCR 识别狗粮截图并记录数量
- URL：`{baseUrl}/api/ocr/dogFood`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `apiKey:string` 百度 OCR 密钥；`secretKey:string` 百度 OCR Secret；与请求头 ABGI apiKey 不是同一密钥。
- 返回：200 application/json `{status:"success",data:string}`，data 为 OCR 识别文本。
- 错误：鉴权失败为公共 401；400 {message,error} 参数格式；500 {status:"error",msg} OCR错误；旧处理器格式错误后未立即返回，可能继续尝试 OCR。
- 副作用：访问百度 OCR，写入狗粮数量记录。

<a id="api-326"></a>

### GET /api/HotKeyConfig/HotKeyQuery

- 用途：读取 BetterGI 系统快捷键
- URL：`{baseUrl}/api/HotKeyConfig/HotKeyQuery`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:Record<string,string>}`；键是 启动停止BetterGI、停止当前脚本/ 独立任务、暂停当前脚本/ 独立任务、游戏截图、日志与状态窗口展示开关、启动停止一条龙；值为配置按键。
- 错误：鉴权失败为公共 401；读取文件失败未显式报错，可能返回空按键。
- 副作用：无业务写入。

<a id="api-327"></a>

### POST /api/HotKeyConfig/PressKey

- 用途：向当前桌面发送按键
- URL：`{baseUrl}/api/HotKeyConfig/PressKey`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `key:string` 按键；不从 JSON 读取。
- 返回：200 application/json，JSON 字符串 `"按键成功"`。
- 错误：鉴权失败为公共 401；底层按键失败只记日志，200不保证实际输入成功。
- 副作用：模拟本机桌面按键，可能控制正在运行的应用。

<a id="api-328"></a>

### GET /api/HotKeyConfig/UserHotKeys

- 用途：读取当前认证用户自定义快捷键
- URL：`{baseUrl}/api/HotKeyConfig/UserHotKeys`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{code:200,data:UserHotKeyShortcut[]}`。
- 错误：鉴权失败为公共 401；500 {code:500,error:string}。
- 副作用：无业务写入。

<a id="api-329"></a>

### POST /api/HotKeyConfig/UserHotKeys

- 用途：创建或更新当前用户自定义快捷键
- URL：`{baseUrl}/api/HotKeyConfig/UserHotKeys`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `id:uint` 可选，非0按编号查找；`name:string` 必填≤128字符；`key:string` 必填≤64字符；名称与按键去首尾空白。
- 返回：200 application/json `{code:200,data:UserHotKeyShortcut}`。
- 错误：鉴权失败为公共 401；400 请求/长度错误；500 存储失败；{code:int,error:string}。
- 副作用：保存数据库记录；id未命中或未传时可创建，未传id按名称匹配。

<a id="api-330"></a>

### DELETE /api/HotKeyConfig/UserHotKeys

- 用途：删除当前用户自定义快捷键
- URL：`{baseUrl}/api/HotKeyConfig/UserHotKeys`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `id:uint` 必填，必须大于0。
- 返回：200 application/json `{code:200}`。
- 错误：鉴权失败为公共 401；400 {code:400,error} 非法id；500数据库错误。
- 副作用：删除自定义快捷键记录。

<a id="api-331"></a>

### POST /api/js/screenShot

- 用途：截取桌面并按坐标裁剪保存
- URL：`{baseUrl}/api/js/screenShot`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `imagesName:string` 文件基名；`imagesX:int`/`imagesY:int` 左上角像素坐标；`imagesW:int`/`imagesH:int` 宽高像素；处理器未声明额外必填校验。
- 返回：200 空响应体；图片写入 img/abgi/{imagesName}.jpg 及 {imagesName}1.jpg，不直接返回图片。
- 错误：鉴权失败为公共 401；400 JSON字符串 "ScreenShot参数错误" 或 "截图失败"；裁剪失败只记日志。
- 副作用：截图、写本地图片。

<a id="api-332"></a>

### GET /api/js/QueryHistoryVersion

- 用途：查询脚本历史备份版本
- URL：`{baseUrl}/api/js/QueryHistoryVersion`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `jsName:string` 脚本名称。
- 返回：200 application/json `{success:true,versions:string[]}`；业务失败也200 `{success:false,message:string}`。
- 错误：鉴权失败为公共 401；历史读取错误在 success:false 内返回。
- 副作用：无业务写入。

<a id="api-333"></a>

### POST /api/js/BgiRollbackHistoryVersion

- 用途：回滚 BetterGI 历史版本并重启
- URL：`{baseUrl}/api/js/BgiRollbackHistoryVersion`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `version:string` 历史版本，兼容 .7z/.zip 后缀；`jsName:string` 历史名称。
- 返回：200 application/json `{success:true,message:"回滚成功"}`；解析或回滚失败也200 `{success:false,message:string}`。
- 错误：鉴权失败为公共 401；启动路径解析失败500 {success:false,message}；进程重启可能先断开连接。
- 副作用：替换历史文件、启动 BetterGI、等待约10秒后重启 ABGI；不要自动重试。


<a id="section-25"></a>

## 矿石与红血统计

源码：`AbundantOre/Ore.go`、`redblood/record.go`。

<a id="api-334"></a>

### GET /api/abundantOre/dashboard

- 用途：查看矿石路线可挖状态与数量
- URL：`{baseUrl}/api/abundantOre/dashboard`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `profile:string` 可选，默认 "default-profile"；不存在的账户按无记录处理。
- 返回：200 application/json `{status:"success",data:OreDashboardData}`。
- 错误：鉴权失败为公共 401；500 {status:"error",msg:string} 元数据读取或解析失败。
- 副作用：无业务写入。

<a id="api-335"></a>

### GET /api/redBlood/stats

- 用途：按来源统计近七天回血与死亡
- URL：`{baseUrl}/api/redBlood/stats`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `days:int` 可选默认7；≤0或>7回退7；`source:string` 可选，空表示全部，支持 abgi/bgi（会去空白转小写）。
- 返回：200 application/json `{status:"success",data:RedBloodStats}`。
- 错误：鉴权失败为公共 401；400 {status:"error",message:string} days非数字或source非法；500统计存储失败。
- 副作用：查询时清理七天前历史记录。


<a id="section-26"></a>

## OBS 录制、回放、推流与媒体

源码：`abgiObs/http.go`、`abgiObs/types.go`、`abgiObs/manager.go`、`abgiObs/ObsVideo.go`。

所有成功响应均为 HTTP 200 application/json，外壳 `{status:"success",msg:T}`。除媒体文件列表/删除外，控制接口依赖 OBS WebSocket 可连接且具备对应 capability；配置、状态和 OBS 本身决定可用性，不因传入密钥绕过。通用错误为 `{status:"error",msg:string}`：400 参数错误、503 OBS不可用、422 不支持该功能、409 当前状态冲突、500 内部失败；均另有公共401。

<a id="api-336"></a>

### GET /api/abgiObs/Status

- 用途：查询 OBS 聚合连接与运行状态
- URL：`{baseUrl}/api/abgiObs/Status`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:OBSStatus}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：无业务写入。

<a id="api-337"></a>

### GET /api/abgiObs/Inventory

- 用途：枚举 OBS 场景、来源和音频输入
- URL：`{baseUrl}/api/abgiObs/Inventory`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:OBSInventory}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：无业务写入。

<a id="api-338"></a>

### POST /api/abgiObs/TestConnection

- 用途：临时测试 OBS 连接，不保存密码
- URL：`{baseUrl}/api/abgiObs/TestConnection`
- 认证：apiKey（请求头）。
- 请求格式：可空 JSON 请求体。
- 参数：JSON `address:string`/`password:string` 可选，空值回退当前连接配置。
- 返回：200 application/json `{status:"success",msg:ConnectionTestResult}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：会建立临时 OBS 连接，不修改 ABGI 保存配置。

<a id="api-339"></a>

### POST /api/abgiObs/StartRecording

- 用途：开始 OBS 录制
- URL：`{baseUrl}/api/abgiObs/StartRecording`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"开始录制"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：启动录制并写视频文件。

<a id="api-340"></a>

### POST /api/abgiObs/StopRecording

- 用途：停止录制并可选重命名
- URL：`{baseUrl}/api/abgiObs/StopRecording`
- 认证：apiKey（请求头）。
- 请求格式：可空 JSON 请求体。
- 参数：JSON `name:string` 可选；空值再读取 Query `videoName:string`，均空保留原名称。
- 返回：200 application/json `{status:"success",msg:MediaSaveResult}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：停止录制；可改名。改名失败时仍保留原视频并在warning中说明。

<a id="api-341"></a>

### GET /api/abgiObs/IsRecording

- 用途：读取录制状态
- URL：`{baseUrl}/api/abgiObs/IsRecording`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:OBSRawRecordingStatus}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：无业务写入。

<a id="api-342"></a>

### POST /api/abgiObs/PauseRecording

- 用途：暂停正在进行的录制
- URL：`{baseUrl}/api/abgiObs/PauseRecording`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"录制已暂停"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-343"></a>

### POST /api/abgiObs/ResumeRecording

- 用途：恢复暂停的录制
- URL：`{baseUrl}/api/abgiObs/ResumeRecording`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"录制已恢复"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-344"></a>

### POST /api/abgiObs/SplitRecording

- 用途：切换到新的录制分段
- URL：`{baseUrl}/api/abgiObs/SplitRecording`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"已开始新的录制分段"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-345"></a>

### POST /api/abgiObs/StartReplayBuffer

- 用途：启动 OBS 回放缓冲
- URL：`{baseUrl}/api/abgiObs/StartReplayBuffer`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"回放缓冲已启动"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-346"></a>

### POST /api/abgiObs/StopReplayBuffer

- 用途：停止 OBS 回放缓冲
- URL：`{baseUrl}/api/abgiObs/StopReplayBuffer`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"回放缓冲已停止"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-347"></a>

### POST /api/abgiObs/StopStream

- 用途：停止 OBS 推流
- URL：`{baseUrl}/api/abgiObs/StopStream`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"推流已停止"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 输出状态。

<a id="api-348"></a>

### POST /api/abgiObs/CreateRecordChapter

- 用途：添加录制章节标记
- URL：`{baseUrl}/api/abgiObs/CreateRecordChapter`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `name:string` 必填，去空白后非空。
- 返回：200 application/json `{status:"success",msg:"章节标记已添加"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：向当前录制写章节标记。

<a id="api-349"></a>

### GET /api/abgiObs/GetReplayBufferStatus

- 用途：查询回放缓冲是否运行
- URL：`{baseUrl}/api/abgiObs/GetReplayBufferStatus`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:OBSRawReplayStatus}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：无业务写入。

<a id="api-350"></a>

### POST /api/abgiObs/SaveReplayBuffer

- 用途：保存回放缓冲片段
- URL：`{baseUrl}/api/abgiObs/SaveReplayBuffer`
- 认证：apiKey（请求头）。
- 请求格式：可空 JSON 请求体。
- 参数：JSON `name:string` 可选，默认 "手动保存"。
- 返回：200 application/json `{status:"success",msg:MediaSaveResult}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：将回放缓冲写入视频文件并尝试重命名。

<a id="api-351"></a>

### POST /api/abgiObs/StartStream

- 用途：开始 OBS 推流
- URL：`{baseUrl}/api/abgiObs/StartStream`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"推流已启动"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：启动外部推流；GET同样有写入副作用，不要预取。

<a id="api-352"></a>

### GET /api/abgiObs/StartStream

- 用途：开始 OBS 推流（旧版 GET 兼容入口）
- URL：`{baseUrl}/api/abgiObs/StartStream`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"推流已启动"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：启动外部推流；GET同样有写入副作用，不要预取。

<a id="api-353"></a>

### GET /api/abgiObs/GetStreamStatus

- 用途：查询推流运行指标
- URL：`{baseUrl}/api/abgiObs/GetStreamStatus`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:OBSRawStreamStatus}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：无业务写入。

<a id="api-354"></a>

### POST /api/abgiObs/SetProgramScene

- 用途：切换当前节目场景
- URL：`{baseUrl}/api/abgiObs/SetProgramScene`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `sceneName:string` 必填，去空白非空。
- 返回：200 application/json `{status:"success",msg:"场景已切换"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变 OBS 当前场景。

<a id="api-355"></a>

### POST /api/abgiObs/SetSceneItemEnabled

- 用途：控制场景来源显示或隐藏
- URL：`{baseUrl}/api/abgiObs/SetSceneItemEnabled`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `sceneName:string` 必填；`sceneItemId:int` 必填且>0；`enabled:bool` 必须显式提供（false有效）。
- 返回：200 application/json `{status:"success",msg:"来源显示状态已更新"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变来源显示状态。

<a id="api-356"></a>

### POST /api/abgiObs/SetInputMute

- 用途：切换音频输入静音
- URL：`{baseUrl}/api/abgiObs/SetInputMute`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `inputName:string` 必填非空；`muted:bool` 必须显式提供。
- 返回：200 application/json `{status:"success",msg:"输入静音状态已更新"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变音频静音状态。

<a id="api-357"></a>

### POST /api/abgiObs/SetInputVolume

- 用途：设置音频输入音量
- URL：`{baseUrl}/api/abgiObs/SetInputVolume`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `inputName:string` 必填；`volumePercent:number` 必须显式提供，范围0–100。
- 返回：200 application/json `{status:"success",msg:"输入音量已更新"}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：改变音频输入音量。

<a id="api-358"></a>

### POST /api/abgiObs/SaveSourceScreenshot

- 用途：把 OBS 指定来源保存为 PNG
- URL：`{baseUrl}/api/abgiObs/SaveSourceScreenshot`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `sourceName:string` 必填；`fileName:string` 可选，清洗后作为文件基名，默认来源名；自动附时间戳。
- 返回：200 application/json `{status:"success",msg:{path:string}}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：在 OBS 保存目录的 screenshots 子目录写 PNG；不返回图片字节。

<a id="api-359"></a>

### GET /api/abgiObs/GetVideoInfo

- 用途：枚举录制目录中的视频文件
- URL：`{baseUrl}/api/abgiObs/GetVideoInfo`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:VideoInfo[]}`。
- 错误：OBS 通用错误，见本节说明。
- 副作用：递归读取配置的保存目录，不返回 screenshots 图片。

<a id="api-360"></a>

### POST /api/abgiObs/DeleteVideo

- 用途：删除单个录制视频
- URL：`{baseUrl}/api/abgiObs/DeleteVideo`
- 认证：apiKey（请求头）。
- 请求格式：可空 JSON 请求体。
- 参数：JSON `name:string` 可选；空值回退 Query `videoName:string`；至少最终值非空；只允许录制根目录内视频。
- 返回：200 application/json `{status:"success",msg:"删除成功"}`。
- 错误：OBS 通用错误，见本节说明；部分未挂稳定错误类别的文件错误为500。
- 副作用：永久删除目标视频。

<a id="api-361"></a>

### POST /api/abgiObs/DeleteAllVideo

- 用途：清空录制目录视频
- URL：`{baseUrl}/api/abgiObs/DeleteAllVideo`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",msg:"删除所有视频成功"}`。
- 错误：公共401；500 application/json `{message:string}`（不是OBS标准错误外壳）。
- 副作用：删除保存目录全部受支持视频，不删除截图或其他文件。


<a id="section-27"></a>

## 团酱多节点代理

源码：`auth/abgiTeam/service.go`、`auth/abgiTeam/types.go`、`auth/abgiTeam/client.go`、`auth/abgiTeam/quick_actions.go`。

请求头 apiKey 认证的是本机 ABGI；远端密钥在新增连接 JSON 中单独提交并加密保存，不回显。所有普通成功返回 `{status:"success",data:T}`；错误为 `{status:"error",code:string,error:string}`。JSON请求体上限128KB。公共错误：400 INVALID_REQUEST/INVALID_CONNECTION、404 CONNECTION_NOT_FOUND、409 CONNECTION_DISABLED/CONNECTION_CONFLICT、424远端鉴权或业务失败、502远端不可达/坏响应、504超时、500内部失败，认证层另有401。远端节点必须已登记且启用。

<a id="api-362"></a>

### GET /api/abgiTeam/connections

- 用途：读取全部已保存节点
- URL：`{baseUrl}/api/abgiTeam/connections`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:ConnectionView[]}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：无业务写入。

<a id="api-363"></a>

### POST /api/abgiTeam/connections

- 用途：添加远端节点并立即检测
- URL：`{baseUrl}/api/abgiTeam/connections`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `displayName:string` 必填，去空白后1–128字符、禁控制字符；`address:string` 必填≤2048，完整http/https协议主机端口，不允许凭据、查询、锚点或子路径；`apiKey:string` 远端密钥，提供时12–256字符、含ASCII字母和数字、无空白控制字符，新增时密钥必填。
- 返回：201 application/json `{status:"success",data:ConnectionView}`；检测离线仍保存连接并反映 connectionStatus。
- 错误：团酱公共错误，见本节说明。
- 副作用：保存连接与加密密钥；发起远端检测。

<a id="api-364"></a>

### PUT /api/abgiTeam/connections/:id

- 用途：更新节点并重新检测
- URL：`{baseUrl}/api/abgiTeam/connections/:id`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：Path `id:string` 必填，已保存连接编号；JSON `displayName:string` 必填，去空白后1–128字符、禁控制字符；`address:string` 必填≤2048，完整http/https协议主机端口，不允许凭据、查询、锚点或子路径；`apiKey:string` 远端密钥，提供时12–256字符、含ASCII字母和数字、无空白控制字符；编辑apiKey留空沿用原密钥。
- 返回：200 application/json `{status:"success",data:ConnectionView}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：更新连接、检测状态并失效截图缓存。

<a id="api-365"></a>

### PATCH /api/abgiTeam/connections/:id/enabled

- 用途：启用或停用节点
- URL：`{baseUrl}/api/abgiTeam/connections/:id/enabled`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：Path `id:string` 必填，已保存连接编号；JSON `enabled:bool` 必须显式提供。
- 返回：200 application/json `{status:"success",data:ConnectionView}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：更新启用状态；停用时清理截图缓存。

<a id="api-366"></a>

### DELETE /api/abgiTeam/connections/:id

- 用途：删除节点连接
- URL：`{baseUrl}/api/abgiTeam/connections/:id`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success"}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：删除本机连接记录，不关闭远端。

<a id="api-367"></a>

### POST /api/abgiTeam/connections/:id/test

- 用途：主动检测远端节点
- URL：`{baseUrl}/api/abgiTeam/connections/:id/test`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success",data:ConnectionView}`。
- 错误：团酱公共错误，见本节说明；检测失败时错误额外带 `data:{connection:ConnectionView}`。
- 副作用：刷新数据库检测时间与连接状态。

<a id="api-368"></a>

### GET /api/abgiTeam/connections/:id/status

- 用途：查询远端运行进度白名单字段
- URL：`{baseUrl}/api/abgiTeam/connections/:id/status`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success",data:RuntimeStatus}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：无业务写入。

<a id="api-369"></a>

### GET /api/abgiTeam/connections/:id/catalog

- 用途：读取远端节点概览、一条龙与配置组目录
- URL：`{baseUrl}/api/abgiTeam/connections/:id/catalog`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success",data:Catalog}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：无业务写入。

<a id="api-370"></a>

### GET /api/abgiTeam/connections/:id/hotkeys

- 用途：读取远端系统及自定义快捷键
- URL：`{baseUrl}/api/abgiTeam/connections/:id/hotkeys`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success",data:HotKeyCatalog}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：无业务写入。

<a id="api-371"></a>

### POST /api/abgiTeam/connections/:id/hotkeys/press

- 用途：向远端发送按键
- URL：`{baseUrl}/api/abgiTeam/connections/:id/hotkeys/press`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：Path `id:string` 必填，已保存连接编号；JSON `key:string` 必填，去空白后≤64字符且无控制字符。
- 返回：200 application/json `{status:"success",data:{connectionId:string,key:string}}`。
- 错误：团酱公共错误，见本节说明；400 INVALID_HOTKEY。
- 副作用：在远端桌面模拟按键，不自动重试。

<a id="api-372"></a>

### POST /api/abgiTeam/actions/one-long/start

- 用途：批量启动选定节点的一条龙
- URL：`{baseUrl}/api/abgiTeam/actions/one-long/start`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `connectionIds:string[]` 必填，原始长度1–32，元素去空白后非空并去重；`name:string` 必填一条龙名称，去空白后≤512字符且无控制字符。
- 返回：200 application/json `{status:"success",data:BatchActionResults}`。
- 错误：团酱公共错误，见本节说明；HTTP200也可能是全部失败或部分失败，应检查results。
- 副作用：对每节点互斥下发启动，不自动重试；超时结果为unknown而非保证失败。

<a id="api-373"></a>

### GET /api/abgiTeam/connections/:id/managed-scripts

- 用途：枚举远端工坊可启动脚本
- URL：`{baseUrl}/api/abgiTeam/connections/:id/managed-scripts`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 application/json `{status:"success",data:ManagedScript[]}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：无业务写入。

<a id="api-374"></a>

### POST /api/abgiTeam/actions/quick

- 用途：批量执行白名单快捷操作
- URL：`{baseUrl}/api/abgiTeam/actions/quick`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `connectionIds:string[]` 必填，原始长度1–32，元素去空白后非空并去重；`action:string` 必填，online/offline/close-yuanshen/close-bgi/script；script时 `nickname:string` 必填≤512字符且无控制字符。
- 返回：200 application/json `{status:"success",data:BatchActionResults}`。
- 错误：团酱公共错误，见本节说明；400 INVALID_ACTION；脚本不存在为结果内REMOTE_SCRIPT_NOT_FOUND。
- 副作用：控制远端联机、关闭游戏/BetterGI或启动脚本；close-bgi同时关闭原神。

<a id="api-375"></a>

### POST /api/abgiTeam/actions/script-groups/start

- 用途：批量在节点启动多个配置组
- URL：`{baseUrl}/api/abgiTeam/actions/script-groups/start`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `connectionIds:string[]` 必填，原始长度1–32，元素去空白后非空并去重；`names:string[]` 必填，1–100项，各名称非空≤512字符且无控制字符，去重。
- 返回：200 application/json `{status:"success",data:BatchActionResults}`。
- 错误：团酱公共错误，见本节说明。
- 副作用：在远端按所给名称启动配置组，不自动重试。

<a id="api-376"></a>

### GET /api/abgiTeam/connections/:id/screenshot

- 用途：读取远端桌面截图
- URL：`{baseUrl}/api/abgiTeam/connections/:id/screenshot`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：200 image/jpeg，JPEG二进制；Cache-Control: no-store/no-cache；同节点约4.5秒内可复用短期缓存。
- 错误：团酱公共错误，见本节说明；错误响应是JSON而非图片。
- 副作用：可能触发远端截图。

<a id="api-377"></a>

### GET /api/abgiTeam/connections/:id/logs/ws

- 用途：建立远端最新日志中转 WebSocket
- URL：`{baseUrl}/api/abgiTeam/connections/:id/logs/ws`
- 认证：apiKey（请求头）。
- 请求格式：WebSocket Upgrade，ws/wss。
- 参数：Path `id:string` 必填，已保存连接编号。
- 返回：101升级后发送JSON文本帧 `LogSocketMessage`：ready含fileName，line含text，error含code/message；每分钟检查日志轮换并重连新日志。
- 错误：团酱公共错误，见本节说明；升级后失败发error帧并断开；有Origin时必须与请求Host相同。
- 副作用：保持长连接；不接受任意远端地址或日志路径。


<a id="section-28"></a>

## ABGI 仓库中转

源码：`abgiRepos/proxy.go`、`abgiRepos/path_mapping.go`。

所有接口仍需要本机 apiKey。仓库分类为 js/pathing/combat/config，分别映射 BetterGI User 下 JsScript/AutoPathing/AutoFight/ScriptGroup；禁止越界路径。服务端固定中转远端仓库，不向浏览器开放任意上游URL。错误通常为 application/json `{status:"error",error:string,code:int}`，并带对应HTTP状态；成功不采用统一status/data外壳。

<a id="api-378"></a>

### GET /api/abgiRepos/categories

- 用途：获取仓库分类
- URL：`{baseUrl}/api/abgiRepos/categories`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{categories:BridgeCategory[]}`。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节。
- 副作用：无业务写入。

<a id="api-379"></a>

### GET /api/abgiRepos/tree

- 用途：获取完整分类与文件树
- URL：`{baseUrl}/api/abgiRepos/tree`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{categories:BridgeCategory[],tree:BridgeNode[]}`；无树归一化为空数组。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节。
- 副作用：无业务写入。

<a id="api-380"></a>

### GET /api/abgiRepos/files/:category/*path

- 用途：下载远端仓库文件
- URL：`{baseUrl}/api/abgiRepos/files/:category/*path`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Path `category:string` 必填分类；`path:string` 必填该分类下相对文件路径，允许多层，禁绝对路径/穿越。
- 返回：200，Content-Type取上游安全媒体类型、文件扩展名或application/octet-stream；Content-Disposition: attachment；返回原文件字节，默认下载上限100MB。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节。
- 副作用：无业务写入。

<a id="api-381"></a>

### POST /api/abgiRepos/subscriptions

- 用途：订阅仓库文件或目录并安装到本地
- URL：`{baseUrl}/api/abgiRepos/subscriptions`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `category:string` 必填分类；`path:string` 必填相对路径；`subscriber:string` 可选（空值优先认证用户，再回退X-Repository-Subscriber头，最后anonymous；最大120字节）；`directory:bool` 可选默认false，仅js/pathing支持true。
- 返回：201 application/json；`RepositorySubscriptionResult`，文件/目录分支见集成模型。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；404不存在/空目录；目录≤500文件、≤200MB；JS目录须有有效manifest.json。
- 副作用：下载并替换本地文件/目录；JS按saved_files保留用户文件并返回preservedCount。

<a id="api-382"></a>

### POST /api/abgiRepos/subscribe

- 用途：订阅仓库文件或目录并安装到本地（兼容别名）
- URL：`{baseUrl}/api/abgiRepos/subscribe`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `category:string` 必填分类；`path:string` 必填相对路径；`subscriber:string` 可选（空值优先认证用户，再回退X-Repository-Subscriber头，最后anonymous；最大120字节）；`directory:bool` 可选默认false，仅js/pathing支持true。
- 返回：201 application/json；`RepositorySubscriptionResult`，文件/目录分支见集成模型。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；404不存在/空目录；目录≤500文件、≤200MB；JS目录须有有效manifest.json。
- 副作用：下载并替换本地文件/目录；JS按saved_files保留用户文件并返回preservedCount。

<a id="api-383"></a>

### POST /api/abgiRepos/upload

- 用途：凭仓库授权码上传文件或批量文件
- URL：`{baseUrl}/api/abgiRepos/upload`
- 认证：apiKey（请求头）＋仓库授权码。
- 请求格式：multipart/form-data。
- 参数：Form `category:string` 必填分类；File `file` 必填，可重复1–500项；Form `path:string` 可重复与文件对应，空值用文件名；Header `X-Repository-Code:string` 必填或Form `code`兼容，header优先；单文件≤50MB、总文件≤200MB。
- 返回：201 application/json；单文件`RepositoryUpload`，批量`RepositoryBatchUpload`。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；403缺少仓库授权码。
- 副作用：上传到远端仓库；ABGI密钥不替代仓库授权码。

<a id="api-384"></a>

### POST /api/abgiRepos/admin/login

- 用途：登录远端仓库管理员
- URL：`{baseUrl}/api/abgiRepos/admin/login`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `username:string`/`password:string` 仓库管理账号密码，校验由上游执行。
- 返回：200 application/json `{token:string,expiresAt:string}`，token为仓库管理令牌、expiresAt为RFC3339时间。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节。
- 副作用：向远端换取令牌，不修改本机ABGI登录态。

<a id="api-385"></a>

### POST /api/abgiRepos/admin/codes

- 用途：创建限时或永久上传授权码
- URL：`{baseUrl}/api/abgiRepos/admin/codes`
- 认证：apiKey（请求头）＋Header `X-Repository-Admin-Token`；也可Authorization: Bearer <仓库令牌>（此时仍需apiKey通过外层认证）。
- 请求格式：可空 JSON 请求体。
- 参数：JSON `ttlMinutes:int` 可选，非永久时0–10080，0的实际有效期交上游默认；`permanent:bool` 可选默认false，为true时创建永久码。
- 返回：201 application/json；`RepositoryCode`。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；401缺少管理员令牌。
- 副作用：在远端创建授权码。

<a id="api-386"></a>

### GET /api/abgiRepos/admin/codes

- 用途：读取仓库授权码列表
- URL：`{baseUrl}/api/abgiRepos/admin/codes`
- 认证：apiKey（请求头）＋Header `X-Repository-Admin-Token`；也可Authorization: Bearer <仓库令牌>（此时仍需apiKey通过外层认证）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{codes:RepositoryCode[]}`。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；401管理员令牌缺失。
- 副作用：无本地写入。

<a id="api-387"></a>

### DELETE /api/abgiRepos/admin/codes/:id

- 用途：撤销仓库授权码
- URL：`{baseUrl}/api/abgiRepos/admin/codes/:id`
- 认证：apiKey（请求头）＋Header `X-Repository-Admin-Token`；也可Authorization: Bearer <仓库令牌>（此时仍需apiKey通过外层认证）。
- 请求格式：无需请求体。
- 参数：Path `id:string` 必填，1–128字符，仅ASCII字母数字、-、_。
- 返回：204，无响应体。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；401管理员令牌缺失。
- 副作用：远端撤销授权码。

<a id="api-388"></a>

### DELETE /api/abgiRepos/admin/files/:category/*path

- 用途：删除仓库文件或目录
- URL：`{baseUrl}/api/abgiRepos/admin/files/:category/*path`
- 认证：apiKey（请求头）＋Header `X-Repository-Admin-Token`；也可Authorization: Bearer <仓库令牌>（此时仍需apiKey通过外层认证）。
- 请求格式：无需请求体。
- 参数：Path `category:string` 必填分类；`path:string` 必填相对路径。
- 返回：204，无响应体。
- 错误：鉴权失败为公共 401；400 路径/参数错误，413大小超限，502上游坏响应或不可达，远端失败映射为本地HTTP状态，错误格式见本节；401管理员令牌缺失。
- 副作用：永久删除远端目标文件或支持的二级目录；不删除本地订阅副本。


<a id="section-29"></a>

## WebDAV User 备份与恢复

源码：`menu/webdav_handlers.go`、`webdavbackup/service.go`、`webdavbackup/errors.go`。

三接口不接收URL、密码或本机目录参数，均使用服务端已保存的WebDAV配置与BetterGI路径。错误统一 `{status:"error",message:string}`：400配置/本地/压缩包错误、404远端User.zip不存在、409操作忙或BetterGI运行中、502远端失败、504超时；另有公共401。

<a id="api-389"></a>

### POST /api/webdav/test

- 用途：测试已配置的WebDAV连接
- URL：`{baseUrl}/api/webdav/test`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",message:"WebDAV 连接测试成功",data:WebDAVTestResult}`。
- 错误：WebDAV通用错误，见本节说明。
- 副作用：访问远端配置目录，不执行User覆盖。

<a id="api-390"></a>

### POST /api/webdav/upload

- 用途：将当前BetterGI User压缩备份到WebDAV
- URL：`{baseUrl}/api/webdav/upload`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",message:"User 上传成功",data:WebDAVUploadResult}`。
- 错误：WebDAV通用错误，见本节说明。
- 副作用：创建本地备份并上传/覆盖远端User.zip；上传与恢复互斥。

<a id="api-391"></a>

### POST /api/webdav/download

- 用途：下载WebDAV备份并恢复本机User
- URL：`{baseUrl}/api/webdav/download`
- 认证：apiKey（请求头）。
- 请求格式：无需请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",message:"User 下载并恢复成功",data:WebDAVDownloadResult}`。
- 错误：WebDAV通用错误，见本节说明。
- 副作用：要求BetterGI已关闭；先验证ZIP与备份本机User，再替换恢复，覆盖设置前必须确认。


<a id="section-30"></a>

## 钓鱼与锄地脚本管理

源码：`JsScript/AutoFishingTeyvat/handler.go`、`JsScript/AutoFishingTeyvat/settings.go`、`JsScript/AutoHoeingOneDragon/handler.go`、`JsScript/AutoHoeingOneDragon/generator.go`。

均有 Cache-Control: no-store，要求有效BetterGI绝对路径及对应已安装脚本。成功 `{status:"success",data:T}`；错误 `{status:"error",msg:string}`，400参数或目录错误、404目标不存在、409版本/目标变更冲突、500读取写入故障；另有公共401。动态脚本设置字段必须从接口返回的schema/fields读取，不在文档虚构固定字段。

<a id="api-392"></a>

### GET /api/autoFishingTeyvat/dashboard

- 用途：读取鱼点昼夜冷却与可兑换材料看板
- URL：`{baseUrl}/api/autoFishingTeyvat/dashboard`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `profile:string` 可选，UID/自定义/房主存档标识，≤1024字节、无NUL；不传或已失效时选择最近记录并返回实际profile。
- 返回：200 application/json `{status:"success",data:FishingDashboard}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-393"></a>

### GET /api/autoFishingTeyvat/configurations

- 用途：列出已有钓鱼配置组实例
- URL：`{baseUrl}/api/autoFishingTeyvat/configurations`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:FishingConfiguration[]}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-394"></a>

### GET /api/autoFishingTeyvat/settings

- 用途：读取一个钓鱼实例的设置和并发版本
- URL：`{baseUrl}/api/autoFishingTeyvat/settings`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `groupName:string` 必填已有组名；`arrayPosition:int` 必填且≥0，为projects真实数组位置而非index。
- 返回：200 application/json `{status:"success",data:FishingSettingsData}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-395"></a>

### PUT /api/autoFishingTeyvat/settings

- 用途：按增量安全保存钓鱼实例设置
- URL：`{baseUrl}/api/autoFishingTeyvat/settings`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `groupName:string` 必填；`arrayPosition:int` 必须显式提供≥0；`revision:string` 必填原版本；`settingsPatch:object` 只含实际变更且schema允许的字段；≤1MB；字段按schema类型、枚举校验，定时停止小时分钟须同时设置或同时无。
- 返回：200 application/json `{status:"success",data:FishingSettingsData}`，返回回读值及新revision。
- 错误：本节通用错误；409需保留草稿并重新读取，不能直接覆盖重试。
- 副作用：只修改指定实例设置，不改变其他项目或未知字段。

<a id="api-396"></a>

### GET /api/autoHoeingOneDragon/dashboard

- 用途：读取锄地路线、怪物、收益与账户设置看板
- URL：`{baseUrl}/api/autoHoeingOneDragon/dashboard`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：Query `profile:string` 可选账户，空值取排序首个账户；不允许路径分隔符、NUL或首尾空白；合法但已不存在的账户回退默认并返回warnings。
- 返回：200 application/json `{status:"success",data:HoeingDashboard}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-397"></a>

### PUT /api/autoHoeingOneDragon/blacklist

- 用途：立即保存锄地账户黑名单
- URL：`{baseUrl}/api/autoHoeingOneDragon/blacklist`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `profile:string` 必填账户名≤256字节且无控制字符/分隔符/首尾空白；`version:string` 必须与看板blacklistVersion一致，首次文件缺失时可为空；`items:string[]` 完整黑名单≤1000项，每项去空白后1–128字节且无控制字符；请求≤1MB。
- 返回：200 application/json `{status:"success",data:BlacklistSnapshot}`，blacklist为去重词条、version为新摘要。
- 错误：本节通用错误；413黑名单条目超过1000。
- 副作用：按版本原子替换账户黑名单。

<a id="api-398"></a>

### GET /api/autoHoeingOneDragon/pickupItems

- 用途：枚举可复制与已保留的拾取模板
- URL：`{baseUrl}/api/autoHoeingOneDragon/pickupItems`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:PickupItemsData}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-399"></a>

### POST /api/autoHoeingOneDragon/pickupItems

- 用途：从源目录复制一个拾取模板
- URL：`{baseUrl}/api/autoHoeingOneDragon/pickupItems`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `path:string` 必填，源模板根目录内相对路径；请求≤1MB，单张模板≤8MB。
- 返回：200 application/json `{status:"success",data:PickupItemsData}`，返回操作后的完整目录列表。
- 错误：本节通用错误。
- 副作用：复制模板到保留目录。

<a id="api-400"></a>

### DELETE /api/autoHoeingOneDragon/pickupItems

- 用途：删除保留目录中的拾取模板
- URL：`{baseUrl}/api/autoHoeingOneDragon/pickupItems`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `path:string` 必填，保留模板根目录内相对路径；≤1MB。
- 返回：200 application/json `{status:"success",data:PickupItemsData}`。
- 错误：本节通用错误。
- 副作用：删除保留模板；不删除源目录原图。

<a id="api-401"></a>

### GET /api/autoHoeingOneDragon/generator/options

- 用途：读取调试路线生成器的组、默认参数和动态字段
- URL：`{baseUrl}/api/autoHoeingOneDragon/generator/options`
- 认证：apiKey（请求头）。
- 请求格式：无请求体。
- 参数：无业务参数。
- 返回：200 application/json `{status:"success",data:GeneratorOptions}`。
- 错误：本节通用错误。
- 副作用：无业务写入。

<a id="api-402"></a>

### POST /api/autoHoeingOneDragon/generator/generate

- 用途：原生生成锄地调试路线与分配报告
- URL：`{baseUrl}/api/autoHoeingOneDragon/generator/generate`
- 认证：apiKey（请求头）。
- 请求格式：application/json。
- 参数：JSON `mode:string` 可选manual默认或group；group时 `groupName:string` 必填、`arrayPosition:int` 指定锄地项目位置；manual时 `accountName:string` 可选覆盖账户、`settings:object` 用options的fields参数覆盖defaults；≤2MB。
- 返回：200 application/json `{status:"success",data:GeneratorResult}`，含debugSelection、summary、warnings、inputTime。
- 错误：本节通用错误。
- 副作用：写入调试路线及路线分配报告；不直接启动游戏任务。


<a id="section-31"></a>

## 集成接口公共数据结构

源码：`CDCollectionManagement`、`CDAwareAutoGather`、`abgiObs`、`abgiRepos`、`auth/abgiTeam`、`JsScript/AutoFishingTeyvat`、`JsScript/AutoHoeingOneDragon`、`webdavbackup`。

下列模型用于本片段中的返回类型引用。time.Time在JSON中是RFC3339时间字符串；数字时间戳单位按字段说明；`?`表示omitempty等可省略字段，`| null`表示可空值。map/Record表示动态键集合，动态脚本settings不等于任意固定字段。


#### 集成模型 BackpackMaterialResult

来源：`CDCollectionManagement/BackpackMaterial.go`（`CDCollectionManagement.BackpackMaterialResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `folderName` | string | 材料所属文件夹名称 |
| `materialName` | string | 匹配的材料名称 |
| `count` | integer | 当前配置角色背包中该材料的数量 |

#### 集成模型 CollectionBlacklistData

来源：`CDCollectionManagement/Blacklist.go`（`CDCollectionManagement.CollectionBlacklistData`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 本次实际使用的采集账户名称，无账户时为空 |
| `accounts` | string[] | 可选账户名称 |
| `items` | string[] | 当前账户去重后的黑名单词条 |
| `version` | string | 黑名单原文件内容的 SHA-256 十六进制摘要；文件不存在时为 missing |

#### 集成模型 Record

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.Record`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `FileName` | string | 文件名 |
| `CdTime` | string | 收集时间 |
| `Status` | string | 可采集/冷却中状态 |
| `History` | History[] | 历史执行记录 |

#### 集成模型 History

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.History`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `Item` | Record<string,integer> | 材料名称到数量映射；PickupRecord中外层为分类 |
| `DurationSec` | integer | 执行耗时秒数 |

#### 集成模型 MaterialRouteAnalysis

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.MaterialRouteAnalysis`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `query` | string | 原查询材料词 |
| `exact` | boolean | 是否精确匹配 |
| `latestOnly` | boolean | 是否仅分析最近一次 |
| `total` | integer | 命中路线总数 |
| `routes` | MaterialRouteRecord[] | 路线明细 |
| `summary` | MaterialRouteSummary | 聚合统计 |

#### 集成模型 MaterialRouteRecord

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.MaterialRouteRecord`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `routeName` | string | 路线名称 |
| `folderName` | string | 材料所属文件夹名称 |
| `folderPath` | string | 所在文件夹路径 |
| `path` | string | 文件或资源路径 |
| `status` | string | 当前状态 |
| `cdTime` | string | 冷却时间文本 |
| `matchedItems` | Record<string,integer> | 命中材料到数量映射 |
| `latestItems` | Record<string,integer> | 最近运行材料数量映射 |
| `latestMatched` | boolean | 最近一次是否命中 |
| `matchedCount` | integer | 匹配材料数量 |
| `matchedRuns` | integer | 命中运行次数 |
| `historyRuns` | integer | 历史运行次数 |
| `averageDurationSec` | integer | 平均耗时秒 |
| `averageEfficiency` | number | 平均每分钟采集效率 |

#### 集成模型 MaterialRouteSummary

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.MaterialRouteSummary`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `matchedItems` | Record<string,integer> | 命中材料到数量映射 |
| `latestRouteCount` | integer | 最近记录命中路线数 |
| `conflictCount` | integer | 冲突数量 |
| `conflictFolders` | string[] | 冲突文件夹列表 |

#### 集成模型 MaterialRouteFile

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.MaterialRouteFile`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `remoteFolder` | string | 远端来源目录 |
| `fileName` | string | 文件名 |
| `count` | integer | 数量 |

#### 集成模型 AddMaterialPathingResult

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.AddMaterialPathingResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `materialName` | string | 匹配的材料名称 |
| `quantity` | integer | 请求目标数量 |
| `cdType` | string | 采集冷却类型 |
| `remoteFolder` | string | 远端来源目录 |
| `selectedFiles` | MaterialRouteFile[] | 选中远端路线 |
| `selectedTotal` | integer | 选中路线材料总数 |
| `destinationFolder` | string | 本地安装目录 |
| `installedFiles` | string[] | 成功安装文件 |
| `skippedFiles` | string[] | 跳过文件 |

#### 集成模型 PickupRecord

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.PickupRecord`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `Date` | string | 记录日期 |
| `Item` | Record<string,Record<string,integer>> | 结构：{"采集": {"晶蝶": 5, "铁块": 3}, "怪物掉落": {"史莱姆凝液": 10}} |

#### 集成模型 CollectionFileNode

来源：`CDCollectionManagement/CDCollectionManagement.go`（`CDCollectionManagement.FileNode`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 文件名 |
| `path` | string | 完整路径 |
| `is_dir` | boolean | 是否是目录 |
| `disabled` | boolean | 是否为已禁用路线 |
| `size` | integer | 大小 (字节) |
| `children?` | (CollectionFileNode 或 null)[] | 子节点；文件通常省略，元素为指针类型 |
| `record` | Record | 当前路线运行记录；无匹配记录时仍返回零值对象，History 可为 null |

#### 集成模型 CookingIngredient

来源：`CDCollectionManagement/CookingRecipe.go`（`CDCollectionManagement.CookingIngredient`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `amount` | integer | 单份食材用量 |

#### 集成模型 CookingRecipe

来源：`CDCollectionManagement/CookingRecipe.go`（`CDCollectionManagement.CookingRecipe`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `rarity` | integer | 料理星级 |
| `category` | string | 料理效果分类，如提升暴击、恢复体力 |
| `type` | string | 类型 |
| `region` | string | 所属地区 |
| `version` | string | 料理加入的游戏版本，如 1.0 |
| `ingredients` | CookingIngredient[] | 食材明细 |
| `baseRecipe?` | string | 特殊料理的基础食谱 |
| `character?` | string | 特殊料理对应角色 |

#### 集成模型 CookingRecipeCatalog

来源：`CDCollectionManagement/CookingRoute.go`（`CDCollectionManagement.CookingRecipeCatalog`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `dataUpdated` | string | 内置食谱数据更新时间 |
| `sourceUrl` | string | 食谱来源URL |
| `recipes` | CookingRecipe[] | 食谱列表 |

#### 集成模型 CookingRouteIngredient

来源：`CDCollectionManagement/CookingRoute.go`（`CDCollectionManagement.CookingRouteIngredient`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `unitAmount` | integer | 每份所需数量 |
| `requiredAmount` | integer | 本次总需求数量 |
| `routeEligible` | boolean | 能否通过路线采集 |
| `skipReason?` | string | 跳过原因 |
| `status?` | string | 当前状态 |
| `message?` | string | 可读操作说明 |
| `route?` | AddMaterialPathingResult | 该食材的材料路线安装结果，无结果时省略 |

#### 集成模型 CookingRoutePlan

来源：`CDCollectionManagement/CookingRoute.go`（`CDCollectionManagement.CookingRoutePlan`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `recipe` | CookingRecipe | 选中料理食谱 |
| `servings` | integer | 料理份数 |
| `ingredients` | CookingRouteIngredient[] | 食材明细 |

#### 集成模型 AddCookingPathingResult

来源：`CDCollectionManagement/CookingRoute.go`（`CDCollectionManagement.AddCookingPathingResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `recipe` | CookingRecipe | 选中料理食谱 |
| `servings` | integer | 料理份数 |
| `ingredients` | CookingRouteIngredient[] | 食材明细 |
| `addedCount` | integer | 新安装食材数 |
| `existingCount` | integer | 已有食材数 |
| `skippedCount` | integer | 跳过条数 |
| `failedCount` | integer | 失败条数 |

#### 集成模型 RouteCooldownFailure

来源：`CDCollectionManagement/RouteCooldown.go`（`CDCollectionManagement.RouteCooldownFailure`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `path` | string | 保留发生错误的路线路径。 |
| `message` | string | 提供可直接显示的中文原因。 |

#### 集成模型 RouteCooldownResult

来源：`CDCollectionManagement/RouteCooldown.go`（`CDCollectionManagement.RouteCooldownResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `message` | string | 描述本次操作的总体结果。 |
| `successCount` | integer | 统计匹配到记录的有效路线数。 |
| `skippedCount` | integer | 统计没有记录而无需重置的路线数。 |
| `failedCount` | integer | 统计校验失败的路线数。 |
| `paths` | string[] | 返回全部成功处理的规范路径。 |
| `skippedPaths` | string[] | 返回没有匹配记录的规范路径。 |
| `failures` | RouteCooldownFailure[] | 返回每条失败路径及其原因。 |

#### 集成模型 RouteDeleteFailure

来源：`CDCollectionManagement/RouteDelete.go`（`CDCollectionManagement.RouteDeleteFailure`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `path` | string | 文件或资源路径 |
| `message` | string | 可读操作说明 |

#### 集成模型 RouteDeleteResult

来源：`CDCollectionManagement/RouteDelete.go`（`CDCollectionManagement.RouteDeleteResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `message` | string | 可读操作说明 |
| `paths` | string[] | 成功处理的路径 |
| `successCount` | integer | 成功条数 |
| `failedCount` | integer | 失败条数 |
| `failures?` | RouteDeleteFailure[] | 逐项失败原因 |

#### 集成模型 RoutePackageManifest

来源：`CDCollectionManagement/RoutePackage.go`（`CDCollectionManagement.RoutePackageManifest`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `packageType` | string | 固定abgi.collection-routes |
| `schemaVersion` | integer | 包结构版本，当前1 |
| `sourceRelativePath` | string | 导出来源相对目录 |
| `routeCount` | integer | 路线数量 |
| `generatedAt` | string (RFC3339) | 路线包生成时间（UTC） |

#### 集成模型 RouteToggleFailure

来源：`CDCollectionManagement/RouteToggle.go`（`CDCollectionManagement.RouteToggleFailure`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `path` | string | 文件或资源路径 |
| `message` | string | 可读操作说明 |

#### 集成模型 RouteToggleResult

来源：`CDCollectionManagement/RouteToggle.go`（`CDCollectionManagement.RouteToggleResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `message` | string | 可读操作说明 |
| `paths` | string[] | 成功处理的路径 |
| `successCount` | integer | 成功条数 |
| `failedCount` | integer | 失败条数 |
| `failures?` | RouteToggleFailure[] | 逐项失败原因 |

#### 集成模型 OreRouteStatus

来源：`AbundantOre/Ore.go`（`AbundantOre.RouteStatus`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 路线显示名（去掉 .json 后缀） |
| `ore_type` | string | 矿石类型 key |
| `ore_type_name` | string | 矿石类型中文名 |
| `ore_count` | integer | 从文件名提取的矿石数量 |
| `region` | string | 地区中文名 |
| `status` | string | "available"（可挖）或 "waiting"（待刷新） |
| `last_run_time` | integer | 最后运行时间（毫秒），0 表示从未运行 |
| `next_refresh_time` | integer | 下次可挖时间（毫秒），可挖时为 0 |
| `hours_until_refresh` | number | 距刷新剩余小时数，可挖时为 0 |
| `avg_yield` | number | 平均产量 |
| `avg_time_consumed` | number | 平均耗时（秒） |

#### 集成模型 OreTypeStats

来源：`AbundantOre/Ore.go`（`AbundantOre.OreTypeStats`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `available` | integer | 可挖矿石总数 |
| `waiting` | integer | 待刷新矿石总数 |
| `available_routes` | integer | 可挖路线数 |
| `waiting_routes` | integer | 待刷新路线数 |

#### 集成模型 OreDashboardData

来源：`AbundantOre/Ore.go`（`AbundantOre.DashboardData`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `profiles` | string[] | 所有账号列表 |
| `total_available` | integer | 全部可挖路线总数 |
| `total_waiting` | integer | 全部待刷新路线总数 |
| `total_available_ore` | integer | 全部可挖矿石总数 |
| `total_waiting_ore` | integer | 全部待刷新矿石总数 |
| `by_type` | Record<string,OreTypeStats> | 按矿石类型分组统计 |
| `routes` | OreRouteStatus[] | 所有路线状态列表 |

#### 集成模型 VideoInfo

来源：`abgiObs/ObsVideo.go`（`abgiObs.VideoInfo`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `path` | string | 文件或资源路径 |
| `relativePath` | string | 相对录制根目录路径 |
| `extension` | string | 文件扩展名 |
| `sizeBytes` | integer | 文件字节数 |
| `sizeMB` | number | 文件大小MB |
| `modifiedTime` | string | 本地修改时间 YYYY-MM-DD HH:mm:ss |

#### 集成模型 Capabilities

来源：`abgiObs/types.go`（`abgiObs.Capabilities`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `pauseRecording` | boolean | 对应OBS控制功能是否可用 |
| `resumeRecording` | boolean | 对应OBS控制功能是否可用 |
| `splitRecording` | boolean | 对应OBS控制功能是否可用 |
| `recordChapter` | boolean | 对应OBS控制功能是否可用 |
| `replayBufferControl` | boolean | 对应OBS控制功能是否可用 |
| `streamControl` | boolean | 对应OBS控制功能是否可用 |
| `sceneControl` | boolean | 对应OBS控制功能是否可用 |
| `sceneItemControl` | boolean | 对应OBS控制功能是否可用 |
| `inputMuteControl` | boolean | 对应OBS控制功能是否可用 |
| `inputVolumeControl` | boolean | 对应OBS控制功能是否可用 |
| `sourceScreenshot` | boolean | 对应OBS控制功能是否可用 |
| `replayBufferPath` | boolean | 对应OBS控制功能是否可用 |

#### 集成模型 RecordingState

来源：`abgiObs/types.go`（`abgiObs.RecordingState`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `active` | boolean | 输出是否正在运行 |
| `paused` | boolean | 录制是否暂停 |
| `bytes` | number | 累计字节数 |
| `durationMs` | number | 持续时间毫秒 |
| `timecode` | string | OBS输出时间码 |

#### 集成模型 ReplayBufferState

来源：`abgiObs/types.go`（`abgiObs.ReplayBufferState`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `active` | boolean | 输出是否正在运行 |

#### 集成模型 StreamState

来源：`abgiObs/types.go`（`abgiObs.StreamState`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `active` | boolean | 输出是否正在运行 |
| `reconnecting` | boolean | 是否正在重连 |
| `congestion` | number | 拥塞指标 |
| `bytes` | number | 累计字节数 |
| `durationMs` | number | 持续时间毫秒 |
| `skippedFrames` | number | 跳帧数 |
| `totalFrames` | number | 总帧数 |
| `timecode` | string | OBS输出时间码 |

#### 集成模型 HealthStats

来源：`abgiObs/types.go`（`abgiObs.HealthStats`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `fps` | number | 渲染帧率 |
| `cpuPercent` | number | CPU使用百分比 |
| `memoryMb` | number | 内存MB |
| `availableDiskSpaceMb` | number | 剩余磁盘MB |
| `averageFrameRenderTime` | number | 平均帧渲染时间 |
| `outputSkippedFrames` | number | 输出跳帧数 |
| `outputTotalFrames` | number | 输出总帧数 |
| `renderSkippedFrames` | number | 渲染跳帧数 |
| `renderTotalFrames` | number | 渲染总帧数 |

#### 集成模型 OBSStatus

来源：`abgiObs/types.go`（`abgiObs.OBSStatus`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `connected` | boolean | 是否已连接OBS |
| `lastError?` | string | 最近错误（脱敏） |
| `lastErrorAt?` | string (RFC3339) | 最近错误时间，无错误时间时省略 |
| `obsVersion?` | string | OBS版本 |
| `webSocketVersion?` | string | OBS WebSocket版本 |
| `protocolVersion?` | string | WebSocket协议版本 |
| `rpcVersion?` | number | RPC版本 |
| `platform?` | string | 平台名称 |
| `platformDescription?` | string | 平台描述 |
| `recordDirectory?` | string | OBS实际录制目录 |
| `configuredSavePath?` | string | ABGI配置保存目录 |
| `directoryMismatch` | boolean | 实际目录是否不同于配置 |
| `currentScene?` | string | 当前节目场景 |
| `capabilities` | Capabilities | 按OBS协议和状态判断的可用能力 |
| `recording` | RecordingState | 录制状态 |
| `replayBuffer` | ReplayBufferState | 回放缓冲状态 |
| `stream` | StreamState | 推流状态 |
| `health` | HealthStats | 性能和帧统计 |
| `updatedAt` | string (RFC3339) | 更新时间 |

#### 集成模型 SceneItemInfo

来源：`abgiObs/types.go`（`abgiObs.SceneItemInfo`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 记录或资源编号 |
| `sourceName` | string | 来源名称 |
| `sourceUuid` | string | 来源UUID |
| `sourceType` | string | 来源类型 |
| `inputKind` | string | 输入种类 |
| `enabled` | boolean | 是否启用或显示 |
| `locked` | boolean | 来源是否锁定 |
| `isGroup` | boolean | 是否来源组 |

#### 集成模型 SceneInfo

来源：`abgiObs/types.go`（`abgiObs.SceneInfo`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `uuid` | string | 资源UUID |
| `index` | integer | 实例或场景索引 |
| `current` | boolean | 是否当前场景 |
| `items` | SceneItemInfo[] | 当前场景中的来源实例列表 |

#### 集成模型 InputInfo

来源：`abgiObs/types.go`（`abgiObs.InputInfo`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `uuid` | string | 资源UUID |
| `kind` | string | 输入类型 |
| `muted` | boolean | 是否静音 |
| `volumePercent` | number | 音量0–100 |

#### 集成模型 OBSInventory

来源：`abgiObs/types.go`（`abgiObs.OBSInventory`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `scenes` | SceneInfo[] | 场景列表 |
| `inputs` | InputInfo[] | 音频输入列表 |

#### 集成模型 ConnectionTestResult

来源：`abgiObs/types.go`（`abgiObs.ConnectionTestResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `obsVersion` | string | OBS版本 |
| `webSocketVersion` | string | OBS WebSocket版本 |
| `protocolVersion` | string | WebSocket协议版本 |
| `rpcVersion` | number | RPC版本 |
| `recordDirectory` | string | OBS实际录制目录 |

#### 集成模型 MediaSaveResult

来源：`abgiObs/types.go`（`abgiObs.MediaSaveResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `path` | string | 文件或资源路径 |
| `warning?` | string | 已保存但改名等后处理未完成的警告 |

#### 集成模型 BridgeCategory

来源：`abgiRepos/proxy.go`（`abgiRepos.bridgeCategory`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `key` | string | 仓库分类键：js/pathing/combat/config |
| `name` | string | 名称 |
| `description` | string | 分类说明 |

#### 集成模型 BridgeNode

来源：`abgiRepos/proxy.go`（`abgiRepos.bridgeNode`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `path` | string | 文件或资源路径 |
| `type` | string | 类型 |
| `size?` | integer | 字节大小 |
| `modified?` | JSON value | 远端修改时间，保留原JSON值 |
| `children?` | (BridgeNode 或 null)[] | 递归子节点，元素为指针类型 |

#### 集成模型 RepositoryUpload

来源：`abgiRepos/proxy.go`（`abgiRepos.uploadResponse`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `category` | string | 仓库分类键：js/pathing/combat/config |
| `path` | string | 文件或资源路径 |
| `name` | string | 名称 |
| `size` | integer | 字节大小 |

#### 集成模型 RepositoryBatchUpload

来源：`abgiRepos/proxy.go`（`abgiRepos.batchUploadResponse`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `category` | string | 仓库分类键：js/pathing/combat/config |
| `files` | RepositoryUpload[] | 上传结果明细 |
| `count` | integer | 数量 |
| `size` | integer | 字节大小 |

#### 集成模型 RepositoryCode

来源：`abgiRepos/proxy.go`（`abgiRepos.codeResponse`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 记录或资源编号 |
| `code?` | string | 仓库授权码 |
| `createdAt` | string (RFC3339) | 创建时间 |
| `expiresAt?` | string (RFC3339) | 过期时间；永久码可省略 |
| `permanent?` | boolean | Permanent 用于把仓库服务返回的永久授权码类型透传给管理界面。 |

#### 集成模型 ConnectionView

来源：`auth/abgiTeam/types.go`（`abgiTeam.ConnectionView`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 记录或资源编号 |
| `displayName` | string | 用户填写的节点名称 |
| `systemName` | string | 远端系统名称 |
| `address` | string | 规范化远端http/https地址 |
| `hasApiKey` | boolean | 是否保存密钥，仅布尔值不回显密钥 |
| `enabled` | boolean | 是否启用或显示 |
| `connectionStatus` | string | unknown/online/offline/auth_failed |
| `lastCheckedAt` | string (RFC3339) 或 null | 最后检测时间，可null |
| `createdAt` | string (RFC3339) | 创建时间 |
| `updatedAt` | string (RFC3339) | 更新时间 |

#### 集成模型 RuntimeStatus

来源：`auth/abgiTeam/types.go`（`abgiTeam.RuntimeStatus`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `group` | string | 当前配置组 |
| `expectedToEnd` | string | 预计完成时间文本 |
| `line` | string | 当前路线文本 |
| `scriptName` | string | 脚本清单名称或当前脚本 |
| `progress` | string | 当前进度文本 |
| `running` | boolean | 是否运行中 |
| `jsProgress` | string | JS运行进度 |
| `title` | string | 远端标题 |
| `isAdmin` | boolean 或 null | 是否管理员，可null |
| `sampledAt` | string (RFC3339) | 服务端采样时间 |

#### 集成模型 Catalog

来源：`auth/abgiTeam/types.go`（`abgiTeam.Catalog`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `connection` | ConnectionView | 当前节点连接摘要 |
| `nodeStatus` | Record<string,JSON value> | 远端运行状态对象，非固定模型 |
| `oneLongs` | string[] | 远端一条龙名称列表 |
| `scriptGroups` | string[] | 远端配置组名称列表 |

#### 集成模型 HotKeyItem

来源：`auth/abgiTeam/types.go`（`abgiTeam.HotKeyItem`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 名称 |
| `key` | string | 按键字符串，已去除首尾空白 |

#### 集成模型 UserHotKeyItem

来源：`auth/abgiTeam/types.go`（`abgiTeam.UserHotKeyItem`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 记录或资源编号 |
| `name` | string | 名称 |
| `key` | string | 按键字符串，已去除首尾空白 |

#### 集成模型 HotKeyCatalog

来源：`auth/abgiTeam/types.go`（`abgiTeam.HotKeyCatalog`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `system` | HotKeyItem[] | 系统快捷键列表 |
| `custom` | UserHotKeyItem[] | 自定义快捷键列表 |

#### 集成模型 LogSocketMessage

来源：`auth/abgiTeam/types.go`（`abgiTeam.LogSocketMessage`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `type` | string | 类型 |
| `fileName?` | string | 文件名 |
| `text?` | string | 日志原始文本 |
| `code?` | string | 错误代码 |
| `message?` | string | 可读操作说明 |

#### 集成模型 ManagedScript

来源：`auth/abgiTeam/quick_actions.go`（`abgiTeam.ManagedScript`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 记录或资源编号 |
| `nickname` | string | 脚本昵称 |

#### 集成模型 ActionResult

来源：`auth/abgiTeam/types.go`（`abgiTeam.ActionResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `connectionId` | string | 节点连接编号 |
| `displayName?` | string | 用户填写的节点名称 |
| `status` | string | success/failed/unknown/busy |
| `code?` | string | 错误代码 |
| `message` | string | 可读操作说明 |
| `statusCode?` | integer | 该节点操作结果HTTP语义码 |

#### 集成模型 FishingPeriod

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Period`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `status` | string | ready/cooling/unrecorded/invalid |
| `lastRecordedAt` | integer 或 null | 上次记录的毫秒时间戳，缺失时为 null。 |
| `readyAt` | integer 或 null | 按脚本规则计算的恢复时间，缺失或异常时为 null。 |

#### 集成模型 FishingPoint

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Point`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `key` | string | 存档中使用的地区、类型和地点原始键。 |
| `area` | string | 点位所属地区。 |
| `name` | string | 地点名称。 |
| `routeType` | string | 普通、战斗或 GCM 路线类型。 |
| `fish` | string[] | 规范化别名后用于查询的鱼类名称。 |
| `baits` | string[] | 此点位使用的鱼饵。 |
| `daytime` | FishingPeriod | 白天独立记录。 |
| `nighttime` | FishingPeriod | 夜晚独立记录。 |

#### 集成模型 FishingFish

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Fish`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 鱼类名称。 |
| `bait` | string | 对应鱼饵名称。 |
| `time` | string | 白天、夜晚或全天。 |

#### 集成模型 FishingMaterial

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Material`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 兑换目标名称。 |
| `fish` | string[] | 所需鱼类名称列表。 |

#### 集成模型 FishingProfile

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Profile`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | string | 完整保留脚本存档标识。 |
| `lastRecordedAt` | integer 或 null | 此标识最新一条有效记录的毫秒时间。 |

#### 集成模型 FishingDashboard

来源：`JsScript/AutoFishingTeyvat/dashboard.go`（`AutoFishingTeyvat.Dashboard`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `version` | string | 已安装脚本清单中的版本。 |
| `profiles` | FishingProfile[] | 按最新记录倒序排列的可选记录标识。 |
| `profile` | string | 本次快照实际使用的记录标识。 |
| `serverNow` | integer | 服务端当前毫秒时间。 |
| `timezone` | string | 服务端本地时区的当前偏移说明。 |
| `warnings` | string[] | 不影响主数据读取的兼容性提示。 |
| `points` | FishingPoint[] | 当前脚本路径清单中的点位。 |
| `historyPoints` | FishingPoint[] | 仅存在于记录中的旧点位。 |
| `fish` | FishingFish[] | 供筛选使用的鱼类元数据。 |
| `baits` | string[] | 供筛选使用的鱼饵元数据。 |
| `materials` | FishingMaterial[] | 供筛选使用的兑换目标元数据。 |
| `regions` | string[] | 当前点位实际出现的地区。 |

#### 集成模型 FishingConfiguration

来源：`JsScript/AutoFishingTeyvat/settings.go`（`AutoFishingTeyvat.Configuration`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `groupName` | string | 配置组文件名，不含扩展名。 |
| `arrayPosition` | integer | 实例在 projects 数组中的真实位置。 |
| `name` | string | 配置实例的展示名称。 |
| `index` | JSON value | 保留原配置中的实例序号。 |
| `status` | string | 保留原配置中的启用状态。 |

#### 集成模型 FishingSettingsField

来源：`JsScript/AutoFishingTeyvat/settings.go`（`AutoFishingTeyvat.SettingsField`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name?` | string | 字段名称，分隔符可为空。 |
| `type` | string | 表单控件类型。 |
| `label?` | string | 脚本提供的字段说明。 |
| `options?` | string[] | 枚举字段可选值。 |
| `default?` | JSON value | 脚本默认值，仅用于展示。 |

#### 集成模型 FishingSettingsData

来源：`JsScript/AutoFishingTeyvat/settings.go`（`AutoFishingTeyvat.SettingsData`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `groupName` | string | 当前配置组名称。 |
| `arrayPosition` | integer | 当前实例的数组位置。 |
| `revision` | string | 配置组与字段定义的联合摘要。 |
| `schema` | FishingSettingsField[] | 有序的脚本字段定义。 |
| `settings` | Record<string,JSON value> | 实际保存的设置，不自动填充缺省值。 |

#### 集成模型 HoeingDashboard

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.Dashboard`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `version` | string | 返回脚本清单中的版本号。 |
| `serverNow` | integer | 返回服务端当前毫秒时间戳。 |
| `timezone` | string | 返回服务端当前时区偏移。 |
| `account` | string | 返回本次实际选择的账户。 |
| `accounts` | string[] | 返回可选择的账户名称列表。 |
| `warnings` | string[] | 返回不影响其他数据的兼容性提示。 |
| `summary` | Summary | 返回路线和运行数据汇总。 |
| `routes` | Route[] | 返回按路径文件排序的路线明细。 |
| `settings` | Record<string,JSON value> | 返回当前账户的原始设置对象。 |
| `blacklist` | string[] | 返回当前账户的黑名单词条。 |
| `blacklistVersion` | string | 返回黑名单文件版本。 |
| `pickupItems` | PickupItemsData | 返回拾取模板源目录和保留目录。 |
| `debugSelection` | DebugSelection | 返回脚本调试结果选择范围。 |
| `debugReport` | string | 返回路线分配结果文本。 |
| `debugReportUpdatedAt` | integer | 返回路线分配结果文件修改时间。 |
| `distributions` | Distributions | 返回图表所需的分布数据。 |

#### 集成模型 DebugSelection

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.DebugSelection`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `available` | boolean | 标记是否存在有效调试结果路线。 |
| `generatedAt` | integer | 返回调试结果文件最后修改时间。 |
| `totalRoutes` | integer | 返回调试结果中的路线总数。 |
| `groups` | DebugGroup[] | 返回每个执行组的路线数量。 |

#### 集成模型 DebugGroup

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.DebugGroup`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 返回路径组一、路径组二等中文名称。 |
| `routeCount` | integer | 返回该执行组的路线数量。 |

#### 集成模型 Summary

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.Summary`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `totalRoutes` | integer | 返回路径全集数量。 |
| `routeCount` | integer | 返回路径全集数量的兼容别名。 |
| `availableCount` | integer | 返回当前可运行路线数量。 |
| `runnableCount` | integer | 返回按脚本规则当前可执行路线数量。 |
| `coolingCount` | integer | 返回处于冷却中的路线数量。 |
| `unrecordedCount` | integer | 返回没有运行记录的路线数量。 |
| `invalidCount` | integer | 返回数据异常路线数量。 |
| `runCount` | integer | 返回所有路线的有效运行次数。 |
| `expectedDuration` | number | 返回所有路线预计耗时总秒数。 |
| `averageDuration` | number | 返回所有有效历史耗时的平均秒数。 |
| `recentDuration` | number | 返回有历史记录路线的最近一次平均秒数。 |
| `eliteCount` | integer | 返回路线描述中的精英怪数量。 |
| `monsterCount` | integer | 返回路线描述中的普通怪数量。 |
| `expectedMora` | number | 返回所有路线预计摩拉总数。 |

#### 集成模型 Route

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.Route`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `key` | string | 返回相对于 pathing 目录的稳定路径键。 |
| `fileName` | string | 返回记录文件使用的路线文件名。 |
| `name` | string | 返回路线展示名称。 |
| `pathGroup` | string | 返回路线所在的路径组目录。 |
| `executionGroup` | string | 返回调试结果中的实际执行组。 |
| `sourceGroup` | string | 返回原始 pathing 来源文件夹。 |
| `region` | string | 返回根据路线编号推断的地区。 |
| `tags` | string[] | 返回路线标签。 |
| `status` | string | available/cooling/unrecorded/invalid |
| `canRun` | boolean | 返回脚本当前是否会执行该路线。 |
| `cdTime` | string | 返回脚本原始冷却时间文本。 |
| `cooldownAt` | integer 或 null | 返回冷却结束的毫秒时间戳。 |
| `cooldownEndsAt` | integer 或 null | 返回冷却结束时间的兼容字段。 |
| `expectedDuration` | number | 返回路线预计耗时秒数。 |
| `averageDuration` | number | 返回路线历史平均耗时秒数。 |
| `recentDuration` | number | 返回路线最近一次有效耗时秒数。 |
| `runCount` | integer | 返回路线有效运行次数。 |
| `items` | string[] | 返回路线最近拾取物名称列表。 |
| `monsters` | Monster[] | 返回路线描述中的怪物明细。 |
| `eliteCount` | integer | 返回路线精英怪数量。 |
| `monsterCount` | integer | 返回路线普通怪数量。 |
| `expectedMora` | number | 返回路线预计收益摩拉。 |

#### 集成模型 Monster

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.Monster`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 返回怪物名称。 |
| `count` | integer | 返回怪物数量。 |
| `type` | string | 返回普通、精英或首领类型。 |
| `moraRate` | number | 返回怪物摩拉倍率。 |
| `items` | string[] | 返回该怪物可能掉落的物品。 |
| `expectedMora` | number | 返回该怪物预计贡献的摩拉。 |

#### 集成模型 DistributionItem

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.DistributionItem`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 返回分类名称。 |
| `count` | integer | 返回分类计数。 |

#### 集成模型 DurationDistribution

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.DurationDistribution`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `key` | string | 返回路线键。 |
| `name` | string | 返回路线名称。 |
| `expected` | number | 返回预计耗时秒数。 |
| `average` | number | 返回历史平均耗时秒数。 |
| `recent` | number | 返回最近一次耗时秒数。 |

#### 集成模型 Distributions

来源：`JsScript/AutoHoeingOneDragon/dashboard.go`（`AutoHoeingOneDragon.Distributions`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `status` | DistributionItem[] | 返回路线状态分布。 |
| `pathGroup` | DistributionItem[] | 返回路径组分布。 |
| `region` | DistributionItem[] | 返回地区分布。 |
| `tag` | DistributionItem[] | 返回标签分布。 |
| `item` | DistributionItem[] | 返回拾取物分布。 |
| `duration` | DurationDistribution[] | 返回路线耗时对比数据。 |

#### 集成模型 GeneratorField

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorField`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `name` | string | 返回参数名称。 |
| `label` | string | 返回参数中文标签。 |
| `type` | string | 返回 input、checkbox 或 textarea。 |
| `default` | JSON value | 返回脚本默认值。 |

#### 集成模型 GeneratorProjectOption

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorProjectOption`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `arrayPosition` | integer | 返回 projects 数组位置。 |
| `index` | integer | 返回 BetterGI 原始项目索引。 |
| `name` | string | 返回项目名称。 |
| `accountName` | string | 返回项目配置中的账户名称。 |
| `settings` | Record<string,JSON value> | 返回路线选择相关配置。 |

#### 集成模型 GeneratorGroupOption

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorGroupOption`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `groupName` | string | 返回配置组名称。 |
| `projects` | GeneratorProjectOption[] | 返回其中的锄地 JS 项目。 |

#### 集成模型 GeneratorOptions

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorOptions`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `groups` | GeneratorGroupOption[] | 返回可选配置组。 |
| `defaults` | Record<string,JSON value> | 返回脚本默认参数。 |
| `fields` | GeneratorField[] | 返回手动表单字段定义。 |

#### 集成模型 GeneratorSummary

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorSummary`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `totalRoutes` | integer | 返回选中路线总数。 |
| `eliteCount` | integer | 返回选中精英数量。 |
| `monsterCount` | integer | 返回选中小怪数量。 |
| `expectedMora` | number | 返回选中路线预计摩拉。 |
| `expectedDuration` | number | 返回选中路线预计耗时。 |

#### 集成模型 GeneratorResult

来源：`JsScript/AutoHoeingOneDragon/generator.go`（`AutoHoeingOneDragon.GeneratorResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `debugSelection` | DebugSelection | 返回新的调试结果范围。 |
| `summary` | GeneratorSummary | 返回新的调试统计。 |
| `warnings` | string[] | 返回非阻断解析警告。 |
| `inputTime` | integer | 返回本次读取输入快照的毫秒时间。 |

#### 集成模型 BlacklistSnapshot

来源：`JsScript/AutoHoeingOneDragon/management.go`（`AutoHoeingOneDragon.BlacklistSnapshot`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `blacklist` | string[] | 返回去重后的黑名单内容。 |
| `version` | string | 返回黑名单文件内容哈希。 |

#### 集成模型 PickupItem

来源：`JsScript/AutoHoeingOneDragon/management.go`（`AutoHoeingOneDragon.PickupItem`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `path` | string | 返回相对于所在模板根目录的斜杠路径。 |
| `name` | string | 返回模板文件名。 |
| `added` | boolean | 标记源模板是否已经存在于保留目录。 |

#### 集成模型 PickupItemsData

来源：`JsScript/AutoHoeingOneDragon/management.go`（`AutoHoeingOneDragon.PickupItemsData`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `kept` | PickupItem[] | 返回其他/保留目录中的模板。 |
| `available` | PickupItem[] | 返回怪物掉落材料目录中的模板。 |

#### 集成模型 WebDAVTestResult

来源：`webdavbackup/service.go`（`webdavbackup.TestResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `connected` | boolean | WebDAV 连接测试是否成功，成功响应为 true |
| `remotePath` | string | 远端User.zip路径 |

#### 集成模型 WebDAVUploadResult

来源：`webdavbackup/service.go`（`webdavbackup.UploadResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `remotePath` | string | 远端User.zip路径 |
| `localBackup` | string | 本地备份路径 |
| `bytes` | integer | 本次上传 ZIP 备份包的字节数 |

#### 集成模型 WebDAVDownloadResult

来源：`webdavbackup/service.go`（`webdavbackup.DownloadResult`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `remotePath` | string | 远端User.zip路径 |
| `localBackup` | string | 恢复前原有 User 的本地 ZIP 备份路径；原 User 不存在时为空字符串 |
| `bytes` | integer | 本次下载 ZIP 包的字节数 |
| `restored` | boolean | 是否已恢复完成 |
| `hadLocalBackup` | boolean | 本次恢复前是否存在原 User，并已为其创建本地备份 |

#### 集成模型 RedBloodStatsSummary

来源：`redblood/record.go`（`redblood.RedBloodStatsSummary`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `today_heal` | integer | 今日回血次数 |
| `today_death` | integer | 今日死亡次数 |
| `total_heal` | integer | 期间回血次数 |
| `total_death` | integer | 期间死亡次数 |
| `record_count` | integer | 记录总数 |

#### 集成模型 RedBloodDailyStats

来源：`redblood/record.go`（`redblood.RedBloodDailyStats`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `date` | string | 日期 |
| `heal_count` | integer | 回血次数 |
| `death_count` | integer | 死亡次数 |

#### 集成模型 RedBloodRouteStats

来源：`redblood/record.go`（`redblood.RedBloodRouteStats`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `group_name` | string | 配置组名称 |
| `route_name` | string | 路线名称 |
| `heal_count` | integer | 回血次数 |
| `death_count` | integer | 死亡次数 |
| `last_time` | string (RFC3339) | 最近事件时间 |

#### 集成模型 RedBloodStats

来源：`redblood/record.go`（`redblood.RedBloodStats`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `summary` | RedBloodStatsSummary | 聚合统计 |
| `daily` | RedBloodDailyStats[] | 每日统计 |
| `routes` | RedBloodRouteStats[] | 路线明细 |
| `records` | RedBloodRecord[] | 事件原始记录 |

#### 集成模型 UserHotKeyShortcut

来源：`models/UserHotKeyShortcutModel.go`（`models.UserHotKeyShortcut`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 记录或资源编号 |
| `userKey` | string | 认证用户标识 |
| `name` | string | 名称 |
| `key` | string | 按键字符串 |
| `sort` | integer | 排序序号 |
| `createdAt` | string (RFC3339) | 创建时间 |
| `updatedAt` | string (RFC3339) | 更新时间 |

#### 集成模型 RedBloodRecord

来源：`models/RedBloodRecordModel.go`（`models.RedBloodRecord`）。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| `id` | integer | 记录或资源编号 |
| `event_time` | string (RFC3339) | 事件发生时间 |
| `group_name` | string | 配置组名称 |
| `route_name` | string | 路线名称 |
| `event_type` | string | heal或death |
| `source` | string | abgi或bgi |
| `heal_count` | integer | 回血次数 |
| `death_count` | integer | 死亡次数 |
| `created_at` | string (RFC3339) | 入库时间 |

#### 集成模型 UidInfo

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| UID | string | 账户目录名 |
| CDAwareAutoGather | object[] | 每项TextName:string为材料txt文件名，Detail:object[]为路线记录 |
| CDAwareAutoGather[].Detail[].FileName | string | 路线文件名 |
| CDAwareAutoGather[].Detail[].CDTime | string | YYYY-MM-DD HH:mm:ss冷却时间 |
| CDAwareAutoGather[].Detail[].CDExpired | boolean | CD是否已经到期 |

#### 集成模型 BatchActionResults

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| total | integer | 去重后的节点数 |
| successCount / failureCount / unknownCount / busyCount | integer | 对应成功、失败、结果未知、节点正在执行其他操作的数量 |
| summaryStatus | string | 全成功为success、全失败为failed，其余partial |
| results | ActionResult[] | 保留节点顺序的逐节点结果；HTTP200不表示每节点都成功 |

#### 集成模型 RepositorySubscriptionResult

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| id / category / path / subscriber / localPath | string | 订阅编号、分类、分类内路径、订阅者、User内安装相对路径 |
| createdAt | string RFC3339 | 订阅创建时间 |
| size | integer | 已下载总字节数 |
| downloaded | boolean | 成功时true |
| directory? | boolean | 目录分支为true，单文件不返回 |
| count? | integer | 目录分支下载文件数量 |
| preservedCount? | integer | 目录分支保留用户文件数量 |
| scriptName? / scriptVersion? | string | 目录分支脚本清单名称/版本（Pathing可为空） |

#### 集成接口调用示例

以下仅为说明性请求和响应，不代表已执行过真实接口联调；替换占位符后按实际环境使用。

```http
GET {baseUrl}/api/abgiObs/IsRecording
apiKey: <ABGI_API_KEY>

HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8

{"status":"success","msg":{"outputActive":true,"outputBytes":1024,"outputDuration":1000,"outputTimecode":"00:00:01.000"}}
```

```http
POST {baseUrl}/api/CDCollectionManagement/toggleRoutes
apiKey: <ABGI_API_KEY>
Content-Type: application/json

{"paths":["D:\\BetterGI\\User\\JsScript\\采集cd管理\\pathing\\材料A\\路线.json"],"disabled":true}
```

```http
POST {baseUrl}/api/abgiTeam/actions/one-long/start
apiKey: <LOCAL_ABGI_API_KEY>
Content-Type: application/json

{"connectionIds":["<CONNECTION_ID>"],"name":"每日一条龙"}
```


#### 集成模型 OBSRawRecordingStatus

来源：goobs v1.9.0 record.GetRecordStatusResponse（已核对本机模块缓存源码），不同于聚合Status中的RecordingState。全部字段可省略，未运行时msg可能为{}。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| outputActive? | boolean | 是否正在录制 |
| outputPaused? | boolean | 是否暂停 |
| outputBytes? | number | 录制累计字节数 |
| outputDuration? | number | 录制持续毫秒数 |
| outputTimecode? | string | 格式化时间码 |

#### 集成模型 OBSRawReplayStatus

来源：goobs v1.9.0 outputs.GetReplayBufferStatusResponse。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| outputActive? | boolean | 回放缓冲是否运行；false可省略，因此msg可能为{} |

#### 集成模型 OBSRawStreamStatus

来源：goobs v1.9.0 stream.GetStreamStatusResponse。全部字段有omitempty，零值可省略。

| 字段 | JSON 类型 | 含义 |
| --- | --- | --- |
| outputActive? | boolean | 推流是否运行 |
| outputBytes? | number | 推流累计字节数 |
| outputCongestion? | number | 拥塞指标 |
| outputDuration? | number | 推流持续毫秒数 |
| outputReconnecting? | boolean | 是否重连 |
| outputSkippedFrames? | number | 输出跳帧数 |
| outputTimecode? | string | 格式化时间码 |
| outputTotalFrames? | number | 输出总帧数 |

<a id="section-32"></a>

## 公共数据结构

以下表格是 JSON 合同，不是 Go 类型声明。字段大小写必须保留；`?` 表示响应可能省略，指针字段也可能为 null；整数不限定为32位，客户端应注意较大ID的精度。时间类型为 RFC3339 JSON 字符串，除非表格另行说明。返回字段存在不代表创建/更新时必填，必填与完整覆盖规则以各接口参数为准。

#### 数据结构 MaterialRecord

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Data` | string | 记录时间，常见 YYYY/M/D HH:mm:ss |
| `Cl` | string | 材料名 |
| `Num` | string | 数量文本（不是数值类型） |
| `Source?` | string | 可选，脚本数据源标识 |
| `Category?` | string | 可选，脚本页签分类 |

#### 数据结构 ChildNode

来源：`config/YuanShen.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `ID` | integer | 材料ID |
| `Name` | string | 材料名称 |
| `Icon` | string | 图标URL |
| `ParentID` | integer | 原分类ID |
| `ParentName` | string | 原分类名 |
| `Number` | integer | 库存数量 |

#### 数据结构 DogFoodRecord

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `FileName` | string | 记录文件名 |
| `Mark` | string | 线路及连续A摘要 |
| `ActivateTime` | string | 最近激活收尾路线时间文本 |
| `Detail` | string[] | 原始明细行 |

#### 数据结构 EarningsSeries

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `dates` | string[] | 时间轴 |
| `line` | string[] | 对应线路 |
| `dogExp` | integer[] | 圣遗物经验 |
| `mora` | integer[] | 摩拉数量 |

#### 数据结构 GroupAnalysis

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `GroupName` | string | 配置组名称 |
| `ArchiveDate` | string | 归档日期 YYYY-MM-DD，可为空 |
| `Segments` | ExecutionSegment[] | 执行分段列表 |
| `LogAnalysis2Json` | LogAnalysis2Json[] | 子任务日志分析 |
| `ErrorSummary` | map<string, integer> | 错误类型→次数 |
| `SumIncome` | map<string, integer> | 收入材料→数量 |

#### 数据结构 EfficiencyRoute

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `GroupName` | string | 配置组 |
| `RouteName` | string | 路线名称 |
| `StartTime` | string | 开始时间 |
| `EndTime` | string | 结束时间 |
| `Consuming` | string | 耗时文本 |
| `DurationSec` | integer | 耗时秒 |
| `Income` | map<string, integer> | 材料→数量 |
| `IncomeTotal` | integer | 收入数量总和 |
| `IncomePerMin` | number | 每分钟收入 |
| `ErrorTotal` | integer | 错误总数 |
| `SelectedLog` | string | 查询来源日志 |

#### 数据结构 MoraleRecord

来源：`models/models.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `id` | integer | 记录ID |
| `uid` | string | 游戏UID |
| `name` | string | 角色昵称 |
| `Time` | string | 记录时间（大小写敏感） |
| `morale` | integer | 摩拉收支数量 |
| `action` | string | 收入/支出行为 |
| `action_id` | integer | 米游社札记动作编号 |
| `record_type` | string | detail/day/month |
| `period` | string | YYYY-MM-DD 或 YYYY-MM |
| `detail_count` | integer | 汇总包含的原始记录数 |
| `mark` | string | 备注；汇总时为收益分布JSON文本 |

#### 数据结构 MoraleMonsterBook

来源：`models/morale_monster_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `scope` | string | 统计口径标识 |
| `total_count` | integer | 总击杀数 |
| `total_morale` | integer | 总摩拉 |
| `elite_count` | integer | 精英击杀数 |
| `elite_morale` | integer | 精英摩拉 |
| `small_count` | integer | 小怪击杀数 |
| `small_morale` | integer | 小怪摩拉 |
| `groups` | MoraleMonsterGroup[] | 按怪物大类汇总 |
| `tiers` | MoraleMonsterTier[] | 按单次摩拉档位汇总 |
| `updated_at` | string | 统计更新时间 |

#### 数据结构 MoraleIncomeBook

来源：`models/morale_income_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `scope` | string | 统计口径 |
| `target_date` | string | 目标日/月/年 |
| `total_count` | integer | 记录总次数 |
| `total_morale` | integer | 收益总摩拉 |
| `raw_total_morale` | integer | 原始总摩拉 |
| `classified_total_morale` | integer | 已分类总摩拉 |
| `unclassified_delta` | integer | 未分类差额 |
| `monster_count` | integer | 怪物次数 |
| `monster_morale` | integer | 怪物摩拉 |
| `elite_count` | integer | 精英次数 |
| `elite_morale` | integer | 精英摩拉 |
| `small_count` | integer | 小怪次数 |
| `small_morale` | integer | 小怪摩拉 |
| `chest_count` | integer | 宝箱次数 |
| `chest_morale` | integer | 宝箱摩拉 |
| `task_count` | integer | 任务次数 |
| `task_morale` | integer | 任务摩拉 |
| `other_count` | integer | 其他次数 |
| `other_morale` | integer | 其他摩拉 |
| `categories` | MoraleIncomeCategory[] | 按来源类别统计 |
| `actions` | MoraleIncomeAction[] | 按行为统计 |
| `groups` | MoraleMonsterGroup[] | 怪物分组 |
| `tiers` | MoraleMonsterTier[] | 怪物摩拉档位 |
| `updated_at` | string | 更新时间 |

#### 数据结构 MoraleUpdateResult

来源：`task/Rank.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `status` | string | 业务结果（如成功/跳过/失败），需配合message |
| `message` | string | 结果说明 |
| `started_at` | string | 本次开始时间 |
| `finished_at` | string | 本次结束时间 |
| `last_record_time` | string | 更新前记录时间 |
| `latest_record_time` | string | 本次最新记录时间 |
| `next_retry_at` | string | 可重试时间 |
| `remote_pages` | integer | 远端页数 |
| `remote_records` | integer | 远端记录数 |
| `candidate_records` | integer | 候选记录数 |
| `inserted_records` | integer | 新增记录数 |
| `skipped_records` | integer | 已存在/跳过记录数 |
| `ignored_old_records` | integer | 忽略的旧记录数 |
| `total_morale` | integer | 本次摩拉合计 |
| `compact_queued` | boolean | 是否排队整理汇总 |
| `interrupted` | boolean | 是否中断 |

#### 数据结构 ScriptHouseState

来源：`warehouse/new_script_house.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Name` | string | 脚本目录名 |
| `ChineseName` | string | 中文展示名 |
| `NowVersion` | string | 当前本地版本 |
| `NewVersion` | string | 远端版本 |
| `Mark` | string | 版本状态说明 |
| `LastUpdated` | string | 远端更新时间 |
| `Description` | string | 脚本简介 |

#### 数据结构 OneDragonRevision

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `config` | OneDragonConfig | 配置对象 |
| `revision` | string | 文件内容修订值，用于乐观并发校验 |
| `modifiedAt` | string | 修改时间字符串 |

#### 数据结构 OneDragonConfigSummary

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 名称 |
| `supported` | boolean | 是否支持当前编辑格式 |
| `format` | string | 识别到的文件格式 |
| `modifiedAt` | string | 修改时间字符串 |

#### 数据结构 OneDragonEditorOptions

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `builtInTasks` | string[] | 内置任务名 |
| `scriptGroups` | string[] | 本地配置组 |
| `combatStrategies` | string[] | 战斗策略 |
| `characters` | OneDragonCharacterOption[] | 角色天赋选项 |
| `domains` | OneDragonDomainOption[] | 秘境选项 |
| `bosses` | OneDragonBossOption[] | 首领选项 |
| `countries` | string[] | 可选国家 |
| `craftingCountries` | string[] | 合成台国家 |
| `adventurersCountries` | string[] | 冒险家协会国家 |
| `leyLineTypes` | string[] | 地脉类型 |
| `secretObjects` | string[] | 尘歌壶购买物品 |
| `sereniteaPotTypes` | string[] | 尘歌壶传送方式 |
| `completionActions` | string[] | 结束行为 |
| `sundayRewardValues` | string[] | 周日奖励选择值 |

#### 数据结构 AutoPickData

来源：`bgiStatus/pickUpList/AutoPick.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `lists` | AutoPickLists | 五份名单 |
| `settings` | AutoPickSettings | 拾取配置 |
| `betterGIRunning` | boolean | 当前BetterGI是否运行 |

#### 数据结构 AbgiConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `OneLong` | oneLong | 一条龙全局设置（不是单份BGI一条龙文件） |
| `BetterGIAddress` | string | BetterGI安装绝对路径 |
| `RepoUrl` | string | BetterGI脚本仓库地址 |
| `ABGIRepositoryURL?` | string | 独立仓库服务地址；查询时清空且omitempty通常省略 |
| `content` | string | 旧通知内容 |
| `BagStatistics` | string | 历史关注材料配置 |
| `post` | string | 监听端口文本，如 :8082 |
| `basePath` | string | 基础路径配置 |
| `Control` | Control | 运行控制 |
| `OneRemote` | OneRemote | 远程桌面配置 |
| `ScreenRecord` | ScreenRecord | OBS配置 |
| `BgiLog` | string | 日志路径配置 |
| `Notice` | Notice | 通知渠道和模板 |
| `Yunzai` | YunzaiConfig | 云崽桥接配置 |
| `CommandBot` | CommandBot | 指令机器人开关 |
| `UpdatePath` | UpdatePathing[] | 地图追踪更新配置 |
| `Account` | Account | 联机账号配置（并非本地登录账号） |
| `OnlineFallback` | OnlineFallback | 联机空房检测 |
| `AbgiAiConfig` | AbgiAiConfig | AI模型设置 |
| `Tunnel` | TunnelConfig | 内网穿透 |
| `WebDAV` | WebDAVConfig | User备份配置 |
| `Maa` | MaaConfig | Maa资源及控制器配置 |

#### 数据结构 CombatResult

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `mode` | string | 模式 |
| `needsClarification` | boolean | 是否需要补充说明 |
| `clarification?` | string | 需要补充的说明 |
| `summary` | string | 分析/生成摘要 |
| `warnings` | string[] | 警告列表 |
| `characterMappings?` | map<string, string> | 原名到官方角色名映射 |
| `actionExplanations?` | CombatStrategyActionExplanation[] | 返回逐动作设计依据。 |
| `teamAnalysis?` | combatStrategyPanelTeamAnalysis 或 null | 返回队伍共鸣和机制分析。 |
| `strategyAssessment?` | CombatStrategyAssessment 或 null | 返回面向用户的战斗结论。 |
| `strategy` | CombatDocument 或 null | 生成策略，无候选时为null |

#### 数据结构 CombatLogResult

来源：`abgiAi/CombatLogStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| （展开公共字段） | CombatResult | 嵌入字段直接展开在同一对象，不会多一层对象名 |
| `needsTeamSelection` | boolean | 指示前端先选择队伍。 |
| `teams?` | combatLogTeam[] | 返回具有有效战斗证据的候选队伍。 |
| `characters?` | string[] | 保留首次识别的角色顺序。 |
| `fileName?` | string | 返回含日期及扩展名的建议文件名。 |

#### 数据结构 Skill

来源：`models/SkillModel.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `id` | integer | 服务端技能ID |
| `name` | string | 技能名，创建时非空唯一 |
| `description` | string | 技能简介 |
| `type` | string | 来源类型 user/imported/system；创建默认user |
| `triggerType` | string | manual/keyword；创建默认manual |
| `keywords` | string | 关键词，逗号分隔 |
| `aiMode` | string | chat/tool；创建默认tool |
| `systemPrompt` | string | 系统提示词 |
| `userPromptTemplate` | string | 用户提示模板，支持 {{input}} |
| `enabled` | boolean | 是否启用，数据库创建默认true |
| `sort` | integer | 排序权重默认0 |
| `icon` | string | 展示图标 |
| `source` | string | 导入来源；普通update不更新此字段 |
| `createdAt` | string RFC3339 | 创建时间RFC3339 |
| `updatedAt` | string RFC3339 | 更新时间RFC3339 |

#### 数据结构 SkillRunLog

来源：`models/SkillModel.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `id` | integer | 日志ID |
| `skillId` | integer | 技能ID |
| `skillName` | string | 运行时技能名 |
| `input` | string | 运行输入 |
| `output` | string | 答案 |
| `mode` | string | 调用模式 |
| `duration` | integer | 耗时毫秒 |
| `success` | boolean | 调用成功标记 |
| `errorMsg` | string | 失败详情 |
| `createdAt` | string RFC3339 | 创建时间RFC3339 |

#### 数据结构 OneBotEvent

来源：`Notice/OneBot.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `time` | integer | Unix秒时间戳 |
| `self_id` | string 或 integer | 机器人ID（数字或字符串） |
| `post_type` | string | 事件类型，message才处理消息 |
| `message_type?` | string | private/group等消息类型 |
| `sub_type?` | string | 消息子类型 |
| `message_id?` | string 或 integer | 消息ID（数字或字符串） |
| `user_id?` | string 或 integer | 发送者ID（数字或字符串） |
| `group_id?` | string 或 integer | 群ID（数字或字符串） |
| `raw_message?` | string | 原始文本；非空时才分发 |
| `message?` | JSON value | OneBot消息原始JSON，字符串或消息段数组 |
| `sender?` | JSON value | {user_id:数字或字符串,nickname:string,card:string}发送者 |

#### 数据结构 ExecutionSegment

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `StartTime` | string | 开始日期时间 |
| `EndTime` | string | 结束日期时间 |
| `Consuming` | string | Go duration 格式耗时文本 |
| `Mola` | integer | 摩拉收益 |

#### 数据结构 LogAnalysis2Json

来源：`bgiStatus/BgiStatus.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `JsonName` | string | 子任务/路线名称 |
| `StartTime` | string | 开始日期时间 |
| `EndTime` | string | 结束日期时间 |
| `Income` | map<string, integer> | 收入名称→数量 |
| `Errors` | map<string, integer> | 错误名称→次数 |
| `ErrorsMark` | map<string, integer> | 错误标记→次数 |
| `ErrorTime` | string | 错误发生时间文本 |
| `Consuming` | string | 耗时文本 |
| `Mola` | integer | 摩拉收益 |

#### 数据结构 MoraleMonsterGroup

来源：`models/morale_monster_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `key` | string | 分类键 |
| `label` | string | 分类名称 |
| `count` | integer | 击杀数 |
| `morale` | integer | 摩拉总额 |
| `share` | number | 占比 |

#### 数据结构 MoraleMonsterTier

来源：`models/morale_monster_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `type` | string | 怪物分类 |
| `single_morale` | integer | 单只摩拉 |
| `amount_label` | string | 数量档位标签 |
| `count` | integer | 击杀数 |
| `morale` | integer | 摩拉总额 |
| `share` | number | 占比 |

#### 数据结构 MoraleIncomeCategory

来源：`models/morale_income_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `key` | string | 分类键 |
| `label` | string | 分类名称 |
| `count` | integer | 次数 |
| `morale` | integer | 摩拉总额 |
| `share` | number | 占比 |

#### 数据结构 MoraleIncomeAction

来源：`models/morale_income_book.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `action` | string | 行为名称 |
| `count` | integer | 次数 |
| `morale` | integer | 摩拉总额 |
| `share` | number | 占比 |

#### 数据结构 OneDragonConfig

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 名称 |
| `nextTaskId` | string | 原生继续执行任务ID |
| `tasks` | OneDragonTask[] | 任务列表 |
| `settings` | OneDragonSettings | 设置对象 |

#### 数据结构 OneDragonCharacterOption

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 名称 |
| `talentCopy` | string | 天赋秘境名 |
| `talentBook` | string | 天赋材料名称 |
| `talentTime` | integer[] | 开放星期编号列表 |
| `talentSchedules?` | OneDragonWeeklyTalentSchedule[] | 天赋材料开放计划 |

#### 数据结构 OneDragonDomainOption

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 名称 |
| `country` | string | 国家/地区 |
| `category?` | string | 分类 |
| `rewards?` | string[] | 奖励材料 |

#### 数据结构 OneDragonBossOption

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 名称 |
| `country` | string | 国家/地区 |

#### 数据结构 AutoPickLists

来源：`bgiStatus/pickUpList/AutoPick.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `blacklist` | string[] | 精确黑名单 |
| `fuzzyBlacklist` | string[] | 模糊黑名单 |
| `blacklistWhitelist` | string[] | 黑名单模式允许拾取名单 |
| `whitelistPick` | string[] | 白名单模式拾取名单 |
| `whitelistDoNotPick` | string[] | 白名单模式禁止拾取名单 |

#### 数据结构 AutoPickSettings

来源：`bgiStatus/pickUpList/AutoPick.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `enabled` | boolean | 自动拾取开关 |
| `mode` | string | Blacklist/Whitelist，空值归一为Whitelist |
| `blacklistModePickEnabled` | boolean | 黑名单模式拾取白名单开关 |
| `whitelistModeDoNotPickEnabled` | boolean | 白名单模式禁止拾取开关 |

#### 数据结构 oneLong

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `AutoUpdateJs` | boolean | 是否开启自动更新js |

#### 数据结构 Control

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `IsCloseYuanShen` | boolean | bgi关闭需要是否关闭原神 |
| `BgiCloseAction` | string | BGI关闭后未完成任务行为 |
| `BgiCloseScriptManagementType` | string | BGI关闭后工坊目标类型 |
| `BgiCloseScriptManagementID` | integer | BGI关闭后工坊目标编号 |
| `SendWeChatImage` | boolean | 是否开启每隔一小时发送截图 |
| `StartOpenBrowser` | boolean | 是否开启启动时打开浏览器 |
| `AutoStart` | boolean | 是否开启开机自启动 |
| `AutoStartArgs` | string | 开机自启动参数 |
| `AbgiScreen` | boolean | 是否开启bgi实时屏幕 |
| `OBSReplayBuffer` | boolean | 一条龙启动是否开启OBS重放缓冲 |
| `IsRedBlood` | boolean | 是否开启血条检测 |
| `RedBloodCount` | integer | 检测到红血，吃药次数 |
| `IsReviveIcon` | boolean | 红血检测时是否检测复活图标 |
| `IsQScreenshot` | boolean | 是否开启Q检测 |
| `IsMcp` | boolean | 是否开启MCP |

#### 数据结构 OneRemote

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `AllowedRemoteAddresses` | string[] | RDP允许的远程IP或CIDR |
| `MaxActiveSessions` | integer | RDP最大活动会话数 |
| `MaxDesktopMemoryMB` | integer | RDP桌面最大预留内存MB |
| `MaxReconnectAttempts` | integer | RDP最大重连次数 |
| `IsMonitor` | boolean | 是否开启1Remote监控 |
| `Mode` | string | 多用户后端: embedded或external |
| `LocalPort` | integer | 本机RDP端口 |
| `LogFilePath` | string | 外部1Remote程序路径 |

#### 数据结构 ScreenRecord

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `IsRecord` | boolean | 是否开启录屏 |
| `StartScreen` | string | OBS WebSocket 地址 |
| `EndScreen` | string | OBS WebSocket 密码 |
| `ObsSavePath` | string | OBS录屏保存路径 |

#### 数据结构 Notice

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Type` | string | 通知类型 |
| `Wechat` | string | 企业微信webhook地址 |
| `TGNotice` | TGNotice | TG通知配置 |
| `OneBot` | OneBot | OneBot配置 |
| `QQBot` | QQBot | 腾讯官方QQ机器人配置 |
| `FeiShu` | FeiShu | 飞书配置 |
| `DingTalk` | DingTalk | 钉钉配置 |
| `Template` | NoticeTemplate | 统一通知模板配置 |

#### 数据结构 YunzaiConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Enabled` | boolean | 是否启用云崽 桥接 |
| `Prefixes` | string[] | 转发给云崽的命令前缀 |
| `ReconnectMin` | integer | 最小重连间隔秒数 |
| `ReconnectMax` | integer | 最大重连间隔秒数 |

#### 数据结构 CommandBot

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `TgBOT` | boolean | 是否开启TG机器人 |
| `FeiShuBot` | boolean | 是否开启飞书机器人 |
| `QQBot` | boolean | 是否开启官方QQ机器人 |

#### 数据结构 UpdatePathing

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `name` | string | 配置组 |
| `folderName` | string | 地图追踪文件夹名称 |

#### 数据结构 Account

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Uid` | string | 账号UID |
| `Name` | string | 账号名称 |
| `IsMultiUser` | boolean | 是否是多用户 |
| `GouLangGroupName` | string | 狗粮联机配置组名称 |
| `SecretKey` | string | 加密密钥 |

#### 数据结构 OnlineFallback

来源：`config/OnlineFallback.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `enabled` | boolean | 是否在指定房间无人时下线并运行工坊脚本。 |
| `timeoutMinutes` | integer | 房间检测间隔分钟数，保留旧字段名以兼容已保存配置。 |
| `groupId` | integer | 需要定时检测人数的房间编号。 |
| `scriptId` | integer | 凯瑟琳工坊中的脚本编号。 |

#### 数据结构 AbgiAiConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `IsAbgiAi` | boolean | 是否开启bgiai |
| `ApiKey` | string | 密钥 |
| `BaseUrl` | string | 地址 |
| `Model` | string | 模型 |

#### 数据结构 TunnelConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `ServerAddr` | string | 穿透服务器地址 |
| `Username` | string | 穿透账号 |
| `Password` | string | 穿透密码 |
| `RemotePort` | string | 穿透远程端口 |
| `TunnelSecretKey` | string | 穿透密钥 |

#### 数据结构 WebDAVConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `URL` | string | WebDAV服务地址 |
| `Username` | string | WebDAV账号 |
| `Password` | string | WebDAV密码 |
| `RemotePath` | string | WebDAV远端目录 |

#### 数据结构 MaaConfig

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Enable` | boolean | 是否启用Maa |
| `LibDir` | string | MaaFramework动态库目录 |
| `ResourceDir` | string | Maa资源目录 |
| `UserDir` | string | Maa用户配置目录 |
| `Controller` | string | 控制器: win32或adb |
| `WindowName` | string | Win32目标窗口标题关键字 |
| `WindowClassName` | string | Win32目标窗口类名关键字 |
| `ProcessName` | string | Win32目标进程名关键字 |
| `AdbPath` | string | ADB路径 |
| `AdbAddress` | string | ADB设备地址 |
| `AgentPath` | string | MaaAgentBinary路径 |

#### 数据结构 CombatStrategyActionExplanation

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `actionName` | string | 记录对应 Actions 的名称。 |
| `character` | string | 记录动作绑定的角色。 |
| `reason` | string | 记录动作顺序和条件理由。 |
| `panelEvidence` | string[] | 记录使用的面板依据。 |

#### 数据结构 combatStrategyPanelTeamAnalysis

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `elementCounts` | map<string, integer> | 保存元素人数。 |
| `bonuses` | string[] | 保存当前队伍可获得的固定增益。 |
| `reactionTags` | string[] | 保存队伍说明中出现的反应标签。 |
| `specialHints` | string[] | 保存特殊角色轮转提示。 |
| `sources` | string[] | 保存队伍规则来源。 |
| `notes` | string[] | 保存资料缺失和版本假设。 |

#### 数据结构 CombatStrategyAssessment

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `cycleStatus` | string | 表示结构化轮转的可循环状态。 |
| `cycleSummary` | string | 用一句话说明轮转结论。 |
| `cycleRisks?` | string[] | 只列出会影响实际运行的循环风险。 |
| `rotationSummary?` | string | 用一句话说明运行时的优先出招路径。 |
| `rotationSteps?` | CombatStrategyRotationStep[] | 保存用户可读的实际出招步骤。 |
| `damageSummary` | string | 说明队伍整体输出能力及数字来源。 |
| `verifiedDamage?` | CombatStrategyVerifiedDamage[] | 保存本地公式已经验证的伤害行。 |
| `reactionSummary?` | string[] | 保存当前队伍可尝试的元素和特殊反应。 |
| `weaponContributions?` | CombatStrategyWeaponContribution[] | 保存武器对角色和队伍的实际帮助。 |
| `artifactContributions?` | CombatStrategyArtifactContribution[] | 保存圣遗物对角色和队伍的实际帮助。 |

#### 数据结构 CombatDocument

来源：`combatStrategy/universal_json.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Info` | CombatInfo | 策略元信息 |
| `Actions` | CombatAction[] | 调度动作列表 |

#### 数据结构 combatLogTeam

来源：`abgiAi/CombatLogParser.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `id` | string | 使用角色集合生成稳定队伍标识。 |
| `characters` | string[] | 保留第一次识别时的四角色顺序。 |
| `battleCount` | integer | 统计包含实际战斗证据的场次。 |

#### 数据结构 OneDragonTask

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `id` | string | 记录标识 |
| `name` | string | 名称 |
| `enabled` | boolean | 是否启用 |

#### 数据结构 OneDragonSettings

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `craftingBenchCountry` | string | 合成台所在国家 |
| `adventurersGuildCountry` | string | 冒险家协会国家 |
| `partyName` | string | 默认队伍名 |
| `domainName` | string | 默认秘境 |
| `weeklyDomainEnabled` | boolean | 按星期选择秘境 |
| `autoBossName` | string | 自动首领名 |
| `autoBossStrategyName` | string | 首领策略 |
| `autoBossTeamName` | string | 首领队伍 |
| `autoBossSpecifyRunCount` | boolean | 是否指定首领次数 |
| `autoBossRunCount` | integer | 首领次数 |
| `autoBossUseTransientResin` | boolean | 是否用须臾树脂 |
| `autoBossUseFragileResin` | boolean | 是否用脆弱树脂 |
| `autoBossReviveRetryCount` | integer | 复活重试次数 |
| `autoBossReturnToStatueAfterEachRound` | boolean | 每轮后是否回神像 |
| `autoBossRewardRecognitionEnabled` | boolean | 是否识别首领奖励 |
| `dailyRewardPartyName` | string | 领取每日奖励队伍 |
| `minResinToKeep` | integer | 保留树脂 |
| `sundayEverySelectedValue` | string | 周日普通秘境奖励选择 |
| `sundayWeeklySelectedValue` | string | 周日周本奖励选择 |
| `sereniteaPotTpType` | string | 尘歌壶传送方式 |
| `secretTreasureObjects` | string[] | 尘歌壶购买物品 |
| `leyLineOneDragonMode` | boolean | 地脉一条龙模式 |
| `leyLineRunMonday` | boolean | 周一是否跑地脉 |
| `leyLineRunTuesday` | boolean | 周二是否跑地脉 |
| `leyLineRunWednesday` | boolean | 周三是否跑地脉 |
| `leyLineRunThursday` | boolean | 周四是否跑地脉 |
| `leyLineRunFriday` | boolean | 周五是否跑地脉 |
| `leyLineRunSaturday` | boolean | 周六是否跑地脉 |
| `leyLineRunSunday` | boolean | 周日是否跑地脉 |
| `leyLineMondayType` | string | 周一地脉类型 |
| `leyLineMondayCountry` | string | 周一地脉国家 |
| `leyLineTuesdayType` | string | 周二地脉类型 |
| `leyLineTuesdayCountry` | string | 周二地脉国家 |
| `leyLineWednesdayType` | string | 周三地脉类型 |
| `leyLineWednesdayCountry` | string | 周三地脉国家 |
| `leyLineThursdayType` | string | 周四地脉类型 |
| `leyLineThursdayCountry` | string | 周四地脉国家 |
| `leyLineFridayType` | string | 周五地脉类型 |
| `leyLineFridayCountry` | string | 周五地脉国家 |
| `leyLineSaturdayType` | string | 周六地脉类型 |
| `leyLineSaturdayCountry` | string | 周六地脉国家 |
| `leyLineSundayType` | string | 周日地脉类型 |
| `leyLineSundayCountry` | string | 周日地脉国家 |
| `leyLineRunCount` | integer | 地脉次数 |
| `leyLineResinExhaustionMode` | boolean | 地脉清空树脂模式 |
| `leyLineOpenModeCountMin` | boolean | 地脉开启最小次数模式 |
| `mondayPartyName` | string | 周一队伍 |
| `mondayDomainName` | string | 周一秘境 |
| `mondaySelectedValue` | string | 周一奖励选择值 |
| `tuesdayPartyName` | string | 周二队伍 |
| `tuesdayDomainName` | string | 周二秘境 |
| `tuesdaySelectedValue` | string | 周二奖励选择值 |
| `wednesdayPartyName` | string | 周三队伍 |
| `wednesdayDomainName` | string | 周三秘境 |
| `wednesdaySelectedValue` | string | 周三奖励选择值 |
| `thursdayPartyName` | string | 周四队伍 |
| `thursdayDomainName` | string | 周四秘境 |
| `thursdaySelectedValue` | string | 周四奖励选择值 |
| `fridayPartyName` | string | 周五队伍 |
| `fridayDomainName` | string | 周五秘境 |
| `fridaySelectedValue` | string | 周五奖励选择值 |
| `saturdayPartyName` | string | 周六队伍 |
| `saturdayDomainName` | string | 周六秘境 |
| `saturdaySelectedValue` | string | 周六奖励选择值 |
| `sundayPartyName` | string | 周日队伍 |
| `sundayDomainName` | string | 周日秘境 |
| `sundaySelectedValue` | string | 周日奖励选择值 |
| `completionAction` | string | 结束后的行为（包括关机等高风险动作） |

#### 数据结构 OneDragonWeeklyTalentSchedule

来源：`bgiStatus/OneDragon.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `talentBook` | string | 天赋材料名称 |
| `talentTime` | integer[] | 开放星期编号列表 |

#### 数据结构 TGNotice

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `TGToken` | string | TG机器人token |
| `ChatID` | integer | TG聊天ID |
| `Proxy` | string | TG代理 |

#### 数据结构 OneBot

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `APIBase` | string | OneBot API 地址，例如 http://127.0.0.1:3000 |
| `Token` | string | 可选 Token，用于鉴权 |
| `QQNum` | string | 机器人QQ号或腾讯官方机器人UUID |
| `GroupNum` | string | QQ群号或腾讯官方群UUID |
| `Secret` | string | 上报消息的签名密钥 |

#### 数据结构 QQBot

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `AppID` | string | 官方QQ机器人AppID |
| `AppSecret` | string | 官方QQ机器人AppSecret |
| `ClientSecret?` | string | 兼容旧配置的官方QQ机器人ClientSecret |
| `UserOpenID` | string | 私聊接收者OpenID |
| `GroupOpenID` | string | 群聊接收群OpenID |
| `GatewayURL` | string | 官方QQ机器人网关地址，可选 |
| `APIBase` | string | 官方QQ机器人接口地址，可选 |
| `Intents?` | integer | 官方QQ机器人事件订阅掩码，可选 |

#### 数据结构 FeiShu

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `FeiShuWebhookURL` | string | 飞书webhook地址 |
| `AppID` | string | 飞书AppID |
| `AppSecret` | string | 飞书AppSecret |

#### 数据结构 DingTalk

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `WebhookURL` | string | 钉钉机器人webhook地址 |
| `Secret` | string | 钉钉机器人加签密钥 |

#### 数据结构 NoticeTemplate

来源：`config/Config.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Enabled` | boolean | 是否启用统一通知模板 |
| `Type` | string | 模板类型:text或image |
| `Markdown` | string | Markdown模板内容 |
| `BackgroundImage` | string | 图片模板背景图相对路径 |
| `TextColor` | string | 图片模板文字颜色 |
| `FontSize` | integer | 图片模板文字字号，0表示默认 |
| `TextPosition` | string | 图片模板文字位置 |
| `TextOffsetX` | integer | 图片模板文字水平偏移 |
| `TextOffsetY` | integer | 图片模板文字垂直偏移 |

#### 数据结构 CombatStrategyRotationStep

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `order` | integer | 保存按 Index 和优先级排序后的步骤编号。 |
| `character` | string | 保存实际执行动作的角色。 |
| `actionName` | string | 保存与 Actions.Name 对应的动作名称。 |
| `summary?` | string | 保存面向用户的简短出招和衔接说明。 |
| `trigger` | string | 将条件和 MorePriorities 转换为触发时机。 |
| `move` | string | 将 BetterGI 指令转换为中文出招描述。 |
| `result` | string | 说明出招后产生的技能或状态结果。 |
| `effects?` | string[] | 保存队伍共鸣、反应和技能效果。 |
| `equipmentHelp?` | string[] | 保存当前武器和圣遗物对本步骤的帮助。 |
| `teamSupport?` | string[] | 保存前后角色之间的技能、增益、治疗和装备衔接说明。 |
| `damageReference?` | CombatStrategyVerifiedDamage[] | 仅引用本地已经验证的伤害结果。 |

#### 数据结构 CombatStrategyVerifiedDamage

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `character` | string | 保存角色名称。 |
| `skill` | string | 保存技能或伤害段名称。 |
| `damage?` | string | 保存暴击或单次伤害文本。 |
| `expectedDamage?` | string | 保存期望伤害文本。 |
| `unit?` | string | 保存伤害单位。 |

#### 数据结构 CombatStrategyWeaponContribution

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `character` | string | 保存装备角色名称。 |
| `weapon` | string | 保存武器名称。 |
| `refinement?` | integer | 保存精炼等级。 |
| `passiveEffect?` | string | 保存当前精炼对应的被动文本。 |
| `teamValue` | string | 说明武器对输出、充能或生存的作用。 |

#### 数据结构 CombatStrategyArtifactContribution

来源：`abgiAi/CombatStrategyAgent.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `character` | string | 保存装备角色名称。 |
| `sets?` | string[] | 保存套装名称。 |
| `setEffects?` | string[] | 保存套装效果文本。 |
| `mainStats?` | string[] | 保存主词条摘要。 |
| `subStats?` | string[] | 保存副词条摘要。 |
| `teamValue` | string | 说明圣遗物对输出、充能或循环的作用。 |

#### 数据结构 CombatInfo

来源：`combatStrategy/universal_json.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Name` | string | 名称 |
| `Author` | string | 作者 |
| `Description?` | string | 保留策略说明，供编辑器和 BetterGI 文档使用。 |
| `Config` | map<string, JSON value> | 配置对象 |
| `PreActions` | string[] | 进入主循环前动作 |

#### 数据结构 CombatAction

来源：`combatStrategy/universal_json.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Name` | string | 名称 |
| `Character` | string | 角色名 |
| `Action` | string | BetterGI动作指令 |
| `Condition` | CombatCondition | 调度条件 |
| `Index` | integer | 动作调度索引 |
| `EnsureCast` | boolean | 是否确认技能施放 |
| `redArrowAim?` | boolean | 保留红箭头瞄准辅助字段。 |
| `MorePriorities` | CombatMorePriority[] | 额外条件优先级 |

#### 数据结构 CombatCondition

来源：`combatStrategy/universal_json.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Expression` | string | 条件表达式 |

#### 数据结构 CombatMorePriority

来源：`combatStrategy/universal_json.go`。

| 字段 | JSON 类型 | 含义 |
|---|---|---|
| `Expression` | string | 条件表达式 |
| `Priority` | integer | 优先级（数值小优先） |

#### 数据结构 BagViewPreference

| 字段 | 类型 | 含义 |
|---|---|---|
| sortMode | string | recent/change/name/custom，默认custom |
| materialNames | string[] | 自定义排序材料名 |
| followedMaterialKeys | string[] | 关注材料的来源及名称稳定键 |

#### 数据结构 SkillExportItem

`{name:string,description:string,icon:string,triggerType:string,keywords:string,aiMode:string,systemPrompt:string,userPromptTemplate:string}`；分别为技能名、说明、图标、触发方式、逗号分隔关键词、AI模式、系统提示和用户提示模板。单条导出另加 version="1.0"，批量导出放在 skills 数组。

#### 全局配置与一条龙的使用边界

- AbgiConfig 是 ABGI main.json 的快照，OneDragonConfig 才是单个 BetterGI 一条龙编辑模型；二者字段不能混用。
- GET /api/config 清空 AI ApiKey、仓库服务地址、穿透 Password/TunnelSecretKey、WebDAV Password、OBS EndScreen、微信Webhook、TGToken、飞书Webhook/AppSecret、OneBot Token/QQNum/Secret、QQBot AppSecret/ClientSecret、钉钉Webhook/Secret；WebDAV URL 也会删除内嵌userinfo。带omitempty的清空字段可能省略。Account.SecretKey等未在清理列表内的值仍可能回显：不要公开完整配置响应。
- POST /api/saveConfig 对这些约定敏感字段的空值通常沿用旧值，不代表清空；WebDAV/QQBot/通知Template/Maa等有旧客户端字段省略兼容，不能推广成对所有字段的PATCH。传入新QQBot AppSecret会清空旧ClientSecret别名；关闭QQBot会关闭云崽桥接；Yunzai.URL/Authorization/SelfID 不参与JSON读写。Maa未提交整组保留，提交时LibDir/ResourceDir/UserDir空值保留当前目录。
- OneDragonSettings 的任务、队伍和枚举取自 options；新建返回的配置可作为保存模板。revision 不传/空值时当前实现可能跳过版本比较，因此客户端应始终带上最新 revision 防覆盖；force仅允许运行中确认，不替代并发校验。
- AutoPick名单会按换行拆分、去空白、去空和首次顺序去重，BGI运行时保存成功不表示其内存缓存立即生效，可能需重启或切换拾取模式。

<a id="section-33"></a>

## 静态资源、OPTIONS 与回退

本节不计入401个业务端点，静态路径中的`*filepath`可含目录。

| 方法 | URL | 参数与认证 | 返回与用途 |
|---|---|---|---|
| GET、HEAD | `{baseUrl}/img/*filepath` | Path filepath:string，相对img目录；公开 | 按文件类型返回图片/其他静态文件，HEAD无body；缺失通常404；包括通知背景图 |
| GET、HEAD | `{baseUrl}/assets/*filepath` | Path filepath:string，嵌入前端资源路径；公开 | 读取web/dist嵌入资源；Content-Type按扩展名，HEAD无body；非业务JSON接口 |
| OPTIONS | `{baseUrl}/*` | 跨域预检，不需要业务参数；全局中间件先行处理 | 204空body，包含当前CORS头；不代表后续跨域方法/自定义头必定允许 |
| 未匹配请求 | `{baseUrl}/api...` | 无对应已注册路由；不进入统一needAuth组 | 404 application/json `{"error":"API not found"}` |
| SPA/文件回退 | `{baseUrl}/...` | 非/api未匹配路径，公开 | 尝试嵌入文件，缺失回index.html；读取失败500 JSON `{error:string}`；此机制不是任意后端API |

ZZZ初始化失败的 `ANY /api/zzz-one-dragon/*path` 见绝区零章节：在普通认证之后返回503 JSON；与正常8条ZZZ接口互斥。Gin的Any展开GET/POST/PUT/PATCH/HEAD/OPTIONS/DELETE/CONNECT/TRACE，但OPTIONS已被全局中间件提前204处理。

<a id="section-34"></a>

## 完整性与验证说明

- 入口为 `menu/routes_api.go`，覆盖runtime/statistics/config/tasks/integrations/public及所有被委托注册的业务模块；MCP另从 `menu/menu.go` 条件挂载。只有本机实际服务端接口进入清单，出站 `/api/abgiWs/...`、上游仓库 `/api/repository/...`、前端路由、注释代码、测试内路由不计入。
- 使用独立Go标准库 `go/parser`/AST程序读取源码，不import任何项目包，不启动应用；按Group前缀拼接，记录处理器和源码行，按HTTP方法与完整路径逐项与本文端点标题对照。源码402条注册调用=401正常显式端点+1互斥ANY兜底；静态挂载/OPTIONS/NoRoute单列。
- 静态核验结论：402条注册均有且仅有一个文档详情条目；缺失0、重复0、额外0；正文逐项检查用途、URL、认证、请求格式、参数、返回及错误；索引、章节锚点、相对源码引用和JSON示例做离线检查。
- 不通过真实写接口“试错”，也不运行项目包测试：项目初始化可能创建数据库、改写配置、启动任务/机器人。本文的字段、限制和错误来自源码检查；Windows桌面、RDP、OBS、Maa、第三方网络和实际文件环境仍需调用者在隔离环境按需验证。
- 代码解析可证实注册覆盖率，不等价于完整协议行为测试。透明代理JSON、Maa开放原生Pipeline、外部脚本动态设置和具体运行状态依赖对应服务/SDK版本，文中明确为动态时不承诺固定业务字段。
- 所有临时生成/检查程序和中间片段在核验后清理；不删除项目原有测试、不执行mvn、不改变鉴权及业务实现。后续新增/删除路由时，应同步维护索引、接口详情和公共模型。
