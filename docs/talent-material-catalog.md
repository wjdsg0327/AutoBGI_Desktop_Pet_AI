# 天赋材料目录的来源与接线边界

核对日期：2026-10-28。实现位于宿主 `TalentPlanService/catalog*.go`，桌宠没有材料 ID 表，也没有新增目录上传接口。

当前目录只覆盖「自由」的教导、指引、哲学三阶。`mapping-ready` 只表示这三个完整名称已与宿主注入的官方计算器材料身份精确对应；它不证明库存属于某个 UID、不证明库存数量、新鲜度、资源尚未被其他角色占用，也不授权保存配置、推进队列、发送完成事件或启动游戏。

未注入可信官方计算器生产者时，`NewTalentCatalogProvider(nil).Load(ctx)` 返回 `ErrTalentCatalogUnavailable`，目录中不内置猜测的游戏材料 ID。目前生产启动入口仍不接线，不能因为新增目录组件而解除原有来源门槛。

## 公共配方证据

米游社百科由平台托管、社区编辑；其资料版本与官方计算器返回的游戏材料身份是不同证据。组件使用下列百科页面明确记载的获得方式与炼金输入数量，不把百科内容编号当作游戏材料 ID。

| 材料完整名称 | 百科 content_id | 内容版本 | 修改时刻 | 本轮核对的最小事实 |
| --- | --- | --- | --- | --- |
| 「自由」的教导 | 819 | 16 | 2023-04-27 16:18:12 | 获得方式指向忘却之峡，周一/四/日 |
| 「自由」的指引 | 820 | 17 | 2023-09-12 10:08:12 | 炼金合成输入「自由」的教导，数量 3；获得方式为忘却之峡，周一/四/日 |
| 「自由」的哲学 | 821 | 14 | 2023-09-12 10:08:31 | 炼金合成输入「自由」的指引，数量 3；获得方式为忘却之峡，周一/四/日 |

三个原始公开响应地址分别为：

- `https://api-static.mihoyo.com/common/blackboard/ys_obc/v1/content/info?app_sn=ys_obc&content_id=819`
- `https://api-static.mihoyo.com/common/blackboard/ys_obc/v1/content/info?app_sn=ys_obc&content_id=820`
- `https://api-static.mihoyo.com/common/blackboard/ys_obc/v1/content/info?app_sn=ys_obc&content_id=821`

提取位置为 `data.content.contents[].text` 中 `data-data` 的 URL 解码 JSON：`tmplKey=material` 的 `data.proceed` 给出开放日和秘境链接；`tmplKey=illustration`、`name_=炼金合成` 的记录给出相邻材料完整名称、链接及 `extend_0_` 中的整数 `3`。这里只保存必要事实、内容版本和原响应字节 SHA-256，不打包页面的角色列表、图片、账号资料或 Cookie。

原始响应字节摘要已固定在 `catalog_evidence.go`：

| 来源 | SHA-256 |
| --- | --- |
| wiki-819 | `e733584f919508c71664544e543f2eb9e3ad6b38ddc6670a124641b3b6ff063e` |
| wiki-820 | `d948745a4f6f0ffc2ad8c3a9762233ba00742a1e5f05598e90bfa53a337e3646` |
| wiki-821 | `ab443e49fee1e7918af87d676179cd70e8b07005dce62fdc938020d9be931b01` |

这些未带版本参数的公开地址日后可能变化。运行时不重新下载或静默替换这些事实；维护者若更新证据，需要重新核对来源、摘要和目录事实版本。摘要提供追溯和变化检测，不能单独证明一次网络响应的来源真实性。

## BetterGI 适配证据

固定上游提交为 `94c1de65fa5c525496a776ad63087162626dfbe7`，完整 URL、提交和原始字节摘要保存在每份 `CatalogSourceProof`：

| 上游文件 | 本轮核对的边界 |
| --- | --- |
| `GameTask/AutoTrackPath/Assets/tp.json` | 传送点 `id=44` 的名称是忘却之峡，类型为 `MasteryDomain`；这个编号也不是游戏材料 ID |
| `GameTask/AutoDomain/AutoDomainTask.cs` | 周日或识别到限时全开时，`SundaySelectedValue` 的 1/2/3 点击游戏界面的三组选项；0 不执行该切换 |
| `Core/Config/OneDragonFlowConfig.cs` | 每日配置使用 `DomainName` 和 `SundayEverySelectedValue`；按服务器时间 04:00 作为业务日边界 |
| `Helpers/ServerTimeHelper.cs` | 使用配置的游戏服务器 UTC 偏移，不能使用桌宠电脑本地日期替代 |

本次没有找到能证明「自由」对应哪一个周日界面序号的足够证据。`tp.json` 的忘却之峡奖励次序是抗争、自由、诗文，不能把它当成周日界面次序。`TrainingGuideEntryCatalog.cs` 还明确说明其阅读次序不能用于推断实际入口。因此 `CatalogDomain.SundaySelectedValue` 保持 `nil`。

`ResolveDaily` 只在宿主明确提供服务器偏移、确认 `OpeningMode=regular`，并且实际业务日为周一或周四时，给出忘却之峡和 `SundayEverySelectedValue="0"` 的字段建议。周日、限时全开、未知开放模式、缺少服务器偏移均返回不可用；普通周二/三/五/六返回 `ErrTalentCatalogClosed`。该方法不会修改任何配置。

## 官方计算器材料身份

