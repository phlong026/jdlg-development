import React, { Suspense, lazy } from 'react';
import type { FeatherIcon as IconType } from '../compat/react-icons-fi';
import {
    FiActivity,
    FiHome,
    FiUsers,
    FiFileText,
    FiShare2,
    FiCompass,
    FiSettings,
    FiCloud,
    FiLayers,
    FiDollarSign,
    FiMonitor,
    FiUserCheck,
    FiImage,
    FiShield,
    FiBarChart2,
} from 'react-icons/fi';
import ProtectedRoute from '../component/ProtectedRoute';

const Login = lazy(() => import('../pages/login'));
const Dashboard = lazy(() => import('../pages/dashboard'));
const Users = lazy(() => import('../pages/users'));
const Logs = lazy(() => import('../pages/logs'));
const DesignerCenter = lazy(() => import('../pages/designerCenter'));
const Distribution = lazy(() => import('../pages/distribution'));
const ContentAnalytics = lazy(() => import('../pages/contentAnalytics'));
const RiskControl = lazy(() => import('../pages/riskControl'));
const SupportTickets = lazy(() => import('../pages/supportTickets'));
const CustomerLeads = lazy(() => import('../pages/customerLeads'));
const CustomerManagement = lazy(() => import('../pages/customerManagement'));
const ReportCenter = lazy(() => import('../pages/reportCenter'));
const AITaskCenter = lazy(() => import('../pages/aiTaskCenter'));
const AIConfig = lazy(() => import('../pages/ai-config'));
const OSS = lazy(() => import('../pages/oss'));
const Templates = lazy(() => import('../pages/templates'));
const Inspirations = lazy(() => import('../pages/inspirations'));
const OrderCenter = lazy(() => import('../pages/orderCenter'));
const VirtualPay = lazy(() => import('../pages/virtualPay'));
const UtilityTools = lazy(() => import('../pages/utilityTools'));
const AITools = lazy(() => import('../pages/aiTools'));
const RechargeConfig = lazy(() => import('../pages/rechargeConfig'));
const MembershipPlans = lazy(() => import('../pages/membershipPlans'));
const MembershipOperations = lazy(() => import('../pages/membershipOperations'));
const Certification = lazy(() => import('../pages/certification'));
const UserWorkbench = lazy(() => import('../pages/userWorkbench'));

const withSuspense = (node: React.ReactNode) => (
    <Suspense fallback={<div className="route-loading">页面加载中...</div>}>
        {node}
    </Suspense>
);

export interface AppRoute {
    path: string;
    element: React.ReactNode;
    title?: string;
    icon?: IconType;
    children?: AppRoute[];
}

export const routes: AppRoute[] = [
    { path: '/customer-management', title: '客户管理', element: withSuspense(<ProtectedRoute><CustomerManagement /></ProtectedRoute>), icon: FiUsers },
    {
        path: '/login',
        title: '登录',
        element: withSuspense(<Login />),
        icon: FiMonitor,
    },
    {
        path: '/dashboard',
        title: '后台总控台',
        element: withSuspense(<ProtectedRoute><Dashboard /></ProtectedRoute>),
        icon: FiHome,
    },
    {
        path: '/users',
        title: '用户管理',
        element: withSuspense(<ProtectedRoute><Users /></ProtectedRoute>),
        icon: FiUsers,
    },
    {
        path: '/user-workbench',
        title: '用户360工作台',
        element: withSuspense(<ProtectedRoute><UserWorkbench /></ProtectedRoute>),
        icon: FiCompass,
    },
    {
        path: '/designer-center',
        title: '设计师中心',
        element: withSuspense(<ProtectedRoute><DesignerCenter /></ProtectedRoute>),
        icon: FiUsers,
    },
    {
        path: '/certification',
        title: '资质认证与审核',
        element: withSuspense(<ProtectedRoute><Certification /></ProtectedRoute>),
        icon: FiUserCheck,
    },
    {
        path: '/logs',
        title: '日志管理',
        element: withSuspense(<ProtectedRoute><Logs /></ProtectedRoute>),
        icon: FiFileText,
    },
    {
        path: '/distribution',
        title: '分销邀请中心',
        element: withSuspense(<ProtectedRoute><Distribution /></ProtectedRoute>),
        icon: FiShare2,
    },
    {
        path: '/content-analytics',
        title: '内容运营分析',
        element: withSuspense(<ProtectedRoute><ContentAnalytics /></ProtectedRoute>),
        icon: FiActivity,
    },
    {
        path: '/risk-control',
        title: '风控台',
        element: withSuspense(<ProtectedRoute><RiskControl /></ProtectedRoute>),
        icon: FiShield,
    },
    {
        path: '/support-tickets',
        title: '异常工单中心',
        element: withSuspense(<ProtectedRoute><SupportTickets /></ProtectedRoute>),
        icon: FiFileText,
    },
    {
        path: '/customer-leads',
        title: '客服线索工作台',
        element: withSuspense(<ProtectedRoute><CustomerLeads /></ProtectedRoute>),
        icon: FiFileText,
    },
    {
        path: '/report-center',
        title: '报表导出中心',
        element: withSuspense(<ProtectedRoute><ReportCenter /></ProtectedRoute>),
        icon: FiBarChart2,
    },
    {
        path: '/ai-tasks',
        title: 'AI任务中心',
        element: withSuspense(<ProtectedRoute><AITaskCenter /></ProtectedRoute>),
        icon: FiActivity,
    },
    {
        path: '/ai-config',
        title: 'AI配置',
        element: withSuspense(<ProtectedRoute><AIConfig /></ProtectedRoute>),
        icon: FiSettings,
    },
    {
        path: '/oss',
        title: 'OSS管理',
        element: withSuspense(<ProtectedRoute><OSS /></ProtectedRoute>),
        icon: FiCloud,
    },
    {
        path: '/templates',
        title: '模板广场管理',
        element: withSuspense(<ProtectedRoute><Templates /></ProtectedRoute>),
        icon: FiLayers,
    },
    {
        path: '/inspirations',
        title: '灵感素材审核',
        element: withSuspense(<ProtectedRoute><Inspirations /></ProtectedRoute>),
        icon: FiImage,
    },
    {
        path: '/recharge',
        title: '订单中心',
        element: withSuspense(<ProtectedRoute><OrderCenter /></ProtectedRoute>),
        icon: FiDollarSign,
    },
    {
        path: '/virtualpay',
        title: '虚拟支付监控',
        element: withSuspense(<ProtectedRoute><VirtualPay /></ProtectedRoute>),
        icon: FiDollarSign,
    },
    {
        path: '/utility-tools',
        title: '实用工具管理',
        element: withSuspense(<ProtectedRoute><UtilityTools /></ProtectedRoute>),
        icon: FiFileText,
    },
    {
        path: '/ai-tools',
        title: 'AI工具管理',
        element: withSuspense(<ProtectedRoute><AITools /></ProtectedRoute>),
        icon: FiLayers,
    },
    {
        path: '/recharge-config',
        title: '充值配置',
        element: withSuspense(<ProtectedRoute><RechargeConfig /></ProtectedRoute>),
        icon: FiDollarSign,
    },
    {
        path: '/membership-plans',
        title: '会员计划',
        element: withSuspense(<ProtectedRoute><MembershipPlans /></ProtectedRoute>),
        icon: FiDollarSign,
    },
    {
        path: '/membership-operations',
        title: '用户会员运营',
        element: withSuspense(<ProtectedRoute><MembershipOperations /></ProtectedRoute>),
        icon: FiUserCheck,
    },
];

export default routes;
