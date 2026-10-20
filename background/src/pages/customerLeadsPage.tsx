import React from 'react';
import CustomerLeads from './customerLeads';
import './salesLeadsSkeleton.scss';

/** 线索入口：只读行为全部来自 customerLeads，这里只加新的页面标题骨架与样式。 */
const CustomerLeadsPage: React.FC = () => (
    <div className="sx-leads">
        <header className="sx-page-head"><h1>线索</h1></header>
        <CustomerLeads />
    </div>
);

export default CustomerLeadsPage;
