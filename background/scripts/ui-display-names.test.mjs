// 后台列表不能把内部代码当成人名或文案：wx_ 登录名、32 位任务号、failed、ai_draw_single、模型原名。
// 这里锁定共用显示工具的口径，各页面测试再验证页面确实用了它。
import assert from 'node:assert/strict';
import test from 'node:test';
import { displayName, displayLabels } from './helpers/display-utils.mjs';

const { formatUserName, maskPhone, visibleUsername, isGeneratedUsername } = displayName();
const { formatAiScene, replaceSceneCodes, formatTaskStatus, formatAiModel, isFallbackModel, shortCode, humanizeTaskText, formatLeadSource } = displayLabels();

test('显示名：昵称 → 脱敏手机号 → 微信用户 #ID', () => {
    assert.equal(formatUserName({ nickname: ' 灿坤 ', phone: '13812341234', username: 'wx_o_gjx6' }, 291), '灿坤');
    assert.equal(formatUserName({ nickname: '', phone: '13812341234', username: 'wx_o_gjx6' }, 291), '138****1234');
    assert.equal(formatUserName({ nickname: '', phone: '138****1234' }, 291), '138****1234', '后端已脱敏的号码原样使用');
    assert.equal(formatUserName({ nickname: '', username: 'wx_o_gjx6' }, 291), '微信用户 #291');
    assert.equal(formatUserName(null, 7), '微信用户 #7');
    assert.equal(formatUserName({}, ''), '微信用户');
});

test('显示名：display_name 是后端拿用户名兜底的值，wx_ 开头时同样不展示', () => {
    assert.equal(formatUserName({ display_name: 'wx_cae6b664', username: 'wx_cae6b664' }, 11), '微信用户 #11');
    assert.equal(formatUserName({ display_name: '张工' }, 11), '张工');
});

test('显示名：人工设置的登录名（非 wx_）仍可作为最后的名字', () => {
    assert.equal(formatUserName({ nickname: '', username: 'zhangsan' }, 5), 'zhangsan');
    assert.equal(visibleUsername('zhangsan'), 'zhangsan');
    assert.equal(visibleUsername('wx_o_gjx6'), '');
    assert.equal(isGeneratedUsername('WX_abc'), true);
    assert.equal(isGeneratedUsername('wxid'), false);
});

test('手机号脱敏：11 位保留前 3 后 4', () => {
    assert.equal(maskPhone('13812341234'), '138****1234');
    assert.equal(maskPhone(' 138****1234 '), '138****1234');
    assert.equal(maskPhone(''), '');
    assert.equal(maskPhone(undefined), '');
});

test('AI 场景代码换成中文名，未知代码保持原样', () => {
    assert.equal(formatAiScene('ai_draw_single'), 'AI 绘画-单图');
    assert.equal(formatAiScene('mystery_scene'), 'mystery_scene');
    assert.equal(formatAiScene(''), '-');
    assert.equal(replaceSceneCodes('AI生成-ai_draw_multi-4张'), 'AI生成-AI 绘画-多图-4张');
});

test('任务状态与模型：failed 显示失败，模型只区分主力/备用服务', () => {
    assert.equal(formatTaskStatus('failed'), '失败');
    assert.equal(formatTaskStatus('success'), '成功');
    assert.equal(formatTaskStatus('processing'), '处理中');
    assert.equal(formatTaskStatus('weird'), 'weird');
    assert.equal(isFallbackModel('seedream-4-5-251128'), true);
    assert.equal(isFallbackModel('gpt-image-2-vip'), false);
    assert.equal(formatAiModel('seedream-4-5-251128'), '备用服务');
    assert.equal(formatAiModel('gpt-image-2-vip'), '主力服务');
    assert.equal(formatAiModel(''), '-');
    assert.equal(formatAiModel('模型待回填'), '模型待回填');
});

test('长编号只显示后 6 位，短编号原样', () => {
    assert.equal(shortCode('9d0fed07a1b2c3d4e5f60718293a4b5c'), '…3a4b5c');
    assert.equal(shortCode('v12'), 'v12');
    assert.equal(shortCode(''), '');
});

test('工单与告警里的任务描述翻成中文', () => {
    const content = '任务状态：failed；场景：ai_draw_single；模型：seedream-4-5-251128；灵石：10；错误信息：图片处理失败';
    assert.equal(humanizeTaskText(content), '任务状态：失败；场景：AI 绘画-单图；模型：备用服务；灵石：10；错误信息：图片处理失败');
    assert.equal(humanizeTaskText('AI图片任务待排查：9d0fed07a1b2c3d4e5f60718293a4b5c'), 'AI图片任务待排查：尾号 3a4b5c');
    assert.equal(humanizeTaskText('用户反馈付款后未到账'), '用户反馈付款后未到账');
    assert.equal(humanizeTaskText(''), '');
});

test('线索来源显示中文，未知来源不外露代码', () => {
    assert.equal(formatLeadSource('home_mascot'), '首页客服入口');
    assert.equal(formatLeadSource('share_tracking'), '分享意向');
    assert.equal(formatLeadSource('some_new_entry'), '其他来源');
    assert.equal(formatLeadSource(''), '-');
});

// 会员运营、虚拟支付没有渲染夹具，用源码约束：人名一律走 formatUserName，不再直接显示 item.username
import fs from 'node:fs';
const pageSource = name => fs.readFileSync(new URL(`../src/pages/${name}.tsx`, import.meta.url), 'utf8');

for (const page of ['membershipOperations', 'virtualPay']) {
    test(`${page}：用户名统一走 formatUserName，不把 wx_ 登录名当名字或副标题`, () => {
        const source = pageSource(page);
        assert.match(source, /from '\.\.\/utils\/displayName'/);
        assert.doesNotMatch(source, /\{item\.(?:display_name \|\| )?item\.username\}|\{item\.username \|\| `用户\$\{item\.user_id\}`\}/);
        assert.doesNotMatch(source, /ID \{item\.user_id\} · \{item\.username\}/);
    });
}

test('AI 定价表：成本列标为「近 7 日成本」，不再出现空的「场景标识」', () => {
    const source = pageSource('aiPricing');
    assert.match(source, /近 7 日成本/);
    assert.doesNotMatch(source, /¥\/次/);
    assert.doesNotMatch(source, /场景标识/);
});

test('审查补充：用户手写的工单内容不被误改，只翻译能确认的代码', () => {
    assert.equal(humanizeTaskText('我选的模型：写实风格，出图不对'), '我选的模型：写实风格，出图不对');
    assert.equal(humanizeTaskText('订单 CF1234567890ABCDEF1234 未到账'), '订单 CF1234567890ABCDEF1234 未到账');
    assert.equal(humanizeTaskText('交易号 4200001234567890123456789012'), '交易号 4200001234567890123456789012');
    assert.equal(humanizeTaskText('任务类型：image；模型：kling-v2'), '任务类型：图片任务；模型：kling-v2');
    assert.equal(formatAiModel('gpt-image-2.5-sunburst-vip'), '主力服务');
    assert.equal(formatAiModel('kling-v2'), 'kling-v2', '认不出的模型保持原样，不冒充主力服务');
});

test('审查补充：display_name 只是后端拿登录名兜底时，不抢在手机号前面', () => {
    assert.equal(formatUserName({ display_name: 'zhangsan', username: 'zhangsan', phone: '13812341234' }, 5), '138****1234');
    assert.equal(formatUserName({ display_name: '张工', username: 'zhangsan', phone: '13812341234' }, 5), '张工');
});
