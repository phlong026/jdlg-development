// 使用相对路径导入构建后的 npm 包（npm 包已构建到 utils 目录下）
// @ts-ignore
const smCrypto = require('./miniprogram-sm-crypto/index.js');
// @ts-ignore
const CryptoJS = require('./crypto-js/index.js');

/**
 * 生成请求所需的参数
 * @param token 登录返回的token
 * @param requestBody 请求体（JSON字符串或对象）
 * @param apiPath 接口地址（包含路径和查询参数，如：/api/v1/miniprogram/ai/draw）
 * @param hmacKey HMAC密钥（需要与后端保持一致）
 * @param sm4Key SM4密钥（32字节，需要与后端保持一致）
 * @returns 包含token、token-signature、sin、md5-signature的对象
 */
export interface RequestParams {
    token: string;
    'token-signature': string;
    sin: string;
    'md5-signature': string;
    pass: string;
    tm: string;
}

export function generateRequestParams(
    token: string,
    requestBody: string | object,
    apiPath: string,
    deviceID?: string,
    hmacKey: string = 'jiadilinguang-hmac-secret-key-2024',
    sm4Key: string = '1234567890123456' // 16字节密钥（会自动转换为32字符十六进制）
): RequestParams {
    // 1. 生成token的HMAC签名（SHA256）
    const tokenSignature = generateTokenSignature(token, hmacKey);

    // 2. 将请求体转换为字符串（如果是对象）
    const bodyString = typeof requestBody === 'string'
        ? requestBody
        : JSON.stringify(requestBody);

    // 3. 生成sin：请求体SM4加密 -> base64编码 -> 取前32位
    const sin = generateSin(bodyString, sm4Key);

    // 4. 生成md5-signature
    const md5Signature = generateMD5Signature(sin, tokenSignature, apiPath, sm4Key);

    // 5. 生成pass和tm（使用同一个时间戳）
    const timestamp = Date.now().toString();
    const pass = generatePass(sin, md5Signature, deviceID || '', timestamp, sm4Key);
    const tm = generateTm(timestamp, apiPath, sm4Key);

    return {
        token,
        'token-signature': tokenSignature,
        sin,
        'md5-signature': md5Signature,
        pass,
        tm,
    };
}

/**
 * 将SM4密钥转换为32字符的十六进制字符串
 * @param sm4Key SM4密钥（可以是16字符ASCII字符串或32字符十六进制字符串）
 * @returns 32字符的十六进制字符串
 */
function convertSM4KeyToHex(sm4Key: string): string {
    // 如果已经是32字符的十六进制字符串，直接返回
    if (sm4Key.length === 32 && /^[0-9a-fA-F]{32}$/.test(sm4Key)) {
        return sm4Key.toLowerCase();
    }

    // 如果是16字符的ASCII字符串，转换为十六进制
    if (sm4Key.length === 16) {
        let hexKey = '';
        for (let i = 0; i < sm4Key.length; i++) {
            const hex = sm4Key.charCodeAt(i).toString(16).padStart(2, '0');
            hexKey += hex;
        }
        if (hexKey.length !== 32) {
            throw new Error(`密钥转换后长度不正确: ${hexKey.length}, 期望32`);
        }
        return hexKey;
    }

    // 其他情况，尝试直接使用
    return sm4Key;
}

/**
 * 生成token的HMAC签名（SHA256）
 * @param token token字符串
 * @param hmacKey HMAC密钥
 * @returns hex编码的签名
 */
function generateTokenSignature(token: string, hmacKey: string): string {
    const hmac = CryptoJS.HmacSHA256(token, hmacKey);
    return hmac.toString(CryptoJS.enc.Hex);
}

/**
 * 生成sin：请求体SM4加密后base64编码取前32位
 * @param requestBody 请求体字符串
 * @param sm4Key SM4密钥（32字符十六进制字符串，对应16字节）
 * @returns sin字符串（32位）
 */
function generateSin(requestBody: string, sm4Key: string): string {
    try {
        // 转换密钥为32字符十六进制格式
        const hexKey = convertSM4KeyToHex(sm4Key);

        // 如果请求体为空（GET请求），使用固定占位符，与后端保持一致
        const bodyToEncrypt = requestBody || '{}';

        // 1. 使用SM4加密请求体（返回hex字符串）
        const encryptedHex = smCrypto.sm4.encrypt(bodyToEncrypt, hexKey);

        // 2. 将hex转换为base64编码
        // hex -> WordArray -> Base64
        const hexWords = CryptoJS.enc.Hex.parse(encryptedHex);
        let base64Encoded = CryptoJS.enc.Base64.stringify(hexWords);

        // 3. 取前32位（如果不足32位，重复填充，与后端保持一致）
        if (base64Encoded.length < 32) {
            while (base64Encoded.length < 32) {
                base64Encoded += base64Encoded;
            }
        }
        return base64Encoded.substring(0, 32);
    } catch (error: any) {
        throw new Error(`生成sin失败: ${error.message || error}`);
    }
}

/**
 * 生成MD5签名
 * 流程：sin + token签名 + base64编码的接口地址 -> base64编码 -> SM4加密 -> base64编码 -> MD5 -> 取前64位
 * @param sin sin字符串
 * @param tokenSignature token签名
 * @param apiPath 接口地址
 * @param sm4Key SM4密钥
 * @returns MD5签名（64位hex字符串）
 */
