import { reportPageLoadError, reportPageLoadRecovered } from '../utils/pageLoadError';
import React, { useState, useEffect, useRef } from 'react';
import { FiSave, FiTrash2, FiCheck, FiX } from 'react-icons/fi';
import {
    getRechargeConfigList,
    getRechargeConfig,
    createOrUpdateRechargeConfig,
    deleteRechargeConfig,
    type RechargeConfigItem,
    type RechargeConfigData,
} from '../api/rechargeConfig';
import './rechargeConfig.scss';

type PaymentMode = RechargeConfigItem['payment_mode'];

type RechargeForm = {
    payment_mode: PaymentMode | '';
    config_data: RechargeConfigData;
    is_enabled: boolean;
};

const emptyForm = (mode: PaymentMode | '' = ''): RechargeForm => ({
    payment_mode: mode,
    config_data: {},
    is_enabled: false,
});

const RechargeConfig: React.FC = () => {
    const [configs, setConfigs] = useState<RechargeConfigItem[]>([]);
    const [loading, setLoading] = useState(false);
    const [selectedMode, setSelectedMode] = useState<string>('');
    const [formData, setFormData] = useState<RechargeForm>(emptyForm);
    const [saving, setSaving] = useState(false);
    const [deleting, setDeleting] = useState(false);
    const [listError, setListError] = useState(false);
    const [detailStatus, setDetailStatus] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
    const [detailError, setDetailError] = useState('');
    const selection = useRef({ requestId: 0, mode: '', ready: false });
    const savingRef = useRef(false);

    const paymentModeOptions = [
        { value: 'static_qrcode', label: '静态二维码（管理员手动确认）' },
        { value: 'wechat_only', label: '仅微信收款' },
        { value: 'alipay_only', label: '仅支付宝收款' },
        { value: 'wechat_alipay', label: '微信和支付宝收款' },
    ];

    useEffect(() => {
        loadConfigs();
        return () => {
            selection.current.requestId += 1;
            selection.current.ready = false;
        };
    }, []);

    const loadConfigs = async () => {
        const requestPage = window.location.pathname;
        setLoading(true);
        setListError(false);
        try {
            const list = await getRechargeConfigList();
            setConfigs(list);
            reportPageLoadRecovered('加载充值配置列表失败', requestPage);
        } catch (error) {
            setListError(true);
            console.error('加载充值配置列表失败:', error);
            reportPageLoadError(error, '加载充值配置列表失败');
        } finally {
            setLoading(false);
        }
    };

    const resetForm = () => {
        selection.current = { requestId: selection.current.requestId + 1, mode: '', ready: false };
        setSelectedMode('');
        setFormData(emptyForm());
        setDetailStatus('idle');
        setDetailError('');
    };

    const handleSelectMode = async (mode: PaymentMode | '') => {
        if (savingRef.current) return;
        const requestId = selection.current.requestId + 1;
        selection.current = { requestId, mode, ready: false };
        setSelectedMode(mode);
        setFormData(emptyForm(mode));
        setDetailError('');
        setDetailStatus(mode ? 'loading' : 'idle');
        if (!mode) return;

        try {
            // The request layer does not retain HTTP status. Only a successful,
            // current list read can confirm that this payment mode has no record.
            const list = await getRechargeConfigList();
            if (selection.current.requestId !== requestId) return;
            setConfigs(list);
            if (list.some((item) => item.payment_mode === mode)) {
                const config = await getRechargeConfig(mode);
                if (selection.current.requestId !== requestId) return;
                if (!config || config.payment_mode !== mode) {
                    throw new Error('充值配置与所选支付方式不一致，请重试');
                }
                setFormData({
                    payment_mode: mode,
                    config_data: config.config_data || {},
                    is_enabled: config.is_enabled,
                });
            }
            selection.current.ready = true;
            setDetailStatus('ready');
        } catch (error: any) {
            if (selection.current.requestId !== requestId) return;
            setDetailError(error?.message || '加载充值配置失败');
            setDetailStatus('error');
        }
    };

    const handleSave = async () => {
        if (savingRef.current || !selection.current.ready || detailStatus !== 'ready' || selection.current.mode !== formData.payment_mode) return;
        const paymentMode = formData.payment_mode;
        if (!paymentMode) {
            alert('请选择支付方式');
            return;
        }

        const requestId = selection.current.requestId;
        savingRef.current = true;
        setSaving(true);
        try {
            await createOrUpdateRechargeConfig({ ...formData, payment_mode: paymentMode });
            alert('保存成功');
            await loadConfigs();
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
        const mode = configs.find((config) => config.id === id)?.payment_mode;
        if (!window.confirm('确定要删除这个配置吗？')) return;
        savingRef.current = true;
        setSaving(true);
        setDeleting(true);
        try {
            await deleteRechargeConfig(String(id));
            alert('删除成功');
            await loadConfigs();
            if (mode && selection.current.mode === mode) resetForm();
        } catch (error: any) {
            alert(error?.message || '删除失败');
        } finally {
            savingRef.current = false;
            setSaving(false);
            setDeleting(false);
        }
    };

    const getModeLabel = (mode: string) => {
        const option = paymentModeOptions.find(opt => opt.value === mode);
        return option?.label || mode;
    };

    return (
        <>
            <div className="recharge-config-container">
                <div className="config-toolbar">
                    <div className="toolbar-left">
                        <h3>支付方式配置</h3>
                        <p className="toolbar-desc">虚拟支付已作为小程序充值与认证支付主链路；本页仅保留旧版人工/外部收款方式作为下架过渡与紧急兜底。</p>
                    </div>
                </div>

                <div className="virtual-pay-notice">
                    <strong>虚拟支付迁移提示</strong>
                    <span>请优先在“虚拟支付监控”检查 OfferID、AppKey、消息推送 Token、发货上报和退款推送状态。这里的配置不再作为小程序默认支付入口。</span>
                </div>

                {loading ? (
                    <div className="loading-state">加载中...</div>
                ) : (
                    <div className="config-content">
                        <div className="config-list">
                            <h4>现有配置</h4>
                            {listError && <div role="alert"><p>充值配置列表未能加载，当前列表可能不完整。</p><button type="button" disabled={saving} onClick={() => void loadConfigs()}>重新加载列表</button></div>}
                            {configs.length === 0 ? (
                                listError ? null : <div className="empty-state">暂无配置</div>
                            ) : (
                                configs.map((config) => (
                                    <div key={config.id} className="config-item">
                                        <div className="config-info">
                                            <div className="config-header">
                                                <span className="config-mode">{getModeLabel(config.payment_mode)}</span>
                                                <span className={`config-status ${config.is_enabled ? 'enabled' : 'disabled'}`}>
                                                    {config.is_enabled ? (
                                                        <>
                                                            <FiCheck size={14} /> 已启用
                                                        </>
                                                    ) : (
                                                        <>
                                                            <FiX size={14} /> 未启用
                                                        </>
                                                    )}
                                                </span>
                                            </div>
                                            <div className="config-preview">
                                                {config.payment_mode === 'static_qrcode' && (
                                                    <div>
                                                        {config.config_data?.wechat_qrcode && (
                                                            <div>微信二维码: {config.config_data.wechat_qrcode.substring(0, 50)}...</div>
                                                        )}
                                                        {config.config_data?.alipay_qrcode && (
                                                            <div>支付宝二维码: {config.config_data.alipay_qrcode.substring(0, 50)}...</div>
                                                        )}
                                                    </div>
                                                )}
                                                {config.payment_mode === 'wechat_only' && (
                                                    <div>微信账号: {config.config_data?.wechat_account || '未设置'}</div>
                                                )}
                                                {config.payment_mode === 'alipay_only' && (
                                                    <div>支付宝账号: {config.config_data?.alipay_account || '未设置'}</div>
                                                )}
                                                {config.payment_mode === 'wechat_alipay' && (
                                                    <div>
                                                        <div>微信账号: {config.config_data?.wechat_account || '未设置'}</div>
                                                        <div>支付宝账号: {config.config_data?.alipay_account || '未设置'}</div>
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                        <div className="config-actions">
                                            <button className="btn-edit" disabled={saving} onClick={() => handleSelectMode(config.payment_mode)}>
                                                编辑
                                            </button>
                                            <button className="btn-delete" disabled={saving} onClick={() => handleDelete(config.id)}>
                                                <FiTrash2 size={14} />
                                            </button>
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>

                        <div className="config-form" aria-busy={detailStatus === 'loading'}>
                            <h4>{selectedMode ? '编辑配置' : '新建配置'}</h4>
                            <div className="form-group">
                                <label>支付方式 *</label>
                                <select
                                    className="form-input"
                                    value={formData.payment_mode}
                                    disabled={saving}
                                    onChange={(e) => handleSelectMode(e.target.value as PaymentMode | '')}
                                >
                                    <option value="">请选择</option>
                                    {paymentModeOptions.map(opt => (
                                        <option key={opt.value} value={opt.value}>{opt.label}</option>
                                    ))}
                                </select>
                            </div>

                            {detailStatus === 'loading' && <p role="status">正在读取支付配置...</p>}
                            {detailStatus === 'error' && (
                                <div role="alert">
                                    <p>读取失败，暂不能保存：{detailError}</p>
                                    <button type="button" className="btn-secondary" onClick={() => handleSelectMode(selectedMode as PaymentMode)}>重试</button>
                                </div>
                            )}
                            <fieldset disabled={detailStatus !== 'ready' || saving} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
                            {formData.payment_mode === 'static_qrcode' && (
                                <>
                                    <div className="form-group">
                                        <label>微信收款码 URL</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.wechat_qrcode || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, wechat_qrcode: e.target.value }
                                            }))}
                                            placeholder="https://..."
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>支付宝收款码 URL</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.alipay_qrcode || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, alipay_qrcode: e.target.value }
                                            }))}
                                            placeholder="https://..."
                                        />
                                    </div>
                                </>
                            )}

                            {formData.payment_mode === 'wechat_only' && (
                                <>
                                    <div className="form-group">
                                        <label>微信账号 *</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.wechat_account || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, wechat_account: e.target.value }
                                            }))}
                                            placeholder="微信号或手机号"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>微信昵称</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.wechat_name || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, wechat_name: e.target.value }
                                            }))}
                                            placeholder="微信昵称"
                                        />
                                    </div>
                                </>
                            )}

                            {formData.payment_mode === 'alipay_only' && (
                                <>
                                    <div className="form-group">
                                        <label>支付宝账号 *</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.alipay_account || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, alipay_account: e.target.value }
                                            }))}
                                            placeholder="支付宝账号或手机号"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>支付宝昵称</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.alipay_name || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, alipay_name: e.target.value }
                                            }))}
                                            placeholder="支付宝昵称"
                                        />
                                    </div>
                                </>
                            )}

                            {formData.payment_mode === 'wechat_alipay' && (
                                <>
                                    <div className="form-group">
                                        <label>微信账号 *</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.wechat_account || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, wechat_account: e.target.value }
                                            }))}
                                            placeholder="微信号或手机号"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>微信昵称</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.wechat_name || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, wechat_name: e.target.value }
                                            }))}
                                            placeholder="微信昵称"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>支付宝账号 *</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.alipay_account || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, alipay_account: e.target.value }
                                            }))}
                                            placeholder="支付宝账号或手机号"
                                        />
                                    </div>
                                    <div className="form-group">
                                        <label>支付宝昵称</label>
                                        <input
                                            className="form-input"
                                            value={formData.config_data.alipay_name || ''}
                                            onChange={(e) => setFormData(f => ({
                                                ...f,
                                                config_data: { ...f.config_data, alipay_name: e.target.value }
                                            }))}
                                            placeholder="支付宝昵称"
                                        />
                                    </div>
                                </>
                            )}

                            {(formData.payment_mode === 'wechat_only' || formData.payment_mode === 'alipay_only' || formData.payment_mode === 'wechat_alipay') && (
                                <div className="form-group">
                                    <label>备注说明</label>
                                    <textarea
                                        className="form-input"
                                        rows={3}
                                        value={formData.config_data.note || ''}
                                        onChange={(e) => setFormData(f => ({
                                            ...f,
                                            config_data: { ...f.config_data, note: e.target.value }
                                        }))}
                                        placeholder="支付说明或注意事项"
                                    />
                                </div>
                            )}

                            <div className="form-group form-group-inline">
                                <label>
                                    <input
                                        type="checkbox"
                                        checked={formData.is_enabled}
                                        onChange={(e) => setFormData(f => ({ ...f, is_enabled: e.target.checked }))}
                                    />
                                    启用此支付方式
                                </label>
                                <p className="form-hint">注意：只能启用一种支付方式，启用新的方式会自动禁用其他方式</p>
                            </div>

                            </fieldset>
                            <div className="form-actions">
                                <button className="btn-secondary" disabled={saving} onClick={() => { if (!savingRef.current) resetForm(); }}>
                                    取消
                                </button>
                                <button className="btn-primary" onClick={handleSave} disabled={saving || detailStatus !== 'ready' || !formData.payment_mode}>
                                    <FiSave size={16} /> {deleting ? '删除中...' : saving ? '保存中...' : '保存'}
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </>
    );
};

export default RechargeConfig;
