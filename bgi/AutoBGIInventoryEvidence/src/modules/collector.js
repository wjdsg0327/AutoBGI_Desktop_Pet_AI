import { assertActive, MAX_BYTES, readItems, readRequest, REQUEST_PATH } from "./contract.js";
import { utf8Length } from "../utils/json.js";
import { failReceipt, newReceipt, report, writeReceipt } from "./receipt.js";

// 只接受实际宿主提供的采集和取消能力，不用默认值伪装缺失接口。
function cancellationToken() {
    if (typeof genshin === "undefined" || typeof genshin.returnMainUi !== "function" ||
        typeof genshin.uid !== "function" || typeof dispatcher === "undefined" ||
        typeof dispatcher.runTask !== "function" ||
        typeof dispatcher.getLinkedCancellationToken !== "function" ||
        typeof SoloTask !== "function" || typeof sleep !== "function") {
        throw new Error("BetterGI 缺少所需采集接口");
    }
    const cancellation = dispatcher.getLinkedCancellationToken();
    if (!cancellation || typeof cancellation.isCancellationRequested !== "boolean") {
        throw new Error("无法取得有效取消令牌");
    }
    return cancellation;
}

// 异步读取实际画面 UID；失败值和非安全整数不能当作当前账号。
async function observedUID(request, cancellation) {
    assertActive(request, cancellation);
    await genshin.returnMainUi();
    await sleep(300);
    assertActive(request, cancellation);
    const uid = await genshin.uid();
    assertActive(request, cancellation);
    if (!Number.isSafeInteger(uid) || uid <= 0) throw new Error("无法识别当前游戏 UID");
    return String(uid);
}

// 按固定请求执行一次扫描；读取现在不会改变请求时间或原始采集起点。
export async function collectInventory() {
    const receipt = newReceipt();
    try {
        // 最先覆盖 started，随后任何崩溃都不能复用上次 completed。
        writeReceipt(receipt);
        const raw = file.readTextSync(REQUEST_PATH);
        const request = readRequest(raw);
        receipt.requestId = request.requestId;
        receipt.requestDigest = request.requestDigest;
        writeReceipt(receipt);
        const cancellation = cancellationToken();
        assertActive(request, cancellation);

        // 在触碰游戏前检查最坏大小，避免扫描结束才发现无法保存未知数量回执。
        const worst = { ...receipt, status: "completed", finishedAt: new Date().toISOString(),
            observedUIDBefore: request.role.uid, observedUIDAfter: request.role.uid,
            items: request.materials.map((material) => ({ ...material, count: null,
                known: false, reason: "recognition-failed" })) };
        utf8Length(JSON.stringify(worst), MAX_BYTES);
        report("info", "AutoBGI 背包观察开始，请勿切换账号");

        receipt.observedUIDBefore = await observedUID(request, cancellation);
        if (receipt.observedUIDBefore !== request.role.uid) throw new Error("采集前 UID 与请求不一致");
        // 调用现有识别器，不启用按物品排序提前结束，也不把未返回材料补成零。
        const result = request.materials.length === 0 ? {} : await dispatcher.runTask(
            new SoloTask("CountInventoryItem", {
                itemNames: request.materials.map((material) => material.name),
                stopByItemSort: false,
            }),
        );
        assertActive(request, cancellation);
        receipt.items = readItems(request, result);
        receipt.observedUIDAfter = await observedUID(request, cancellation);
        if (receipt.observedUIDAfter !== request.role.uid ||
            receipt.observedUIDAfter !== receipt.observedUIDBefore) {
            throw new Error("采集后 UID 与请求或采集前身份不一致");
        }
        // 请求被替换或过期时拒绝完成，宿主必须重新创建本次请求。
        if (file.readTextSync(REQUEST_PATH) !== raw) throw new Error("采集期间请求发生变化");
        assertActive(request, cancellation);
        receipt.status = "completed";
        receipt.finishedAt = new Date().toISOString();
        writeReceipt(receipt);
        report("info", "AutoBGI 背包观察已保存，未知数量仍保留未知");
        return receipt;
    } catch (error) {
        // 失败后清除所有部分数量；文件系统完全拒绝写删时明确保留失败事实给宿主。
        const invalidated = failReceipt(receipt);
        const message = error && error.message ? String(error.message) : "未知采集错误";
        report("error", "AutoBGI 背包观察失败：" + message);
        if (!invalidated) report("error", "回执无法更新或移除，宿主必须拒绝本次采集");
        throw error;
    }
}
