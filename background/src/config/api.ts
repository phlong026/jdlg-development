// API配置
export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

export const API_ENDPOINTS = {
    // 认证
    AUTH: {
        LOGIN: '/api/v1/management/login',
        LOGOUT: '/api/v1/management/logout',
        ME: '/api/v1/management/me',
    },
    // 用户管理
    USERS: {
        LIST: '/api/v1/management/users',
        DETAIL: (id: string) => `/api/v1/management/users/${id}`,
        WORKBENCH: (id: string) => `/api/v1/management/users/${id}/workbench`,
        SET_STONES: (id: string) => `/api/v1/management/users/${id}/stones`,
        ADJUST_STONES: (id: string) => `/api/v1/management/users/${id}/stones/adjust`,
        ENTERPRISE_WECHAT: (id: string) => `/api/v1/management/users/${id}/enterprise-wechat`,
        STATS: '/api/v1/management/user-stats',
        BAN: (id: string | number) => `/api/v1/management/users/${id}/ban`,
        UNBAN: (id: string | number) => `/api/v1/management/users/${id}/unban`,
        PHONE_REVEAL: (id: string | number) => `/api/v1/management/users/${id}/phone-reveal`,
        MERGE_PRECHECK: '/api/v1/management/user-merge/precheck',
    },
    DESIGNERS: {
        LIST: '/api/v1/management/designers',
        DETAIL: (id: string) => `/api/v1/management/designers/${id}`,
        VISIBILITY: (id: string) => `/api/v1/management/designers/${id}/visibility`,
        SERVICE_STATUS: (id: string) => `/api/v1/management/designers/${id}/service-status`,
    },
    DISTRIBUTION: {
        OVERVIEW: '/api/v1/management/distribution/overview',
        INVITERS: '/api/v1/management/distribution/inviters',
        REWARDS: '/api/v1/management/distribution/rewards',
    },
    CONTENT_ANALYTICS: {
        OVERVIEW: '/api/v1/management/content-analytics/overview',
        DOWNLOAD_RANKING: '/api/v1/management/content-analytics/download-ranking',
        ENGAGEMENT_RANKING: '/api/v1/management/content-analytics/engagement-ranking',
        NEW_TEMPLATES: '/api/v1/management/content-analytics/new-templates',
        LOW_CONVERSION: '/api/v1/management/content-analytics/low-conversion',
        FEATURED_CASES: '/api/v1/management/content-analytics/featured-cases',
    },
    RISK_CONTROL: {
        OVERVIEW: '/api/v1/management/risk-control/overview',
        DEVICE_GROUPS: '/api/v1/management/risk-control/device-groups',
        DEVICE_CHANGES: '/api/v1/management/risk-control/device-changes',
        ALERTS: '/api/v1/management/risk-control/alerts',
        USERS: '/api/v1/management/risk-control/users',
    },
    SUPPORT_TICKETS: {
        OVERVIEW: '/api/v1/management/support-tickets/overview',
        LIST: '/api/v1/management/support-tickets',
        CREATE: '/api/v1/management/support-tickets',
        DETAIL: (id: string) => `/api/v1/management/support-tickets/${id}`,
        ASSIGN: (id: string) => `/api/v1/management/support-tickets/${id}/assign`,
        STATUS: (id: string) => `/api/v1/management/support-tickets/${id}/status`,
        RESOLUTION_NOTE: (id: string) => `/api/v1/management/support-tickets/${id}/resolution-note`,
        SYNC_SYSTEM_EXCEPTIONS: '/api/v1/management/support-tickets/sync-system-exceptions',
    },
    CUSTOMER_LEADS: {
        OVERVIEW: '/api/v1/management/customer-leads/overview',
        LIST: '/api/v1/management/customer-leads',
        DETAIL: (id: string) => `/api/v1/management/customer-leads/${id}`,
        FOLLOWUPS: (id: string) => `/api/v1/management/customer-leads/${id}/followups`,
        STATUS: (id: string) => `/api/v1/management/customer-leads/${id}/status`,
        BATCH: '/api/v1/management/customer-leads/batch',
    },
    REPORT_CENTER: {
        OVERVIEW: '/api/v1/management/report-center/overview',
        REPORTS: '/api/v1/management/report-center/reports',
        EXPORT: '/api/v1/management/report-center/export',
    },
    // 订单中心
    ORDERS: {
        LIST: '/api/v1/management/orders',
        FUND_STATS: '/api/v1/management/orders/fund-stats',
        DETAIL: (id: string) => `/api/v1/management/orders/${id}`,
        SUPPORT_TICKET: (id: string) => `/api/v1/management/orders/${id}/support-ticket`,
        VIRTUAL_PAY_REFUND: (id: string) => `/api/v1/management/virtualpay/orders/${id}/refund`,
        VIRTUAL_PAY_REFUND_PREVIEW: (id: string) => `/api/v1/management/virtualpay/orders/${id}/refund-preview`,
        VIRTUAL_PAY_VERIFY_REFUND: (id: string) => `/api/v1/management/virtualpay/orders/${id}/verify-refund`,
        VIRTUAL_PAY_ADVANCE: (id: string) => `/api/v1/management/virtualpay/orders/${id}/advance`,
    },
    VIRTUAL_PAY: {
        HEALTH: '/api/v1/management/virtualpay/health',
        LOGS: '/api/v1/management/virtualpay/logs',
        XPAY_LOG_REPLAY: (id: number) => `/api/v1/management/virtualpay/xpay-logs/${id}/replay`,
        CALIBRATIONS_SUMMARY: '/api/v1/management/virtualpay/calibrations/summary',
        CALIBRATE: '/api/v1/management/virtualpay/calibrate',
    },
    // 统计
    STATS: {
        DASHBOARD_OVERVIEW: '/api/v1/management/dashboard/overview',
        DASHBOARD_TRENDS: '/api/v1/management/dashboard/trends',
        DASHBOARD_TODO: '/api/v1/management/dashboard/todo',
    },
    // AI配置
    AI: {
        PRICING: '/api/v1/management/ai/pricing',
        API_CONFIG: '/api/v1/management/ai/api/config',
        API_CONFIG_TEST: '/api/v1/management/ai/api/config/test',
    },
    AI_TASKS: {
        LIST: '/api/v1/management/ai/tasks',
        BACKFILL_MODELS: '/api/v1/management/ai/tasks/backfill-models',
        DETAIL: (id: string) => `/api/v1/management/ai/tasks/${id}`,
        SUPPORT_TICKET: (id: string) => `/api/v1/management/ai/tasks/${id}/support-ticket`,
        VIDEO_LIST: '/api/v1/management/ai/video-tasks',
        VIDEO_DETAIL: (id: string) => `/api/v1/management/ai/video-tasks/${id}`,
        VIDEO_SUPPORT_TICKET: (id: string) => `/api/v1/management/ai/video-tasks/${id}/support-ticket`,
    },
    // OSS管理
    OSS: {
        LIST: '/api/v1/management/oss/files',
        UPLOAD: '/api/v1/management/oss/upload',
        DELETE: (id: string) => `/api/v1/management/oss/files/${id}`,
        BATCH_DELETE: '/api/v1/management/oss/files/batch',
    },
    // 模板管理
    TEMPLATES: {
        LIST: '/api/v1/management/templates',
        CREATE: '/api/v1/management/templates',
        DETAIL: (id: string) => `/api/v1/management/templates/${id}`,
        UPDATE: (id: string) => `/api/v1/management/templates/${id}`,
        STATUS: (id: string) => `/api/v1/management/templates/${id}/status`,
        DELETE: (id: string) => `/api/v1/management/templates/${id}`,
        CATEGORY: (id: string) => `/api/v1/management/templates/${id}/category`,
        FEATURED: (id: string) => `/api/v1/management/templates/${id}/featured`,
        FEATURED_LIST: '/api/v1/management/templates/featured',
        CATEGORIES: {
            LIST: '/api/v1/management/templates/categories',
            CREATE: '/api/v1/management/templates/categories',
            DELETE: (id: string) => `/api/v1/management/templates/categories/${id}`,
        },
        TAB_CONFIG: '/api/v1/management/templates/tab-config',
        FEATURED_GROUPS: {
            LIST: '/api/v1/management/templates/featured-groups',
            CREATE: '/api/v1/management/templates/featured-groups',
            DETAIL: (id: string) => `/api/v1/management/templates/featured-groups/${id}`,
            UPDATE: (id: string) => `/api/v1/management/templates/featured-groups/${id}`,
            DELETE: (id: string) => `/api/v1/management/templates/featured-groups/${id}`,
        },
    },
    INSPIRATIONS: {
        LIST: '/api/v1/management/inspirations',
        CREATE: '/api/v1/management/inspirations',
        DETAIL: (id: string) => `/api/v1/management/inspirations/${id}`,
        UPDATE: (id: string) => `/api/v1/management/inspirations/${id}`,
        STATUS: (id: string) => `/api/v1/management/inspirations/${id}/status`,
        DELETE: (id: string) => `/api/v1/management/inspirations/${id}`,
    },
    // 充值流水
    RECHARGE: {
        LIST: '/api/v1/management/recharge/records',
        DETAIL: (id: string) => `/api/v1/management/recharge/records/${id}`,
        EXPORT: '/api/v1/management/recharge/export',
    },
    // 操作审计（原"日志管理"：logs 表无人写入，已按审计方案改造为操作审计页）
    AUDIT: {
        LIST: '/api/v1/management/audit',
    },
    // 实用工具管理
    UTILITY_TOOLS: {
        LIST: '/api/v1/management/utility-tools',
        CREATE: '/api/v1/management/utility-tools',
        DETAIL: (id: string) => `/api/v1/management/utility-tools/${id}`,
        UPDATE: (id: string) => `/api/v1/management/utility-tools/${id}`,
        DELETE: (id: string) => `/api/v1/management/utility-tools/${id}`,
    },
    AI_TOOLS: {
        LIST: '/api/v1/management/ai-tools',
        CREATE: '/api/v1/management/ai-tools',
        DETAIL: (id: string) => `/api/v1/management/ai-tools/${id}`,
        UPDATE: (id: string) => `/api/v1/management/ai-tools/${id}`,
        DELETE: (id: string) => `/api/v1/management/ai-tools/${id}`,
    },
    // 充值配置管理
    RECHARGE_CONFIG: {
        LIST: '/api/v1/management/recharge-config',
        GET: (paymentMode: string) => `/api/v1/management/recharge-config/${paymentMode}`,
        CREATE_OR_UPDATE: '/api/v1/management/recharge-config',
        DELETE: (id: string) => `/api/v1/management/recharge-config/${id}`,
    },
    MEMBERSHIP_PLANS: {
        LIST: '/api/v1/management/membership-plans',
        DETAIL: (id: string) => `/api/v1/management/membership-plans/${id}`,
        CREATE_OR_UPDATE: '/api/v1/management/membership-plans',
        DELETE: (id: string) => `/api/v1/management/membership-plans/${id}`,
    },
    MEMBERSHIP_OPERATIONS: {
        OVERVIEW: '/api/v1/management/membership-operations/overview',
        USERS: '/api/v1/management/membership-operations/users',
        FIX_PERMISSION: (userId: string | number) => `/api/v1/management/membership-operations/users/${userId}/fix-permission`,
    },
    // 资质认证与审核（工单）
    CERTIFICATION: {
        LIST: '/api/v1/management/certification-applications',
        DETAIL: (id: string) => `/api/v1/management/certification-applications/${id}`,
        REVIEW: (id: string) => `/api/v1/management/certification-applications/${id}/review`,
        STATS: '/api/v1/management/certification-application-stats',
        REVEAL: (id: string) => `/api/v1/management/certification-applications/${id}/reveal`,
    },
};
