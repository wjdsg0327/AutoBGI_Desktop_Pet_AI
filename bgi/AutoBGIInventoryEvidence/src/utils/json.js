// 按 UTF-8 字节计数并拒绝孤立代理项，避免字符数限制被中文或无效 Unicode 绕过。
export function utf8Length(text, limit) {
    if (typeof text !== "string") throw new Error("文本类型无效");
    let size = 0;
    for (let index = 0; index < text.length; index++) {
        const code = text.charCodeAt(index);
        if (code >= 0xd800 && code <= 0xdbff) {
            const next = text.charCodeAt(index + 1);
            if (!(next >= 0xdc00 && next <= 0xdfff)) throw new Error("文本包含孤立代理项");
            index++;
            size += 4;
        } else if (code >= 0xdc00 && code <= 0xdfff) {
            throw new Error("文本包含孤立代理项");
        } else {
            size += code < 0x80 ? 1 : code < 0x800 ? 2 : 3;
        }
        if (size > limit) throw new Error("JSON 超过字节上限");
    }
    return size;
}

// 原生解析确认 JSON 语法；第二次有界扫描拒绝重复键，不能静默采用最后一个同名字段。
export function parseUniqueJSON(text, limit) {
    utf8Length(text, limit);
    const result = JSON.parse(text);
    let position = 0;
    const skipSpace = () => {
        while (position < text.length && /[\t\n\r ]/.test(text[position])) position++;
    };
    const readString = () => {
        const start = position++;
        for (; position < text.length; position++) {
            if (text[position] === "\\") {
                position++;
            } else if (text[position] === '"') {
                position++;
                const value = JSON.parse(text.slice(start, position));
                utf8Length(value, limit);
                return value;
            }
        }
        throw new Error("JSON 字符串未结束");
    };
    const scan = (depth) => {
        if (depth > 16) throw new Error("JSON 嵌套超过上限");
        skipSpace();
        if (text[position] === '"') {
            readString();
            return;
        }
        if (text[position] === "{") {
            position++;
            skipSpace();
            const keys = new Set();
            while (position < text.length && text[position] !== "}") {
                const key = readString();
                if (keys.has(key)) throw new Error("JSON 包含重复字段");
                keys.add(key);
                skipSpace();
                position++;
                scan(depth + 1);
                skipSpace();
                if (text[position] === "}") break;
                position++;
                skipSpace();
            }
            position++;
            return;
        }
        if (text[position] === "[") {
            position++;
            skipSpace();
            while (position < text.length && text[position] !== "]") {
                scan(depth + 1);
                skipSpace();
                if (text[position] === "]") break;
                position++;
                skipSpace();
            }
            position++;
            return;
        }
        // 标量语法已由 JSON.parse 检查，这里只推进至下一个结构边界。
        while (position < text.length && !/[\t\n\r ,}\]]/.test(text[position])) position++;
    };
    scan(0);
    return result;
}

// 所有结构都采用字段白名单，额外数据不能成为另一个隐式协议。
export function exactKeys(value, keys, label) {
    if (value === null || typeof value !== "object" || Array.isArray(value)) {
        throw new Error(label + "必须是对象");
    }
    const actual = Object.keys(value);
    if (actual.length !== keys.length || actual.some((key) => !keys.includes(key))) {
        throw new Error(label + "字段不完整或包含未知字段");
    }
}
