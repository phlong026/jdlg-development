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
    FiTrendingUp,
} from 'react-icons/fi';

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
const Opportunities = lazy(() => import('../pages/opportunities'));
const CustomerManagement = lazy(() => import('../pages/customerManagement'));
const ReportCenter = lazy(() => import('../pages/reportCenter'));
const AITaskCenter = lazy(() => import('../pages/aiTaskCenter'));
const AIOverview = lazy(() => import('../pages/aiOverview'));
const AIPricing = lazy(() => import('../pages/aiPricing'));
const AIContentAudits = lazy(() => import('../pages/aiContentAudits'));
const AIConfig = lazy(() => import('../pages/ai-config'));
const OSS = lazy(() => import('../pages/oss'));
const Templates = lazy(() => import('../pages/templates'));
const Inspirations = lazy(() => import('../pages/inspirations'));
const OrderCenter = lazy(() => import('../pages/orderCenter'));
const VirtualPay = lazy(() => import('../pages/virtualPay'));
const UtilityTools = lazy(() => import('../pages/utilityTools'));
const AITools = lazy(() => import('../pages/aiTools'));
const RechargeConfig = lazy(() => import('../pages/rechargeConfig'));
const PricingConfig = lazy(() => import('../pages/pricingConfig'));
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
    { path: '/customer-management', title: '客户管理', element: withSuspense(<CustomerManagement />), icon: FiUsers },
    {
        path: '/login',
        title: '登录',
        element: withSuspense(<Login />),
        icon: FiMonitor,
    },
    {
        path: '/dashboard',
        title: '后台总控台',
        element: withSuspense(<Dashboard />),
        icon: FiHome,
    },
    {
        path: '/users',
        title: '用户账号',
        element: withSuspense(<Users />),
        icon: FiUsers,
    },
    {
        path: '/user-workbench',
        title: '用户详情',
        element: withSuspense(<UserWorkbench />),
        icon: FiCompass,
    },
    {
        path: '/designer-center',
        title: '设计师中心',
        element: withSuspense(<DesignerCenter />),
        icon: FiUsers,
    },
    {
        path: '/certification',
        title: '资质认证与审核',
        element: withSuspense(<Certification />),
        icon: FiUserCheck,
    },
    {
        path: '/logs',
        title: '操作审计',
        element: withSuspense(<Logs />),
        icon: FiFileText,
    },
    {
        path: '/distribution',
        title: '分销邀请中心',
        element: withSuspense(<Distribution />),
        icon: FiShare2,
    },
    {
        path: '/content-analytics',
        title: '内容运营分析',
        element: withSuspense(<ContentAnalytics />),
        icon: FiActivity,
    },
    {
        path: '/risk-control',
        title: '风控台',
        element: withSuspense(<RiskControl />),
        icon: FiShield,
    },
    {
        path: '/support-tickets',
        title: '异常工单中心',
        element: withSuspense(<SupportTickets />),
        icon: FiFileText,
    },
    {
        path: '/customer-leads',
        title: '销售线索',
        element: withSuspense(<CustomerLeads />),
        icon: FiFileText,
    },
    {
        path: '/opportunities',
        title: '商机',
        element: withSuspense(<Opportunities />),
        icon: FiTrendingUp,
    },
    {
        path: '/report-center',
        title: '报表导出中心',
        element: withSuspense(<ReportCenter />),
        icon: FiBarChart2,
    },
    {
        path: '/ai-overview',
        title: 'AI运营总览',
        element: withSuspense(<AIOverview />),
        icon: FiActivity,
    },
    {
        path: '/ai-pricing',
        title: 'AI定价与供应商',
        element: withSuspense(<AIPricing />),
        icon: FiDollarSign,
    },
    {
        path: '/ai-content-audits',
        title: 'AI内容抽检',
        element: withSuspense(<AIContentAudits />),
        icon: FiImage,
    },
    {
        path: '/ai-tasks',
        title: 'AI任务中心',
        element: withSuspense(<AITaskCenter />),
        icon: FiActivity,
    },
    {
        path: '/ai-config',
        title: 'AI配置',
        element: withSuspense(<AIConfig />),
        icon: FiSettings,
    },
    {
        path: '/oss',
        title: 'OSS管理',
        element: withSuspense(<OSS />),
        icon: FiCloud,
    },
    {
        path: '/templates',
        title: '模板广场管理',
        element: withSuspense(<Templates />),
        icon: FiLayers,
    },
    {
        path: '/inspirations',
        title: '灵感素材审核',
        element: withSuspense(<Inspirations />),
        icon: FiImage,
    },
    {
        path: '/recharge',
        title: '订单中心',
        element: withSuspense(<OrderCenter />),
        icon: FiDollarSign,
    },
    {
        path: '/virtualpay',
        title: '虚拟支付监控',
        element: withSuspense(<VirtualPay />),
        icon: FiDollarSign,
    },
    {
        path: '/utility-tools',
        title: '实用工具管理',
        element: withSuspense(<UtilityTools />),
        icon: FiFileText,
    },
    {
        path: '/ai-tools',
        title: 'AI工具管理',
        element: withSuspense(<AITools />),
        icon: FiLayers,
    },
    {
        path: '/recharge-config',
        title: '充值配置',
        element: withSuspense(<RechargeConfig />),
        icon: FiDollarSign,
    },
    {
        path: '/pricing-config',
        title: '定价配置',
        element: withSuspense(<PricingConfig />),
        icon: FiDollarSign,
    },
    {
        path: '/membership-plans',
        title: '会员计划',
        element: withSuspense(<MembershipPlans />),
        icon: FiDollarSign,
    },
    {
        path: '/membership-operations',
        title: '用户会员运营',
        element: withSuspense(<MembershipOperations />),
        icon: FiUserCheck,
    },
];

export default routes;
