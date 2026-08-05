/**
 * ============================================================
 * MD5 摘要工具（纯 JS，无第三方依赖）
 * ============================================================
 * 用于计算接口签名：SIGNATURE = md5(body + TIMESTAMP + NONCE)
 * 实现标准 RFC 1321，输入按 UTF-8 编码，输出 32 位小写十六进制。
 */

// 每轮循环左移位数（64 项）
const S = [
    7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
    5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
    4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
    6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
];

// 正弦常量表（64 项）：floor(abs(sin(i+1)) * 2^32)
const K = Array.from({ length: 64 }, (_, i) =>
    Math.floor(Math.abs(Math.sin(i + 1)) * 0x100000000)
);

/** 将字符串按 UTF-8 编码为字节数组（正确处理代理对） */
function utf8Bytes(str) {
    const out = [];
    for (let i = 0; i < str.length; i++) {
        let code = str.charCodeAt(i);
        if (code < 0x80) {
            out.push(code);
        } else if (code < 0x800) {
            out.push(0xc0 | (code >>> 6), 0x80 | (code & 0x3f));
        } else if (code < 0xd800 || code >= 0xe000) {
            out.push(0xe0 | (code >>> 12), 0x80 | ((code >>> 6) & 0x3f), 0x80 | (code & 0x3f));
        } else {
            // 代理对：两个 UTF-16 码元合成一个码点
            i++;
            code = 0x10000 + (((code & 0x3ff) << 10) | (str.charCodeAt(i) & 0x3ff));
            out.push(
                0xf0 | (code >>> 18),
                0x80 | ((code >>> 12) & 0x3f),
                0x80 | ((code >>> 6) & 0x3f),
                0x80 | (code & 0x3f)
            );
        }
    }
    return out;
}

/** 32 位整数循环左移 */
function leftRotate(value, shift) {
    return ((value << shift) | (value >>> (32 - shift))) >>> 0;
}

/**
 * 计算字符串的 MD5 摘要
 * @param {string} input 待计算字符串
 * @returns {string} 32 位小写十六进制摘要
 */
export function md5(input) {
    const bytes = utf8Bytes(String(input));
    const bitLen = bytes.length * 8;

    // 填充：补 0x80 与 0x00 直到长度 ≡ 56 (mod 64)，最后 8 字节写入原始位长（小端序）。
    // 注意：不能用 bitLen >>> (8*i) 取第 5 个字节（i=4 时 32 移位被 JS 按模 32 处理返回原值），
    // 因此改用除法右移，确保 64 位长度字段按位展开。
    const padded = bytes.slice();
    padded.push(0x80);
    while (padded.length % 64 !== 56) padded.push(0x00);
    for (let i = 0; i < 8; i++) {
        padded.push(Math.floor(bitLen / 2 ** (8 * i)) & 0xff);
    }

    let a0 = 0x67452301;
    let b0 = 0xefcdab89;
    let c0 = 0x98badcfe;
    let d0 = 0x10325476;

    for (let i = 0; i < padded.length; i += 64) {
        // 每 64 字节一块，拆成 16 个 32 位小端序字
        const w = new Array(16);
        for (let j = 0; j < 16; j++) {
            w[j] =
                padded[i + j * 4] |
                (padded[i + j * 4 + 1] << 8) |
                (padded[i + j * 4 + 2] << 16) |
                (padded[i + j * 4 + 3] << 24);
        }

        let a = a0;
        let b = b0;
        let c = c0;
        let d = d0;

        for (let j = 0; j < 64; j++) {
            let f;
            let g;
            if (j < 16) {
                f = (b & c) | (~b & d);
                g = j;
            } else if (j < 32) {
                f = (d & b) | (~d & c);
                g = (5 * j + 1) % 16;
            } else if (j < 48) {
                f = b ^ c ^ d;
                g = (3 * j + 5) % 16;
            } else {
                f = c ^ (b | ~d);
                g = (7 * j) % 16;
            }

            f = (f + a + K[j] + w[g]) >>> 0;
            a = d;
            d = c;
            c = b;
            b = (b + leftRotate(f, S[j])) >>> 0;
        }

        a0 = (a0 + a) >>> 0;
        b0 = (b0 + b) >>> 0;
        c0 = (c0 + c) >>> 0;
        d0 = (d0 + d) >>> 0;
    }

    // 按小端序输出十六进制（每个 32 位字 4 个字节）
    const hexChr = '0123456789abcdef'.split('');
    return [a0, b0, c0, d0]
        .map(word => {
            let hex = '';
            for (let i = 0; i < 4; i++) {
                const byte = (word >>> (8 * i)) & 0xff;
                hex += hexChr[(byte >>> 4) & 0xf] + hexChr[byte & 0xf];
            }
            return hex;
        })
        .join('');
}