现有 `CharacterPlanService/CalculatorRemote.go` 的官方 `/v2/compute` 响应包含 `id/name/num`。目录要求宿主生产者提供同一次请求的原始无凭据正文、原始响应正文、实际接收时刻和固定来源地址：

```go
// 只能由服务器内部已核实的官方计算器请求链实现此来源；不能接收 HTTP 上传或扫描库存。
source := TalentPlanService.CalculatorCatalogSource(func(ctx context.Context) ([]TalentPlanService.CalculatorCatalogEvidence, error) {
    // 从已证明来源的官方计算器请求生产者取得原始请求/响应；本例不提供替代实现。
    return trustedCalculatorEvidence(ctx)
})
provider := TalentPlanService.NewTalentCatalogProvider(source)

// 成功只代表「自由」三阶映射覆盖，仍须分别核对真实库存、持久资源预留和启动事务。
catalog, err := provider.Load(ctx)
```

`trustedCalculatorEvidence` 是未来宿主接线处的占位名，当前代码未定义或接线它。不得通过复制请求中当前配置 UID、把来源地址填成官方 URL、重新计算一个摘要，或者把 `SourceURL` 字符串检查当作网络来源真实性证明。

材料身份只能从成功的原始官方响应中读取。三个完整名称逐字相等合并，任何一阶缺席均不可用；名称引号、空格或别名不自动修正；同名异 ID、同 ID 异名、重复 JSON 键、大小写键别名、尾随正文、负数量、空数量、非法 UTF-8、异常嵌套和容量超限均拒绝。

`avatar_skill_consume` 必须存在且为数组，角色升级消耗若存在也参加身份冲突检查。`num` 只用于拒绝异常材料响应，绝不作为拥有数量。

目录来源同时保留无凭据请求 SHA-256、原响应 SHA-256 和实际接收时刻。目录 `Version` 绑定材料身份、固定事实版本和所有来源摘要；同一请求与响应只是重新读取时，不产生新的静态映射版本。原始正文不会进入公开目录快照。

## 输出与覆盖限制

- `TalentCatalogProvider.Load(ctx)` 返回私有目录对象；`Snapshot()` 返回深拷贝，外部修改不会改变后续查询。空指针或外部构造的零值目录均保持 `unavailable`，不能因字段尚未初始化而声明映射覆盖。
- `FindMaterial(gameMaterialID)` 只接受游戏材料 ID，返回完整三阶家族和独立的 3:1 关系。
- `CatalogCrafting` 故意不提供 `Verified` 标记，也不会转换为 `TalentBookRecipe` 或修改 `EvaluationInput.ProviderVerified`。
- 其他天赋书、武器素材、其他材料与未确认的周日序号均不在本次覆盖范围；不能依据本次三个名称或数字规律扩展。
- 目录对跨启动库存复用、真实消费确认、持久资源预留以及需求快照刷新没有授权或替代作用。上述条件仍由宿主执行链分别保证。

## 隔离验证

验证只复制本组件的三个标准库源文件到本次专属临时目录，使用合成官方格式请求与响应运行正常及 race 验证，不导入现有 `config`、`menu`、`task`、数据库或游戏控制包。合成材料 ID 只用于测试，不作为真实官方材料表交付。

覆盖精确三阶合并、缺阶、名称与 ID 冲突、JSON 歧义、来源错误、错误脱敏、取消、有界输入、来源摘要与版本稳定性、深拷贝、04:00 边界、服务器偏移、周日和限时全开拒绝。测试源码在验证完成后只尝试清理一次；验证摘要另存为本次目录中的 JSON。当前文档不宣称匿名官方计算接口、真实账号、真实库存、生产缓存、游戏或端到端培养执行验证通过。

本轮正常与 race 验证各通过 11 个顶层用例、55 个子用例，没有失败或 race 告警。超出 JSON 时间范围的年份与时区两个回归用例，先在旧隔离副本上复现失败，修复后通过；来源时间先校验，公开快照使用显式深拷贝。源文件与隔离副本的字节摘要一致；依赖图只有标准库及本次隔离包。宿主 `go test -c ./TalentPlanService` 编译成功，生成的宿主测试程序未执行。验证记录为 `.validation/catalog-domain-6971f3891b154b1285146dfc795ff32d/catalog-validation.json`；该目录的清理结果以记录中的 `cleanup` 字段为准。

后续零值审查发现，外部可以构造 `new(TalentCatalog)`；此前的快照、零编号查询及诊断会将它当作已完成目录合并，合法周一的日程查询也没有首先拒绝未初始化目录。现以只有完整编译成功后才生成的私有版本作为初始化条件，`Snapshot`、`FindMaterial` 和 `String` 统一拒绝空指针与零值，`ResolveDaily` 从材料查询传播相同的不可用结果。正常工厂目录的快照、材料查询、诊断和日程保持原有结果，正常目录查询材料编号 `0` 仍返回不支持。

零值修复使用全新隔离目录 `.validation/catalog-zero-9882dc3eea39495b8f7e8370563342c3`，只读复制首次验证测试，并增加空指针、零值四个入口和正常工厂四个入口的边界验证。旧源码首先复现四个零值叶子用例失败；最终普通与 race 全量回归各通过 13 个顶层用例、69 个子用例，没有失败或 race 告警，独立包 `go vet` 通过。新记录保留首次验证报告与旧源码摘要，并绑定最终三个生产源文件、隔离副本和新回归证据。首次被自动审批拒绝清理的五个文件完全保留，本轮不会通过改名、移动、覆盖或删除父目录改变它们；新目录仅对自身临时文件尝试一次精确清理，以新记录的 `cleanup` 结果为准。
