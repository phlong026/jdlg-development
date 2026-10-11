import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useState, useRef } from 'react';
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
    const [deleting, setDeleting] = useState(false);
    const [listError, setListError] = useState(false);
    const [selectedId, setSelectedId] = useState<number>(0);
    const [formData, setFormData] = useState(defaultForm);
    const [detailStatus, setDetailStatus] = useState<'loading' | 'ready' | 'error'>('ready');
    const [detailError, setDetailError] = useState('');
    const selection = useRef({ requestId: 0, id: 0, ready: true });
    const savingRef = useRef(false);

    useEffect(() => {
        // React StrictMode replays setup after cleanup on the same initial form.
        selection.current.ready = detailStatus === 'ready';
        loadPlans();
        return () => {
            selection.current.requestId += 1;
            selection.current.ready = false;
        };
    }, []);

    const loadPlans = async () => {
        const requestPage = window.location.pathname;
        setLoading(true);
        setListError(false);
        try {
            const list = await getMembershipPlanList();
            setPlans(list);
            reportPageLoadRecovered('加载会员计划失败', requestPage);
        } catch (error) {
            setListError(true);
            console.error('加载会员计划失败:', error);
            reportPageLoadError(error, '加载会员计划失败');
        } finally {
            setLoading(false);
        }
    };

    const handleSelect = async (id: number) => {
        if (savingRef.current) return;
        const requestId = selection.current.requestId + 1;
        selection.current = { requestId, id, ready: false };
        setSelectedId(id);
        setFormData({ ...defaultForm, id });
        setDetailStatus('loading');
        setDetailError('');
        try {
            const item = await getMembershipPlan(String(id));
            if (selection.current.requestId !== requestId) return;
            if (!item || item.id !== id) throw new Error('会员计划与所选记录不一致，请重试');
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
            selection.current.ready = true;
            setDetailStatus('ready');
        } catch (error: any) {
            if (selection.current.requestId !== requestId) return;
            setDetailError(error?.message || '加载会员计划详情失败');
            setDetailStatus('error');
        }
    };

    const resetForm = () => {
        selection.current = { requestId: selection.current.requestId + 1, id: 0, ready: true };
        setSelectedId(0);
        setFormData(defaultForm);
        setDetailStatus('ready');
        setDetailError('');
    };

    const handleSave = async () => {
        if (savingRef.current || !selection.current.ready || detailStatus !== 'ready' || selection.current.id !== formData.id) return;
        if (!formData.plan_code.trim() || !formData.title.trim()) {
            alert('计划编码和计划名称不能为空');
            return;
        }
        if (formData.recharge_amount_fen <= 0 || formData.duration_days <= 0) {
            alert('充值金额和有效天数必须大于 0');
            return;
        }
        const requestId = selection.current.requestId;
        savingRef.current = true;
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
            if (selection.current.requestId === requestId) resetForm();
        } catch (error: any) {
            alert(error?.message || '保存失败');
        } finally {
            savingRef.current = false;
            setSaving(false);
        }
    };

    const handleDelete = async (id: number) => {
        if (savingRef.current) return;
        if (!window.confirm('确定删除这个会员计划吗？')) return;
        savingRef.current = true;
        setSaving(true);
        setDeleting(true);
        try {
            await deleteMembershipPlan(String(id));
            alert('删除成功');
            await loadPlans();
            if (selection.current.id === id) {
                resetForm();
            }
        } catch (error: any) {
            alert(error?.message || '删除失败');
        } finally {
            savingRef.current = false;
            setSaving(false);
            setDeleting(false);
        }
    };

    return (
        <>
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
                            {listError && <div role="alert"><p>会员计划未能加载，当前列表可能不完整。</p><button type="button" disabled={saving} onClick={() => void loadPlans()}>重新加载列表</button></div>}
                            {plans.length === 0 ? (
                                listError ? null : <div className="empty-state">暂无会员计划</div>
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
                                            <button className="btn-edit" disabled={saving} onClick={() => handleSelect(item.id)}>编辑</button>
                                            <button className="btn-delete" disabled={saving} onClick={() => handleDelete(item.id)}>删除</button>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>

                        <div className="config-form" aria-busy={detailStatus === 'loading'}>
                            <h4>{selectedId ? '编辑会员计划' : '新建会员计划'}</h4>
                            {detailStatus === 'loading' && <p role="status">正在读取会员计划...</p>}
                            {detailStatus === 'error' && (
                                <div role="alert">
                                    <p>读取失败，暂不能保存：{detailError}</p>
                                    <button type="button" className="btn-secondary" onClick={() => handleSelect(selectedId)}>重试</button>
                                </div>
                            )}
                            <fieldset disabled={detailStatus !== 'ready' || saving} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
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
                            </fieldset>
                            <div className="form-actions">
                                <button className="btn-secondary" disabled={saving} onClick={() => { if (!savingRef.current) resetForm(); }}>重置</button>
                                <button className="btn-primary" onClick={handleSave} disabled={saving || detailStatus !== 'ready'}>{deleting ? '删除中...' : saving ? '保存中...' : '保存计划'}</button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default MembershipPlans;
