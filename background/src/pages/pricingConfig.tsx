import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useEffect, useState } from 'react';
import { FiAlertTriangle, FiPlus, FiRefreshCw, FiSave, FiTrash2 } from 'react-icons/fi';
import {
    getPricingConfig,
    savePricingConfig,
    type PricingCertPrice,
    type PricingConfigResponse,
    type PricingRechargePlan,
} from '../api/pricing';
import './pricingConfig.scss';

const fenToYuan = (fen: number) => (fen > 0 ? String(fen / 100) : '');
const yuanToFen = (yuan: string): number | null => {
    const trimmed = yuan.trim();
    if (trimmed === '') return null;
    if (!/^\d+(\.0{1,2})?$/.test(trimmed)) return null;
    return Math.round(Number(trimmed) * 100);
};

interface PlanRow {
    title: string;
    amountYuan: string;
    recommended: boolean;
}

interface CertRow {
    product_id: string;
    title: string;
    amountYuan: string;
}

const PricingConfig: React.FC = () => {
    const [loading, setLoading] = useState(false);
    const [saving, setSaving] = useState(false);
    const [source, setSource] = useState<'db' | 'default'>('default');
    const [coinRatio, setCoinRatio] = useState(10);
    const [membershipNote, setMembershipNote] = useState('');
    const [plans, setPlans] = useState<PlanRow[]>([]);
    const [certs, setCerts] = useState<CertRow[]>([]);
    const [dirty, setDirty] = useState(false);

    const hydrate = (data: PricingConfigResponse) => {
        setSource(data.source);
        setCoinRatio(data.coin_ratio);
        setMembershipNote(data.membership_note || '');
        setPlans((data.recharge_plans || []).map((plan: PricingRechargePlan) => ({
            title: plan.title,
            amountYuan: fenToYuan(plan.amount_fen),
            recommended: !!plan.recommended,
        })));
        setCerts((data.cert_prices || []).map((cert: PricingCertPrice) => ({
            product_id: cert.product_id,
            title: data.cert_title_map?.[cert.product_id] || cert.product_id,
            amountYuan: fenToYuan(cert.price_fen),
        })));
        setDirty(false);
    };

    const loadConfig = async () => {
        setLoading(true);
        try {
            const recoveryPage = window.location.pathname;
            hydrate(await getPricingConfig());
            reportPageLoadRecovered("加载定价配置失败", recoveryPage);
        } catch (error) {
            console.error('加载定价配置失败:', error);
            reportPageLoadError(error, '加载定价配置失败');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        void loadConfig();
    }, []);

    const updatePlan = (index: number, patch: Partial<PlanRow>) => {
        setPlans((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
        setDirty(true);
    };

    const removePlan = (index: number) => {
        setPlans((rows) => rows.filter((_, i) => i !== index));
        setDirty(true);
    };

    const addPlan = () => {
        setPlans((rows) => [...rows, { title: '', amountYuan: '', recommended: false }]);
        setDirty(true);
    };

    const updateCert = (productId: string, amountYuan: string) => {
        setCerts((rows) => rows.map((row) => (row.product_id === productId ? { ...row, amountYuan } : row)));
        setDirty(true);
    };

    const handleSave = async () => {
        // 组装并本地预校验（服务端为最终校验方）
        const planPayload: PricingRechargePlan[] = [];
        for (let i = 0; i < plans.length; i++) {
            const row = plans[i];
            if (!row.title.trim()) {
                alert(`第 ${i + 1} 个档位名称不能为空`);
                return;
            }
            const fen = yuanToFen(row.amountYuan);
            if (fen === null || fen < 100) {
                alert(`档位「${row.title}」金额需为 ≥1 元的整数元`);
                return;
            }
            planPayload.push({ title: row.title.trim(), amount_fen: fen, recommended: row.recommended });
        }
        if (planPayload.length < 1) {
            alert('至少保留 1 个充值档位');
            return;
        }
        const certPayload: PricingCertPrice[] = [];
        for (const row of certs) {
            const fen = yuanToFen(row.amountYuan);
            if (fen === null || fen < 100) {
                alert(`认证道具「${row.title}」价格需为 ≥1 元的整数元`);
                return;
            }
            certPayload.push({ product_id: row.product_id, price_fen: fen });
        }
        const planSummary = planPayload
            .map((plan) => `${plan.title} ¥${plan.amount_fen / 100}（${(plan.amount_fen * coinRatio / 100).toLocaleString('zh-CN')} 灵石）${plan.recommended ? ' [推荐]' : ''}`)
            .join('\n');
        const certSummary = certPayload
            .map((cert) => `${cert.product_id === 'cert_designer' ? '设计师认证' : '企业认证'} ¥${cert.price_fen / 100}`)
            .join('\n');
        if (!window.confirm(`确认保存以下定价并立即生效？\n\n【充值档位】\n${planSummary}\n\n【认证道具】\n${certSummary}\n\n注意：档位灵石由兑换比例（1 元 = ${coinRatio} 灵石）折算；改价立即作用于小程序下单。`)) {
            return;
        }
        setSaving(true);
        try {
            await savePricingConfig({ recharge_plans: planPayload, cert_prices: certPayload });
            alert('定价已保存并生效（最长 30 秒内全量生效）。');
            await loadConfig();
        } catch (error: any) {
            console.error('保存定价配置失败:', error);
            alert(error?.message || '保存定价配置失败');
        } finally {
            setSaving(false);
        }
    };

    return (
        <div className="pricing-config-container">
            <div className="pricing-toolbar">
                <div>
                    <h3>定价配置</h3>
                    <p>充值档位与认证道具价格，保存后立即生效，无需发版。</p>
                </div>
                <div className="pricing-toolbar-actions">
                    <span className={`source-badge ${source === 'db' ? 'is-db' : ''}`}>
                        {source === 'db' ? '后台配置生效中' : '默认值（尚未在后台配置过）'}
                    </span>
                    <button className="btn-secondary" onClick={() => void loadConfig()} disabled={loading}>
                        <FiRefreshCw />
                        {loading ? '加载中...' : '刷新'}
                    </button>
                    <button className="btn-primary" onClick={() => void handleSave()} disabled={saving || loading}>
                        <FiSave />
                        {saving ? '保存中...' : dirty ? '保存并生效' : '保存'}
                    </button>
                </div>
            </div>

            <div className="pricing-note-card">
                <FiAlertTriangle />
                <div>
                    <strong>兑换比例：1 元 = {coinRatio} 灵石</strong>（只读）
                    <span>该比例必须与微信小程序后台「虚拟支付 → 代币兑换比例」完全一致，因此不在后台提供修改；档位灵石由系统按此比例自动折算，不可手填，否则会出现"页面标价与微信实付不符"。</span>
                    {membershipNote && <span>{membershipNote}</span>}
                </div>
            </div>

            <section className="pricing-section">
                <div className="pricing-section-head">
                    <h4>充值档位（1~10 个）</h4>
                    <button className="btn-secondary" onClick={addPlan} disabled={plans.length >= 10}>
                        <FiPlus />
                        添加档位
                    </button>
                </div>
                <div className="admin-table-region"><table className="pricing-table">
                    <thead>
                        <tr>
                            <th>档位名称</th>
                            <th>金额（元）</th>
                            <th>到账灵石（自动折算）</th>
                            <th>推荐标记</th>
                            <th>操作</th>
                        </tr>
                    </thead>
                    <tbody>
                        {plans.length === 0 ? (
                            <tr><td colSpan={5} className="empty-state">暂无档位，请添加</td></tr>
                        ) : plans.map((row, index) => {
                            const fen = yuanToFen(row.amountYuan);
                            const stones = fen && fen > 0 ? Math.floor((fen * coinRatio) / 100).toLocaleString('zh-CN') : '—';
                            return (
                                <tr key={index}>
                                    <td>
                                        <input
                                            className="pricing-input"
                                            value={row.title}
                                            maxLength={16}
                                            placeholder="如：普通会员"
                                            onChange={(event) => updatePlan(index, { title: event.target.value })}
                                        />
                                    </td>
                                    <td>
                                        <input
                                            className="pricing-input pricing-input-amount"
                                            value={row.amountYuan}
                                            placeholder="如：50"
                                            inputMode="decimal"
                                            onChange={(event) => updatePlan(index, { amountYuan: event.target.value })}
                                        />
                                    </td>
                                    <td className="stones-cell">{stones}</td>
                                    <td>
                                        <label className="recommend-toggle">
                                            <input
                                                type="checkbox"
                                                checked={row.recommended}
                                                onChange={(event) => updatePlan(index, { recommended: event.target.checked })}
                                            />
                                            {row.recommended ? '推荐' : '—'}
                                        </label>
                                    </td>
                                    <td>
                                        <button className="btn-mini danger" onClick={() => removePlan(index)} disabled={plans.length <= 1} title={plans.length <= 1 ? '至少保留 1 个档位' : '删除该档位'}>
                                            <FiTrash2 />
                                        </button>
                                    </td>
                                </tr>
                            );
                        })}
                    </tbody>
                </table></div>
            </section>

            <section className="pricing-section">
                <div className="pricing-section-head">
                    <h4>认证道具价格</h4>
                </div>
                <div className="admin-table-region"><table className="pricing-table">
                    <thead>
                        <tr>
                            <th>道具</th>
                            <th>价格（元）</th>
                        </tr>
                    </thead>
                    <tbody>
                        {certs.map((row) => (
                            <tr key={row.product_id}>
                                <td>{row.title}<span className="product-id">（{row.product_id}）</span></td>
                                <td>
                                    <input
                                        className="pricing-input pricing-input-amount"
                                        value={row.amountYuan}
                                        placeholder="如：299"
                                        inputMode="decimal"
                                        onChange={(event) => updateCert(row.product_id, event.target.value)}
                                    />
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table></div>
            </section>
        </div>
    );
};

export default PricingConfig;
