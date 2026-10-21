import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import CustomerLeads from './customerLeads';
import CustomerReportForm from './customerReportForm';
import { useManagementSession } from '../component/ManagementSession';
import './customerManagement.scss';
import './customerResponsive.scss';
import './salesModule.scss';
import './salesLeadsSkeleton.scss';

/**
 * 线索入口：列表与只读规则全部来自 customerLeads，这里只放标题栏。
 * 「新增线索」直接写入客户档案（与客户页的报备是同一个接口），线索的跟进也在客户档案里做。
 */
const CustomerLeadsPage: React.FC = () => {
    const session = useManagementSession();
    const canReport = !session.crmLoading && !!session.crmContext;
    const [reporting, setReporting] = useState(false);
    const [createdId, setCreatedId] = useState('');

    return (
        <div className="sx-leads">
            <header className="sx-page-head">
                <h1>线索</h1>
                <span className="sx-page-head-gap" aria-hidden />
                {canReport && <button type="button" className="sx-lead-add" onClick={() => { setCreatedId(''); setReporting(true); }}>＋ 新增线索</button>}
            </header>
            {createdId && (
                <p className="sx-lead-created" role="status">
                    已新建客户档案（线索列表只显示小程序留资），可在客户档案里分配和跟进。
                    <Link to={`/customer-management?tab=customers&customer=${encodeURIComponent(createdId)}`}>打开客户档案 ›</Link>
                </p>
            )}
            <CustomerLeads />
            {reporting && (
                <div className="crm-page sx-lead-report">
                    <div className="crm-overlay">
                        <section className="crm-drawer" role="dialog" aria-modal="true" aria-label="新增线索">
                            <header><h2>新增线索</h2></header>
                            <div className="crm-drawer-body">
                                <CustomerReportForm cancel={() => setReporting(false)} saved={(customer) => { setReporting(false); setCreatedId(customer.id); }} />
                            </div>
                        </section>
                    </div>
                </div>
            )}
        </div>
    );
};

export default CustomerLeadsPage;
