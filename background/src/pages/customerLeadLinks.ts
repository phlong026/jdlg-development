export const CUSTOMER_ARCHIVE = '/customer-management?tab=customers';

/**
 * 「去客户档案跟进」的落地地址。
 * 线索已同步到客户档案时带 customer 参数直达详情；尚未同步（或未启用客户管理）
 * 时回退到客户列表，保持原行为而不是给一个死链。
 */
export function customerArchiveHref(lead: { customer_id?: string | null } | null | undefined): string {
    const id = typeof lead?.customer_id === 'string' ? lead.customer_id.trim() : '';
    return id ? `${CUSTOMER_ARCHIVE}&customer=${encodeURIComponent(id)}&from=customer-leads` : CUSTOMER_ARCHIVE;
}
