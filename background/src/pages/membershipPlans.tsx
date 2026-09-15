import React, { useEffect, useState } from 'react';
import Layout from '../component/layout';
import {
    getMembershipPlanList,
    getMembershipPlan,
    createOrUpdateMembershipPlan,
    deleteMembershipPlan,
    type MembershipPlanItem,
} from '../api/membershipPlans';
import './membershipPlans.scss';

const defaultForm = {
    id: 0,
    plan_code: '',
    title: '',
    description: '',
    badge_text: '',
    recharge_amount_fen: 5000,
    duration_days: 30,
    template_download_enabled: true,
    is_enabled: true,
    sort_order: 0,
    download_validity_days: 30,
    max_total_downloads: 0,
    daily_download_limit: 0,
    rate_limit_per_minute: 0,
};

const MembershipPlans: React.FC = () => {
    const [plans, setPlans] = useState<MembershipPlanItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [selectedId, setSelectedId] = useState<number>(0);
    const [formData, setFormData] = useState(defaultForm);

    useEffect(() => {
        loadPlans();
    }, []);

    const loadPlans = async () => {
        setLoading(true);
        try {
            const list = await getMembershipPlanList();
            setPlans(list);
        } catch (error) {
            console.error('加载会员计划失败:', error);
            alert('加载会员计划失败');
        } finally {
            setLoading(false);
        }
    };

    const handleSelect = async (id: number) => {
        setSelectedId(id);
        try {
            const item = await getMembershipPlan(String(id));
            setFormData({
                id: item.id,
                plan_code: item.plan_code || '',
                title: item.title || '',
                description: item.description || '',
                badge_text: item.badge_text || '',
                recharge_amount_fen: item.recharge_amount_fen || 0,
                duration_days: item.duration_days || 30,
                template_download_enabled: Boolean(item.template_download_enabled),
                is_enabled: Boolean(item.is_enabled),
                sort_order: item.sort_order || 0,
                download_validity_days: item.download_validity_days ?? 30,
                max_total_downloads: item.max_total_downloads ?? 0,
                daily_download_limit: item.daily_download_limit ?? 0,
                rate_limit_per_minute: item.rate_limit_per_minute ?? 0,
            });
        } catch (error) {
            console.error('加载会员计划详情失败:', error);
            alert('加载会员计划详情失败');
        }
    };

    const resetForm = () => {
        setSelectedId(0);
        setFormData(defaultForm);
    };

    const handleSave = async () => {
        if (!formData.plan_code.trim() || !formData.title.trim()) {
            alert('计划编码和计划名称不能为空');
            return;
        }
        if (formData.recharge_amount_fen <= 0 || formData.duration_days <= 0) {
            alert('充值金额和有效天数必须大于 0');
            return;
        }
        setSaving(true);
        try {
            await createOrUpdateMembershipPlan({
                ...formData,
                plan_code: formData.plan_code.trim(),
                title: formData.title.trim(),
                description: formData.description.trim(),
                badge_text: formData.badge_text.trim(),
            });
            alert('保存成功');
            await loadPlans();
            resetForm();
        } catch (error: any) {
            alert(error?.message || '保存失败');
        } finally {
            setSaving(false);
        }
    };

    const handleDelete = async (id: number) => {
        if (!window.confirm('确定删除这个会员计划吗？')) return;
        try {
            await deleteMembershipPlan(String(id));
            alert('删除成功');
            await loadPlans();
            if (selectedId === id) {
                resetForm();
            }
        } catch (error: any) {
            alert(error?.message || '删除失败');
        }
    };

    return (
        <Layout title="会员计划">
            <div className="membership-plans-container">
                <div className="config-toolbar">
                    <div className="toolbar-left">
                        <h3>下载会员计划</h3>
                        <p className="toolbar-desc">配置充值档位与模板下载会员的映射关系，支付成功后会自动发放对应会员。</p>
                    </div>
                </div>

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="config-content">
                        <div className="config-list">
                            <h4>已配置计划</h4>
                            {plans.length === 0 ? (
                                <div className="empty-state">暂无会员计划</div>
                            ) : (
                                plans.map((item) => (
                                    <div key={item.id} className={`config-item ${selectedId === item.id ? 'active' : ''}`}>
                                        <div className="config-info" onClick={() => handleSelect(item.id)}>
                                            <div className="config-header">
                                                <span className="config-mode">{item.title}</span>
                                                <span className={`config-status ${item.is_enabled ? 'enabled' : 'disabled'}`}>
                                                    {item.is_enabled ? '已启用' : '未启用'}
                                                </span>
                                            </div>
                                            <div className="config-preview">
                                                <div>计划编码：{item.plan_code}</div>
                                                <div>充值金额：￥{(item.recharge_amount_fen / 100).toFixed(2)}</div>
                                                <div>有效时长：{item.duration_days} 天</div>
                                                <div>下载有效期：{item.download_validity_days ?? '-'} 天</div>
                                                <div>累计可下载：{item.max_total_downloads ? item.max_total_downloads + ' 个' : '不限'}</div>
                                                <div>每日下载上限：{item.daily_download_limit ? item.daily_download_limit + ' 个' : '不限'}</div>
                                                <div>每分钟请求：{item.rate_limit_per_minute ? item.rate_limit_per_minute + ' 次' : '不限'}</div>
                                                <div>权益说明：{item.benefit_text || item.description || '模板下载会员'}</div>
                                            </div>
                                        </div>
                                        <div className="config-actions">
                                            <button className="btn-edit" onClick={() => handleSelect(item.id)}>编辑</button>
                                            <button className="btn-delete" onClick={() => handleDelete(item.id)}>删除</button>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>

                        <div className="config-form">
                            <h4>{selectedId ? '编辑会员计划' : '新建会员计划'}</h4>
                            <div className="form-group">
                                <label>计划编码 *</label>
                                <input
                                    className="form-input"
                                    value={formData.plan_code}
                                    onChange={(e) => setFormData((prev) => ({ ...prev, plan_code: e.target.value }))}
                                    placeholder="例如 download_monthly"
                                />
                            </div>
                            <div className="form-group">
                                <label>计划名称 *</label>
                                <input
                                    className="form-input"
                                    value={formData.title}
                                    onChange={(e) => setFormData((prev) => ({ ...prev, title: e.target.value }))}
                                    placeholder="例如 模板下载月卡"
                                />
                            </div>
                            <div className="form-group">
                                <label>徽标文案</label>
                                <input
                                    className="form-input"
                                    value={formData.badge_text}
                                    onChange={(e) => setFormData((prev) => ({ ...prev, badge_text: e.target.value }))}
                                    placeholder="例如 推荐开通"
                                />
                            </div>
                            <div className="form-group">
                                <label>计划说明</label>
                                <textarea
                                    className="form-input form-textarea"
                                    value={formData.description}
                                    onChange={(e) => setFormData((prev) => ({ ...prev, description: e.target.value }))}
                                    placeholder="例如 支付成功后可下载模板图片"
                                />
                            </div>
                            <div className="form-grid">
                                <div className="form-group">
                                    <label>匹配充值金额（分）*</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.recharge_amount_fen}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, recharge_amount_fen: Number(e.target.value || 0) }))}
                                    />
                                </div>
                                <div className="form-group">
                                    <label>有效天数 *</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.duration_days}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, duration_days: Number(e.target.value || 0) }))}
                                    />
                                </div>
                                <div className="form-group">
                                    <label>排序值</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.sort_order}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, sort_order: Number(e.target.value || 0) }))}
                                    />
                                </div>
                            </div>
                            <div className="form-section-title">下载限制配置</div>
                            <div className="form-grid form-grid-2col">
                                <div className="form-group">
                                    <label>下载有效期天数 *</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.download_validity_days}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, download_validity_days: Number(e.target.value || 0) }))}
                                    />
                                </div>
                                <div className="form-group">
                                    <label>累计可下载模板数（0 = 不限）</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.max_total_downloads}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, max_total_downloads: Number(e.target.value || 0) }))}
                                    />
                                </div>
                                <div className="form-group">
                                    <label>每日下载上限（0 = 不限）</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.daily_download_limit}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, daily_download_limit: Number(e.target.value || 0) }))}
                                    />
                                </div>
                                <div className="form-group">
                                    <label>频控：每分钟请求次数（0 = 不限）</label>
                                    <input
                                        className="form-input"
                                        type="number"
                                        value={formData.rate_limit_per_minute}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, rate_limit_per_minute: Number(e.target.value || 0) }))}
                                    />
                                </div>
                            </div>
                            <div className="form-group form-group-inline">
                                <label>
                                    <input
                                        type="checkbox"
                                        checked={formData.template_download_enabled}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, template_download_enabled: e.target.checked }))}
                                    />
                                    开启模板下载权益
                                </label>
                            </div>
                            <div className="form-group form-group-inline">
                                <label>
                                    <input
                                        type="checkbox"
                                        checked={formData.is_enabled}
                                        onChange={(e) => setFormData((prev) => ({ ...prev, is_enabled: e.target.checked }))}
                                    />
                                    启用该会员计划
                                </label>
                            </div>
                            <div className="form-actions">
                                <button className="btn-secondary" onClick={resetForm}>重置</button>
                                <button className="btn-primary" onClick={handleSave} disabled={saving}>{saving ? '保存中...' : '保存计划'}</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </Layout>
    );
};

export default MembershipPlans;
