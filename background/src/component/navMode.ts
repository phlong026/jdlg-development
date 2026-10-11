// 新旧导航的切换：灰度期保留旧导航作为一键回退。
// 优先级：地址里的 ?nav=old|new（并记住）> 本地记住的选择 > 默认新导航。
export type NavMode = 'new' | 'old';

const KEY = 'adminNav';

export function readNavMode(): NavMode {
    try {
        const fromUrl = new URLSearchParams(window.location.search).get('nav');
        if (fromUrl === 'old' || fromUrl === 'new') {
            window.localStorage.setItem(KEY, fromUrl);
            return fromUrl;
        }
        return window.localStorage.getItem(KEY) === 'old' ? 'old' : 'new';
    } catch {
        return 'new'; // 隐私模式等无法访问 localStorage 的环境，用默认值
    }
}

export function saveNavMode(mode: NavMode) {
    try {
        window.localStorage.setItem(KEY, mode);
    } catch {
        /* 无法持久化时只在本次会话内生效 */
    }
}
