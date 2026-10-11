import React from 'react';
import { Link } from 'react-router-dom';
import { FiTrendingUp } from 'react-icons/fi';
import './opportunities.scss';

/**
 * 商机（占位）：销售线索分组里「客户 · 线索 · 商机」三类之一。
 * 目前系统里没有商机的数据与定义（线索表只有 新线索 / 跟进中 / 已成交 / 无效 四个状态），
 * 所以这里不展示任何数字，只说明状态；定义清楚后再做。
 */
const Opportunities: React.FC = () => (
    <div className="opportunities-page">
        <div className="opportunities-card">
            <FiTrendingUp className="opportunities-icon" aria-hidden />
            <h2>商机暂未启用</h2>
            <p>商机是线索转化之后、带有预计金额与跟进阶段的销售机会。系统目前只有线索，还没有商机的数据与流程，因此这里暂不显示任何数字。</p>
            <Link to="/customer-leads">返回线索</Link>
        </div>
    </div>
);

export default Opportunities;
