"use strict";
/**
 * 设备指纹生成工具
 * 使用系统信息和canvas生成设备指纹
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.generateDeviceFingerprint = generateDeviceFingerprint;
exports.getCachedDeviceFingerprint = getCachedDeviceFingerprint;
exports.cacheDeviceFingerprint = cacheDeviceFingerprint;
// @ts-ignore
const CryptoJS = require('./crypto-js/index.js');
/**
 * 生成设备指纹
 * @returns Promise<string> 设备指纹字符串
 */
function generateDeviceFingerprint() {
    return new Promise((resolve) => {
        try {
            // 1. 获取系统信息
            const systemInfo = wx.getSystemInfoSync();
            // 2. 生成canvas指纹（带超时处理）
            const canvasPromise = generateCanvasFingerprint();
            const timeoutPromise = new Promise((resolve) => {
                setTimeout(() => {
                    resolve('');
                }, 2000); // 2秒超时
            });
            Promise.race([canvasPromise, timeoutPromise])
                .then((canvasFingerprint) => {
                // 3. 组合设备信息
                const deviceInfo = {
                    brand: systemInfo.brand || '',
                    model: systemInfo.model || '',
                    system: systemInfo.system || '',
                    platform: systemInfo.platform || '',
                    version: systemInfo.version || '',
                    SDKVersion: systemInfo.SDKVersion || '',
                    screenWidth: systemInfo.screenWidth || 0,
                    screenHeight: systemInfo.screenHeight || 0,
                    pixelRatio: systemInfo.pixelRatio || 0,
                    language: systemInfo.language || '',
                    fontSizeSetting: systemInfo.fontSizeSetting || 0,
                    canvas: canvasFingerprint || '',
                };
                // 4. 将设备信息转换为字符串并计算hash
                const deviceString = JSON.stringify(deviceInfo);
                const fingerprint = hashString(deviceString);
                resolve(fingerprint);
            })
                .catch((_error) => {
                // 即使失败，也使用系统信息生成指纹
                const deviceInfo = {
                    brand: systemInfo.brand || '',
                    model: systemInfo.model || '',
                    system: systemInfo.system || '',
                    platform: systemInfo.platform || '',
                    version: systemInfo.version || '',
                    SDKVersion: systemInfo.SDKVersion || '',
                    screenWidth: systemInfo.screenWidth || 0,
                    screenHeight: systemInfo.screenHeight || 0,
                    pixelRatio: systemInfo.pixelRatio || 0,
                    language: systemInfo.language || '',
                    fontSizeSetting: systemInfo.fontSizeSetting || 0,
                };
                const deviceString = JSON.stringify(deviceInfo);
                const fingerprint = hashString(deviceString);
                resolve(fingerprint);
            });
        }
        catch (error) {
            // 如果获取系统信息失败，使用默认值
            const defaultFingerprint = hashString('default_device_' + Date.now());
            resolve(defaultFingerprint);
        }
    });
}
/**
 * 生成canvas指纹
 * @returns Promise<string> canvas指纹字符串
 */
function generateCanvasFingerprint() {
    return new Promise((resolve) => {
        try {
            // 检查是否支持 createOffscreenCanvas
            if (typeof wx.createOffscreenCanvas === 'undefined') {
                resolve('');
                return;
            }
            // 尝试使用离屏canvas（微信小程序推荐方式）
            let offscreenCanvas;
            try {
                const createOffscreenCanvas = wx.createOffscreenCanvas;
                offscreenCanvas = createOffscreenCanvas({
                    type: '2d',
                    width: 200,
                    height: 200,
                });
            }
            catch (e) {
                resolve('');
                return;
            }
            if (!offscreenCanvas) {
                resolve('');
                return;
            }
            const ctx = offscreenCanvas.getContext('2d');
            if (!ctx) {
                resolve('');
                return;
            }
            // 绘制一些内容以生成设备特定的渲染特征
            try {
                ctx.fillStyle = 'rgb(200, 100, 50)';
                ctx.fillRect(10, 10, 50, 50);
                ctx.fillStyle = 'rgba(50, 150, 200, 0.5)';
                ctx.fillRect(30, 30, 50, 50);
                ctx.font = '14px Arial';
                ctx.fillStyle = 'rgb(100, 100, 100)';
                ctx.fillText('DeviceFingerprint', 10, 80);
                // 添加一些复杂的图形
                ctx.beginPath();
                ctx.arc(100, 100, 30, 0, Math.PI * 2);
                ctx.fill();
            }
            catch (e) {
                resolve('');
                return;
            }
            // 设置超时，防止 toDataURL 回调一直不执行
            const timeout = setTimeout(() => {
                try {
                    const textMetrics = ctx.measureText('DeviceFingerprint');
                    const canvasFingerprint = JSON.stringify({
                        textWidth: textMetrics.width,
                        canvas: offscreenCanvas.width + 'x' + offscreenCanvas.height,
                    });
                    resolve(hashString(canvasFingerprint));
                }
                catch (e) {
                    resolve(hashString('canvas_' + Date.now()));
                }
            }, 1500); // 1.5秒超时
            // 转换为base64数据
            try {
                offscreenCanvas.toDataURL({
                    success: (res) => {
                        clearTimeout(timeout);
                        const fingerprint = hashString(res.data);
                        resolve(fingerprint);
                    },
                    fail: (_err) => {
                        clearTimeout(timeout);
                        // 如果转换失败，使用canvas的渲染特征
                        try {
                            const textMetrics = ctx.measureText('DeviceFingerprint');
                            const canvasFingerprint = JSON.stringify({
                                textWidth: textMetrics.width,
                                canvas: offscreenCanvas.width + 'x' + offscreenCanvas.height,
                            });
                            resolve(hashString(canvasFingerprint));
                        }
                        catch (e) {
                            // 最后使用时间戳作为fallback
                            resolve(hashString('canvas_' + Date.now()));
                        }
                    },
                });
            }
            catch (e) {
                clearTimeout(timeout);
                resolve('');
            }
        }
        catch (error) {
            // 如果canvas完全失败，返回空字符串（不影响设备指纹生成）
            resolve('');
        }
    });
}
/**
 * 使用MD5生成设备指纹（至少18位）
 * @param str 要hash的字符串
 * @returns hash值（至少18位十六进制字符串）
 */
function hashString(str) {
    // 计算MD5
    const md5Hash = CryptoJS.MD5(str);
    const md5Hex = md5Hash.toString(CryptoJS.enc.Hex);
    // MD5是32位hex，确保至少18位
    // 如果不够18位（理论上不会），重复直到18位
    let result = md5Hex;
    if (result.length < 18) {
        while (result.length < 18) {
            result += md5Hex;
        }
        result = result.substring(0, 18);
    }
    // 返回完整的MD5（32位），确保足够长
    return result;
}
/**
 * 获取缓存的设备指纹（如果已生成过）
 * @returns string | null
 */
function getCachedDeviceFingerprint() {
    try {
        return wx.getStorageSync('device_fingerprint') || null;
    }
    catch (error) {
        return null;
    }
}
/**
 * 保存设备指纹到缓存
 * @param fingerprint 设备指纹
 */
function cacheDeviceFingerprint(fingerprint) {
    try {
        wx.setStorageSync('device_fingerprint', fingerprint);
    }
    catch (error) {
    }
}
