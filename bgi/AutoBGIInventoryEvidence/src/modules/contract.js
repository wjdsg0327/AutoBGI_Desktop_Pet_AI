import { exactKeys, parseUniqueJSON, utf8Length } from "../utils/json.js";

// 路径固定在脚本沙箱内，任何输入字段都不参与文件路径拼接。
export const REQUEST_PATH = "local/request.json";
export const RECEIPT_PATH = "local/receipt.json";
export const MAX_BYTES = 512 * 1024;
export const SCRIPT_VERSION = "1.0.0";
const HEX = /^[0-9a-f]{64}$/;
const UINT64_MAX = "18446744073709551615";
// ClearScript 7.4.5 的 ExpandoObject 会枚举这四个 .NET 方法，只能把函数值视为宿主元数据。
const HOST_METADATA_METHODS = new Set(["GetType", "ToString", "Equals", "GetHashCode"]);
const HOST_METADATA_NAMES = new Set(Array.from(HOST_METADATA_METHODS, (name) => name.toLowerCase()));

// 正则不接受数组或对象的隐式字符串转换，所有摘要都必须是原始字符串。
function hexDigest(value) {
    return typeof value === "string" && HEX.test(value);
}

// 精确字符串不做名称归一化；控制字符和首尾空白均不能成为材料身份的一部分。
function exactText(value, label) {
    if (typeof value !== "string" || value.length === 0 || value.trim() !== value ||
        /[\u0000-\u001f\u007f]/.test(value)) throw new Error(label + "不是规范的非空字符串");
    utf8Length(value, MAX_BYTES);
    return value;
}

// 时间只接受 UTC 的 RFC3339 文本，并核对日期，防止无效月份日期被自动顺延。
function utcTime(value, label) {
    if (typeof value !== "string" ||
        !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)) {
        throw new Error(label + "必须为 UTC RFC3339 时间");
    }
    const milliseconds = Date.parse(value);
    if (!Number.isFinite(milliseconds) || value.slice(0, 4) === "0000" ||
        new Date(milliseconds).toISOString().slice(0, 19) !== value.slice(0, 19)) {
        throw new Error(label + "日期无效");
    }
    return milliseconds;
}

// 校验请求时间范围时不刷新任何来源时刻，过期请求不能通过重复读取恢复有效。
export function assertActive(request, cancellation) {
    if (cancellation && cancellation.isCancellationRequested === true) throw new Error("采集已取消");
    const now = Date.now();
    if (utcTime(request.createdAt, "createdAt") > now ||
        utcTime(request.expiresAt, "expiresAt") <= now) throw new Error("采集请求尚未生效或已过期");
}

// 请求只带宿主给出的官方一一映射；脚本不下载目录、不猜测 ID，也不重算摘要。
export function readRequest(raw) {
    const request = parseUniqueJSON(raw, MAX_BYTES);
    exactKeys(request, ["schemaVersion", "scriptVersion", "requestId", "requestDigest", "createdAt", "expiresAt",
        "role", "context", "materials"], "请求");
    if (request.schemaVersion !== 1 || request.scriptVersion !== SCRIPT_VERSION ||
        !hexDigest(request.requestId) || !hexDigest(request.requestDigest)) {
        throw new Error("请求版本或摘要格式无效");
    }
    exactKeys(request.role, ["uid", "region"], "角色");
    if (typeof request.role.uid !== "string" || !/^[1-9][0-9]{0,19}$/.test(request.role.uid)) {
        throw new Error("UID 必须是规范十进制字符串");
    }
    exactText(request.role.region, "区服");
    exactKeys(request.context, ["scopeId", "configName", "planRevision", "planFingerprint"], "执行上下文");
    if (!hexDigest(request.context.scopeId) || !hexDigest(request.context.planFingerprint)) {
        throw new Error("执行范围或计划指纹无效");
    }
    exactText(request.context.configName, "一条龙名称");
    const revision = request.context.planRevision;
    if (typeof revision !== "string" || !/^[1-9][0-9]{0,19}$/.test(revision) ||
        revision.length === 20 && revision > UINT64_MAX) throw new Error("计划版本不是有效正整数");
    if (utcTime(request.expiresAt, "expiresAt") <= utcTime(request.createdAt, "createdAt")) {
        throw new Error("请求结束时间必须晚于创建时间");
    }
    assertActive(request, null);
    if (!Array.isArray(request.materials) || request.materials.length > 4096) {
        throw new Error("材料数组超过上限或格式无效");
    }
    const ids = new Set();
    const names = new Set();
    for (const material of request.materials) {
        exactKeys(material, ["materialId", "name"], "材料");
        if (!Number.isSafeInteger(material.materialId) || material.materialId <= 0) {
            throw new Error("材料 ID 必须是正安全整数");
        }
        exactText(material.name, "材料名");
        // BetterGI 开启大小写不敏感绑定，保守拒绝与宿主方法同名的材料，避免数量读取歧义。
        if (HOST_METADATA_NAMES.has(material.name.toLowerCase())) throw new Error("材料名与宿主方法冲突");
        if (ids.has(material.materialId) || names.has(material.name)) throw new Error("材料 ID 或名称重复");
        ids.add(material.materialId);
        names.add(material.name);
    }
    return request;
}

// 计数结果只允许本次请求的精确名称，不能把额外项或奇形数量塞进可信结果。
export function readItems(request, result) {
    if (result === null || typeof result !== "object" || Array.isArray(result)) {
        throw new Error("背包计数返回的不是名称数量对象");
    }
    const expected = new Set(request.materials.map((material) => material.name));
    for (const name of expected) {
        if (HOST_METADATA_NAMES.has(name.toLowerCase())) throw new Error("材料名与宿主方法冲突");
    }
    for (const name of Object.keys(result)) {
        if (expected.has(name)) continue;
        // 不忽略任意函数或额外字段；只兼容真实 V8 已验证的四个函数类型宿主方法。
        if (HOST_METADATA_METHODS.has(name) && typeof result[name] === "function") continue;
        throw new Error("背包计数包含未请求的名称");
    }
    return request.materials.map((material) => {
        const item = { materialId: material.materialId, name: material.name, count: null,
            known: false, reason: "not-returned" };
        if (!Object.prototype.hasOwnProperty.call(result, material.name)) return item;
        const count = result[material.name];
        if (count === -2) {
            item.reason = "recognition-failed";
        } else if (typeof count === "number" && Number.isSafeInteger(count) && count >= 0) {
            item.count = count;
            item.known = true;
            item.reason = "";
        } else {
            throw new Error("背包计数包含无效数量");
        }
        return item;
    });
}