function generateMD5Signature(
    sin: string,
    tokenSignature: string,
    apiPath: string,
    sm4Key: string
): string {
    try {
        // 转换密钥为32字符十六进制格式
        const hexKey = convertSM4KeyToHex(sm4Key);

        // 1. 组合：sin + token签名 + base64编码的接口地址
        const apiPathEncoded = CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(apiPath));
        const combined = sin + tokenSignature + apiPathEncoded;

        // 2. Base64编码
        const combinedEncoded = CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(combined));

        // 3. 使用SM4加密（返回hex字符串）
        const encryptedHex = smCrypto.sm4.encrypt(combinedEncoded, hexKey);

        // 4. 将hex转换为base64编码
        const hexWords = CryptoJS.enc.Hex.parse(encryptedHex);
        const encryptedEncoded = CryptoJS.enc.Base64.stringify(hexWords);

        // 5. 计算MD5
        const md5Hash = CryptoJS.MD5(encryptedEncoded);
        const md5Hex = md5Hash.toString(CryptoJS.enc.Hex);

        // 6. 取前64位（如果MD5不够64位，重复直到64位）
        let result = md5Hex;
        if (result.length < 64) {
            // 重复MD5直到64位
            while (result.length < 64) {
                result += md5Hex;
            }
            result = result.substring(0, 64);
        } else {
            result = result.substring(0, 64);
        }

        return result;
    } catch (error: any) {
        throw new Error(`生成MD5签名失败: ${error.message || error}`);
    }
}


/**
 * 生成pass参数
 * 规则：sin + md5 + 设备id + 时间戳 -> base64编码 -> SM4加密 -> 取128位
 * @param sin sin字符串
 * @param md5Signature md5签名
 * @param deviceID 设备ID
 * @param timestamp 时间戳（字符串）
 * @param sm4Key SM4密钥
 * @returns pass字符串（128位）
 */
function generatePass(
    sin: string,
    md5Signature: string,
    deviceID: string,
    timestamp: string,
    sm4Key: string
): string {
    try {
        // 转换密钥为32字符十六进制格式
        const hexKey = convertSM4KeyToHex(sm4Key);

        // 1. 组合：sin + md5 + 设备id + 时间戳（不再包含随机数）
        const combined = sin + md5Signature + deviceID + timestamp;

        // 2. Base64编码
        const combinedEncoded = CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(combined));

        // 3. 使用SM4加密（返回hex字符串）
        const encryptedHex = smCrypto.sm4.encrypt(combinedEncoded, hexKey);

        // 5. 在hex阶段截断到96字节（16字节的倍数，确保SM4解密时数据完整）
        // 96字节 = 192个hex字符
        // 128位base64可以表示96字节的数据（128 = 96 * 4/3，向上取整）
        let truncatedHex = encryptedHex;
        if (truncatedHex.length > 192) {
            truncatedHex = truncatedHex.substring(0, 192);
        } else if (truncatedHex.length < 192) {
            // 如果不够192个hex字符，重复直到至少192个
            while (truncatedHex.length < 192) {
                truncatedHex += encryptedHex;
            }
            truncatedHex = truncatedHex.substring(0, 192);
        }

        // 6. 将截断后的hex转换为base64编码
        const hexWords = CryptoJS.enc.Hex.parse(truncatedHex);
        const encryptedBase64 = CryptoJS.enc.Base64.stringify(hexWords);

        // 7. 取前128位（此时应该是完整的base64字符串，正好128位）
        // 96字节的base64编码正好是128字符（96 * 4/3 = 128）
        let result = encryptedBase64;
        if (result.length < 128) {
            // 如果不够128位，重复直到至少128位
            while (result.length < 128) {
                result += encryptedBase64;
            }
        }
        // 取前128位（确保是4的倍数）
        result = result.substring(0, 128);

        // 确保是4的倍数（128已经是4的倍数，但为了安全还是检查）
        const remainder = result.length % 4;
        if (remainder > 0) {
            // 如果不够4的倍数，补齐（但128应该是4的倍数）
            result += '='.repeat(4 - remainder);
        }
        return result;
    } catch (error: any) {
        throw new Error(`生成pass失败: ${error.message || error}`);
    }
}

/**
 * 生成tm参数
 * 规则：时间戳 + 请求地址（base64编码） -> SM4加密 -> base64编码
 * 如果末尾有两个=，则替换为其他字符
 * @param timestamp 时间戳（字符串，必须和pass使用同一个）
 * @param apiPath 接口地址
 * @param sm4Key SM4密钥
 * @returns tm字符串
 */
function generateTm(
    timestamp: string,
    apiPath: string,
    sm4Key: string
): string {
    try {
        // 转换密钥为32字符十六进制格式
        const hexKey = convertSM4KeyToHex(sm4Key);

        // 1. 将接口地址进行base64编码
        const apiPathEncoded = CryptoJS.enc.Base64.stringify(CryptoJS.enc.Utf8.parse(apiPath));

        // 2. 组合：时间戳 + base64编码的接口地址
        const combined = timestamp + apiPathEncoded;

        // 3. 使用SM4加密（返回hex字符串）
        const encryptedHex = smCrypto.sm4.encrypt(combined, hexKey);

        // 4. 将hex转换为base64（不替换末尾=），后端原样解码；密文长度由 SM4 加密保证为 16 的倍数
        const hexWords = CryptoJS.enc.Hex.parse(encryptedHex);
        return CryptoJS.enc.Base64.stringify(hexWords);
    } catch (error: any) {
        throw new Error(`生成tm失败: ${error.message || error}`);
    }
}

/**
 * 将请求参数转换为请求头对象
 * @param params 请求参数
 * @returns 请求头对象
 */
export function paramsToHeaders(params: RequestParams): Record<string, string> {
    return {
        'token': params.token,
        'token-signature': params['token-signature'],
        'sin': params.sin,
        'md5-signature': params['md5-signature'],
        'pass': params.pass,
        'tm': params.tm,
    };
}
