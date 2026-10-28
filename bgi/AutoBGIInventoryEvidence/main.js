// 入口只等待完整采集流程，具体校验和失败回执由任务模块统一处理。
import { collectInventory } from "./src/modules/collector.js";

// 不吞掉任务失败，让调用宿主继续按失败处理本次脚本执行。
await collectInventory();
