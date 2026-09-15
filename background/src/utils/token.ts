// Token管理工具

const TOKEN_KEY = 'admin_token';

export const tokenUtils = {
    // 保存token
    setToken: (token: string) => {
        localStorage.setItem(TOKEN_KEY, token);
    },

    // 获取token
    getToken: (): string | null => {
        return localStorage.getItem(TOKEN_KEY);
    },

    // 删除token
    removeToken: () => {
        localStorage.removeItem(TOKEN_KEY);
    },

    // 检查是否有token
    hasToken: (): boolean => {
        return !!localStorage.getItem(TOKEN_KEY);
    },
};
