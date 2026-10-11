"use strict";
const POLICY_DOCUMENTS = {
    privacy: {
        pageTitle: '隐私政策',
        updateDate: '2026-03-19',
        heroTitle: '感谢你使用甲第灵光',
        heroDesc: '为了向你提供登录、安全校验、AI 生成、订单与客户服务，我们会在必要范围内收集和使用相关信息，并严格按照本政策进行处理。',
        loginTip: '你需要先阅读并同意本政策后，才可以继续登录。',
        sections: [
            {
                title: '一、我们会收集哪些信息',
                items: [
                    '账号与身份信息：包括微信登录标识、用户名、头像、昵称等基础账号信息。',
                    '设备与系统信息：包括设备指纹、设备型号、系统版本、屏幕信息、语言与 Canvas 渲染特征，用于账号安全和风控校验。',
                    '业务使用信息：包括你上传的图片、输入的文字需求、生成记录、模板使用记录、订单记录、充值记录和邀请关系。',
                    '联系与认证信息：包括你主动提交的手机号、企业微信联系方式、实名认证与设计师认证资料。',
                ],
            },
            {
                title: '二、我们为什么使用这些信息',
                items: [
                    '用于完成登录注册、账号识别和安全校验。',
                    '用于完成 AI 生成、模板使用、订单处理、充值到账、认证审核与消息提醒。',
                    '用于排查异常行为、保护账号资产安全，并持续优化产品体验。',
                ],
            },
            {
                title: '三、信息如何存储与保护',
                items: [
                    '我们会在实现服务目标所必需的期限内保存你的相关信息。',
                    '我们会采取合理的技术与管理措施，尽力保护你的账号、订单、联系信息和生成内容安全。',
                    '未经你的明确同意，我们不会将你的个人信息出售给第三方。',
                ],
            },
            {
                title: '四、你的权利',
                items: [
                    '你可以在产品内查看、修改昵称、头像、密码等账户信息。',
                    '你可以申请变更或删除你主动填写的联系方式与资料。',
                    '如需注销账号、删除信息或反馈隐私问题，可通过页面内提供的联系方式与我们联系。',
                ],
            },
            {
                title: '五、补充说明',
                items: [
                    '设备指纹仅用于账号安全与异常校验，不会用于与你当前业务无关的用途。',
                    '你分享、发布或上传的内容，应确保你拥有合法使用权。',
                    '当隐私政策发生重要变更时，我们会通过合适方式提醒你。',
                ],
            },
        ],
        contactSectionTitle: '六、联系我们',
        contactItems: [
            { label: '主体名称', value: '甲第灵光（福建）科技有限公司' },
            { label: '联系邮箱', value: 'service@jiadilingguang.com' },
            { label: '联系说明', value: '如需处理隐私、删除、注销、纠错等问题，请在邮件中注明你的账号信息。' },
        ],
    },
    agreement: {
        pageTitle: '用户协议',
        updateDate: '2026-03-19',
        heroTitle: '欢迎使用甲第灵光',
        heroDesc: '本协议是你与甲第灵光（福建）科技有限公司之间关于使用本小程序及相关服务所订立的协议。请你仔细阅读并充分理解各条款内容。',
        loginTip: '你需要先阅读并同意本协议后，才可以继续登录。',
        sections: [
            {
                title: '一、服务说明',
                items: [
                    '甲第灵光向你提供 AI 设计生成、模板使用、订单管理、充值消费、认证服务、客服咨询等功能。',
                    '我们有权根据业务需要对服务内容进行调整，并会在合理范围内进行通知或说明。',
                    '你使用本服务时应具备相应的民事行为能力，并保证提交信息真实、合法、有效。',
                ],
            },
            {
                title: '二、账号与使用规范',
                items: [
                    '你应妥善保管账号、密码及登录凭证，不得出租、出借、转让或与他人共享账号。',
                    '你不得利用本服务从事违法违规活动，不得上传、生成或传播侵权、低俗、虚假或有害内容。',
                    '如我们发现异常登录、滥用功能或其他风险行为，有权采取限制、冻结或终止服务等措施。',
                ],
            },
            {
                title: '三、内容与知识产权',
                items: [
                    '你上传的素材、文字描述等内容，应确保拥有合法授权或使用权。',
                    '平台提供的模板、界面、标识及相关技术服务的知识产权归平台或权利人所有。',
                    'AI 生成结果仅供你在合法授权范围内使用，你应自行判断并承担相应使用风险。',
                ],
            },
            {
                title: '四、付费与订单',
                items: [
                    '充值、消费、认证等费用以页面展示为准，支付成功后按规则到账或生效。',
                    '虚拟权益、生成次数等数字化服务具有即时交付特征，除法律规定或平台明确说明外，不支持随意退款。',
                    '如发生支付异常、订单争议，可通过客服渠道与我们联系处理。',
                ],
            },
            {
                title: '五、免责与协议变更',
                items: [
                    '因网络故障、第三方服务异常、不可抗力等导致的服务中断或数据损失，平台将在合理范围内协助处理，但不承担超出法律规定之外的责任。',
                    '我们有权根据法律法规及业务变化更新本协议，并通过适当方式提示你查阅。',
                    '你继续使用服务，即视为你已阅读并同意更新后的协议内容。',
                ],
            },
        ],
        contactSectionTitle: '六、联系我们',
        contactItems: [
            { label: '主体名称', value: '甲第灵光（福建）科技有限公司' },
            { label: '联系邮箱', value: 'service@jiadilingguang.com' },
            { label: '联系说明', value: '如对本协议有疑问，请通过邮件联系我们并注明你的账号信息。' },
        ],
    },
    llm: {
        pageTitle: '大模型使用协议',
        updateDate: '2026-03-19',
        heroTitle: '甲第灵光大模型服务说明',
        heroDesc: '本协议用于说明你在使用甲第灵光 AI 大模型相关功能时的权利、义务与使用边界，包括智能设计、客服问答、规范查询等能力。',
        loginTip: '',
        sections: [
            {
                title: '一、服务范围',
                items: [
                    '大模型服务包括但不限于：AI 图像生成、视频生成、智能客服问答、建规快查、方案建议等能力。',
                    '模型输出结果基于你输入的提示词、图片、上下文信息等生成，仅供参考，不构成专业审批、施工或法律结论。',
                    '部分能力可能接入第三方模型或知识服务，具体以实际功能页面展示为准。',
                ],
            },
            {
                title: '二、输入与输出要求',
                items: [
                    '你输入的内容不得包含国家法律法规禁止的信息，不得侵犯他人知识产权、隐私权或其他合法权益。',
                    '你应对上传图片、文字描述、业务资料的真实性、合法性和授权范围负责。',
                    '对于模型生成的文字、图片、视频等内容，你应在合法合规前提下使用，并自行进行必要审核。',
                ],
            },
            {
                title: '三、数据使用与保护',
                items: [
                    '为完成生成与优化服务，我们可能处理你的输入内容、生成记录及必要的会话上下文信息。',
                    '我们不会将你的输入内容用于与本服务无关的用途，也不会在未经同意的情况下向第三方出售你的个人信息。',
                    '涉及账号安全、异常检测、服务稳定性优化的数据处理，将遵循隐私政策及相关法律规定。',
                ],
            },
            {
                title: '四、使用限制',
                items: [
                    '不得利用大模型服务批量采集、爬取平台数据，不得逆向分析、干扰或破坏模型服务接口。',
                    '不得将生成内容用于欺诈、误导、侵权、造谣或其他违法违规场景。',
                    '如你的使用行为触发风控规则，平台有权限制调用、暂停服务或封禁账号。',
                ],
            },
            {
                title: '五、责任说明',
                items: [
                    '大模型输出可能存在不准确、不完整或过时的情况，重要决策请你结合专业意见与官方规范自行判断。',
                    '因你违反本协议或上传违法内容导致的后果，由你自行承担。',
                    '平台有权根据业务变化、监管要求或模型能力升级对本协议进行更新。',
                ],
            },
        ],
        contactSectionTitle: '六、联系我们',
        contactItems: [
            { label: '主体名称', value: '甲第灵光（福建）科技有限公司' },
            { label: '联系邮箱', value: 'service@jiadilingguang.com' },
            { label: '联系说明', value: '如对大模型服务规则、内容纠错或投诉有疑问，请通过邮件联系我们。' },
        ],
    },
};
function normalizePolicyType(raw) {
    const type = String(raw || '').trim().toLowerCase();
    if (type === 'agreement' || type === 'user' || type === 'user_agreement') {
        return 'agreement';
    }
    if (type === 'llm' || type === 'model' || type === 'ai' || type === 'llm_agreement') {
        return 'llm';
    }
    return 'privacy';
}
function buildPolicyPageData(type, from) {
    const document = POLICY_DOCUMENTS[type];
    return {
        from,
        policyType: type,
        pageTitle: document.pageTitle,
        updateDate: document.updateDate,
        heroTitle: document.heroTitle,
        heroDesc: document.heroDesc,
        loginTip: document.loginTip,
        sections: document.sections,
        contactSectionTitle: document.contactSectionTitle,
        contactItems: document.contactItems,
    };
}
Page({
    data: {
        from: '',
        policyType: 'privacy',
        pageTitle: '隐私政策',
        updateDate: '2026-03-19',
        heroTitle: '',
        heroDesc: '',
        loginTip: '',
        sections: [],
        contactSectionTitle: '联系我们',
        contactItems: [],
    },
    onLoad(options) {
        const from = String(options?.from || '');
        const policyType = normalizePolicyType(String(options?.type || ''));
        this.setData(buildPolicyPageData(policyType, from));
    },
    onShareAppMessage() {
        const policyType = this.data.policyType || 'privacy';
        return {
            title: this.data.pageTitle || POLICY_DOCUMENTS[policyType].pageTitle,
            path: `/pages/privacy/privacy?type=${policyType}`,
        };
    },
});
