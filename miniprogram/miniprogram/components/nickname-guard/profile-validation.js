"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.hasSavedAvatar = hasSavedAvatar;
exports.hasProfileNickname = hasProfileNickname;
exports.hasCompleteProfile = hasCompleteProfile;
// 只接受已持久化的资料；界面占位图和微信临时文件不能代表用户已上传头像。
function hasSavedAvatar(value) {
    const avatar = String(value || '').trim();
    if (avatar.length > 2048 || /[\s\\#]/.test(avatar)
        || avatar.includes('/assets/images/home.jpg') || avatar.includes('/assets/images/default-avatar'))
        return false;
    const match = /^https:\/\/([^/]+)\//.exec(avatar);
    if (!match)
        return false;
    // 上传服务和微信头像使用 HTTPS 域名；不接受后台会隐藏的本地地址或带凭据地址。
    const labels = match[1].toLowerCase().replace(/\.$/, '').split('.');
    if (labels.length < 2 || !/^[a-z]+$/.test(labels[labels.length - 1])
        || ['local', 'localhost'].includes(labels[labels.length - 1])
        || !labels.every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label)))
        return false;
    try {
        decodeURI(avatar);
    }
    catch (_) {
        return false;
    }
    return true;
}
function hasProfileNickname(value) {
    const nickname = String(value || '').trim();
    return nickname.length > 0 && Array.from(nickname).length <= 32;
}
function hasCompleteProfile(profile) {
    return hasProfileNickname(profile?.nickname) && hasSavedAvatar(profile?.avatar);
}
