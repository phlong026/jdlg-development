// 会话失效统一入口：清 token 并广播失效事件。
// 事件由 ManagementSession 监听清空用户态，受保护路由经 ProtectedRoute
// 软跳登录页——两套请求层（request.ts / wecomCustomers.ts）必须统一走这里，
// 不得再使用 window.location 硬跳（会整页刷新并丢失前端状态）。

import { tokenUtils } from './token';

export function expireSession() {
    tokenUtils.removeToken();
    window.dispatchEvent(new Event('management-session-expired'));
}
