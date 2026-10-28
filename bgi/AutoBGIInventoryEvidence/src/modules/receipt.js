import { MAX_BYTES, RECEIPT_PATH, SCRIPT_VERSION } from "./contract.js";
import { utf8Length } from "../utils/json.js";

// 回执仅描述本次观察，不添加区服观察、可信标志或防伪造承诺。
export function newReceipt() {
    return {
        schemaVersion: 1, requestId: "", requestDigest: "", scriptVersion: SCRIPT_VERSION,
        status: "started", startedAt: new Date().toISOString(), finishedAt: "",
        observedUIDBefore: "", observedUIDAfter: "", items: [],
    };
}

// 文件接口返回 false 时不会抛异常，必须检查返回值并读回完全相同的正文。
export function writeReceipt(receipt) {
    if (typeof file === "undefined" || typeof file.writeTextSync !== "function" ||
        typeof file.readTextSync !== "function") throw new Error("缺少回执读写接口");
    const raw = JSON.stringify(receipt);
    utf8Length(raw, MAX_BYTES);
    if (file.writeTextSync(RECEIPT_PATH, raw, false) !== true ||
        file.readTextSync(RECEIPT_PATH) !== raw) throw new Error("回执写入或读回核对失败");
}

// 只处理本次固定回执的普通文件，绝不递归删除目录或尝试其他路径。
export function removeReceiptFile() {
    try {
        if (typeof file !== "undefined" && typeof file.isFile === "function" &&
            typeof file.deletePathSync === "function" && file.isFile(RECEIPT_PATH) === true) {
            return file.deletePathSync(RECEIPT_PATH) === true && file.isFile(RECEIPT_PATH) === false;
        }
    } catch (error) {
        // 宿主连固定文件也拒绝处理时，明确返回失败而不是吞掉原始采集错误。
    }
    return false;
}

// 失败先清空所有数量再落盘；无法写入时尝试移除本次回执，不能保留旧的 completed。
export function failReceipt(receipt) {
    receipt.status = "failed";
    receipt.finishedAt = new Date().toISOString();
    receipt.items = [];
    try {
        writeReceipt(receipt);
        return true;
    } catch (error) {
        return removeReceiptFile();
    }
}

// 通知和日志不影响回执状态；诊断接口不可用时仍由调用方收到任务异常。
export function report(level, message) {
    try {
        if (typeof log !== "undefined" && typeof log[level] === "function") log[level](message);
    } catch (error) {
        // 日志接口失败不能覆盖实际采集错误或将任务改判为成功。
    }
    try {
        if (typeof notification !== "undefined") {
            if (level === "error" && typeof notification.error === "function") notification.error(message);
            else if (typeof notification.send === "function") notification.send(message);
        }
    } catch (error) {
        // 通知只是提示，不参与材料与身份判定。
    }
}
