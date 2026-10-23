// 后台列表里的「人」统一按这个口径显示：昵称 → 脱敏手机号 → 微信用户 #ID。
// wx_ 开头的是小程序按 openid 自动生成的登录名（service/model/user.go buildMiniprogramUsername），
// 运营认不出人，只在用户详情的「账号与安全」区展示。

export interface UserNameSource {
    nickname?: string | null;
    /** 部分接口在昵称为空时用 username 兜底，可能也是 wx_ 开头 */
    display_name?: string | null;
    phone?: string | null;
    username?: string | null;
}

export const isGeneratedUsername = (username?: string | null): boolean =>
    /^wx_/i.test(String(username || '').trim());

/** 自动生成的登录名返回空串，人工设置的登录名原样返回 */
export const visibleUsername = (username?: string | null): string => {
    const value = String(username || '').trim();
    return isGeneratedUsername(value) ? '' : value;
};

/** 与后端 maskPhoneNumber 同口径：11 位保留前 3 后 4；已脱敏的号码原样返回 */
export const maskPhone = (phone?: string | null): string => {
    const value = String(phone || '').trim();
    if (!value || value.includes('*')) return value;
    if (/^\d{11}$/.test(value)) return `${value.slice(0, 3)}****${value.slice(7)}`;
    if (value.length <= 4) return '*'.repeat(value.length);
    const keep = Math.max(1, Math.floor(value.length / 4));
    return `${value.slice(0, keep)}****${value.slice(-keep)}`;
};

export const formatUserName = (user: UserNameSource | null | undefined, id?: number | string | null): string => {
    // display_name 等于登录名时只是后端兜底，不算真正的昵称
    const displayName = user?.display_name && user.display_name !== user.username ? user.display_name : '';
    // A saved profile nickname is presentation, even if its text starts with wx_.
    const nickname = String(user?.nickname || '').trim();
    if (nickname) return nickname;
    const visibleDisplayName = visibleUsername(displayName);
    if (visibleDisplayName) return visibleDisplayName;
    const phone = maskPhone(user?.phone);
    if (phone) return phone;
    const username = visibleUsername(user?.username);
    if (username) return username;
    return id ? `微信用户 #${id}` : '微信用户';
};
