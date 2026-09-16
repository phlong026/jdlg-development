import React, { useEffect, useState } from 'react';
import { FiUser, FiLock, FiEye, FiEyeOff } from 'react-icons/fi';
import { useNavigate } from 'react-router-dom';
import { login } from '../api/auth';
import { API_BASE_URL } from '../config/api';
import './login.scss';

const REMEMBERED_LOGIN_KEY = 'background_remembered_login';

// 记住密码：按用户明确要求保留原有的读取、回填、保存与取消逻辑
// （2026-09-16 上线评审中再次确认接受明文缓存的本机暴露风险）。
const Login: React.FC = () => {
    const navigate = useNavigate();
    const [account, setAccount] = useState('');
    const [password, setPassword] = useState('');
    const [rememberPassword, setRememberPassword] = useState(false);
    const [showPassword, setShowPassword] = useState(false);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [wecomEnabled,setWecomEnabled] = useState(false);
    useEffect(()=>{
        const controller=new AbortController();
        fetch(API_BASE_URL+'/api/v1/management/wecom/config',{signal:controller.signal,credentials:'include'}).then(r=>r.json()).then(r=>setWecomEnabled(r.code===0&&r.data?.enabled===true)).catch((err)=>{console.warn('企微登录配置探测失败，已隐藏入口',err)});
        const reason=new URLSearchParams(window.location.search).get('wecom_error');
        const messages:Record<string,string>={expired:'企微登录已过期或当前浏览器不匹配，请重新发起。',identity:'企微成员校验失败，请确认使用本企业已激活账号，或联系管理员检查应用权限。',not_enabled:'此企微成员尚未绑定已启用的后台账号，请联系管理员开通。',unavailable:'企微登录暂不可用，请稍后重试或使用账号登录。'};
        if(reason){setError(messages[reason]||'企微登录失败，请重新发起。');window.history.replaceState(null,'',window.location.pathname)}
        return()=>controller.abort();
    },[]);

    useEffect(() => {
        try {
            const saved = window.localStorage.getItem(REMEMBERED_LOGIN_KEY);
            if (!saved) return;

            const parsed = JSON.parse(saved) as { account?: string; password?: string };
            setAccount(parsed.account || '');
            setPassword(parsed.password || '');
            setRememberPassword(!!parsed.account && !!parsed.password);
        } catch {
            window.localStorage.removeItem(REMEMBERED_LOGIN_KEY);
        }
    }, []);

    const handleAccountChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        setAccount(e.target.value);
        setError('');
    };

    const handlePasswordChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        setPassword(e.target.value);
        setError('');
    };

    const handleRememberChange = (e: React.ChangeEvent<HTMLInputElement>) => {
        const checked = e.target.checked;
        setRememberPassword(checked);
        if (!checked) {
            window.localStorage.removeItem(REMEMBERED_LOGIN_KEY);
        }
    };

    const handleLogin = async (e: React.FormEvent) => {
        e.preventDefault();
        if(loading)return;
        setError('');

        if (!account.trim() || !password) {
            setError('请输入账号和密码');
            return;
        }

        setLoading(true);
        try {
            const normalizedAccount = account.trim();
            await login({
                username: normalizedAccount,
                password: password,
            });
            if (rememberPassword) {
                window.localStorage.setItem(
                    REMEMBERED_LOGIN_KEY,
                    JSON.stringify({
                        account: normalizedAccount,
                        password: password,
                    })
                );
            } else {
                window.localStorage.removeItem(REMEMBERED_LOGIN_KEY);
            }
            navigate('/dashboard');
        } catch (err: any) {
            setError(err.message || '登录失败，请检查账号和密码');
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className="login-page">
            <div className="login-container">
                {/* 右侧登录表单区域 */}
                <div className="form-section">
                    <div className="form-content">
                        <div className="login-brand">客户管理</div><h1 className="welcome-title">欢迎登录</h1><p className="login-subtitle">记录客户需求，跟进每一次机会</p>

                        <form onSubmit={handleLogin} className="login-form">
                            {/* 错误提示 */}
                            {error && (
                                <div role="alert" className="error-message" style={{
                                    color: '#ef4444',
                                    marginBottom: '16px',
                                    fontSize: '14px',
                                    textAlign: 'center'
                                }}>
                                    {error}
                                </div>
                            )}

                            {/* 账号输入框 */}
                            <div className="form-group">
                                <label className="form-label" htmlFor="login-account">账号</label>
                                <div className="input-wrapper email-wrapper">
                                    <FiUser className="input-icon email-icon" />
                                    <input
                                        id="login-account"
                                        type="text"
                                        className="form-input email-input"
                                        placeholder="请输入账号"
                                        value={account}
                                        onChange={handleAccountChange}
                                        disabled={loading}
                                        autoComplete="username"
                                    />
                                </div>
                            </div>

                            {/* 密码输入框 */}
                            <div className="form-group">
                                <label className="form-label" htmlFor="login-password">密码</label>
                                <div className="input-wrapper password-wrapper">
                                    <FiLock className="input-icon lock-icon-input" />
                                    <input
                                        id="login-password"
                                        type={showPassword ? "text" : "password"}
                                        className="form-input password-input"
                                        placeholder="请输入密码"
                                        value={password}
                                        onChange={handlePasswordChange}
                                        disabled={loading}
                                        autoComplete="current-password"
                                    />
                                    <button type="button" className="password-toggle" aria-label={showPassword ? "隐藏密码" : "显示密码"} aria-pressed={showPassword} disabled={loading} onClick={() => setShowPassword(v => !v)}>{showPassword ? <FiEyeOff/> : <FiEye/>}</button>
                                </div>
                            </div>

                            <label className="remember-row">
                                <input
                                    type="checkbox"
                                    checked={rememberPassword}
                                    onChange={handleRememberChange}
                                    disabled={loading}
                                />
                                <span className="remember-box" aria-hidden="true"></span>
                                <span className="remember-text">记住密码</span>
                            </label>

                            {/* 登录按钮 */}
                            <div className="form-actions">
                                <button type="submit" className="btn btn-login" disabled={loading || !account.trim() || !password}>
                                    {loading ? '登录中...' : '登录'}
                                </button>
                            </div>
                        </form>
                        {wecomEnabled&&<div className="form-actions"><a className="btn btn-login" href={API_BASE_URL+'/api/v1/management/wecom/start'}>企业微信登录</a></div>}
                        {wecomEnabled&&<p style={{fontSize:13,marginTop:12}}>企微内打开可授权登录，电脑可扫码。角色由管理员预先配置。</p>}
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Login;
