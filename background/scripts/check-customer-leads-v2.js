const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');
const read = (file) => fs.readFileSync(path.join(root, file), 'utf8');

const apiConfig = read('src/config/api.ts');
const api = read('src/api/customerLeads.ts');
const page = read('src/pages/customerLeads.tsx');
const styles = read('src/pages/customerLeads.scss');
const route = fs.readFileSync(path.resolve(root, '../service/route/customer_service.go'), 'utf8');
const model = fs.readFileSync(path.resolve(root, '../service/model/customer_service.go'), 'utf8');

assert.match(apiConfig, /DETAIL:\s*\(id:\s*string\)/, 'customer lead detail endpoint should be configured');
assert.match(apiConfig, /FUNNEL:/, 'customer lead funnel endpoint should be configured');
assert.match(apiConfig, /FOLLOWUPS:\s*\(id:\s*string\)/, 'customer lead followup endpoint should be configured');
assert.match(api, /CustomerServiceEventItem/, 'customer lead API should expose event timeline type');
assert.match(api, /CustomerFunnelMetric/, 'customer lead API should expose funnel metric type');
assert.match(api, /CustomerLeadFollowupItem/, 'customer lead API should expose followup type');
assert.match(api, /CustomerIntentAnalysis/, 'customer lead API should expose intent analysis type');
assert.match(api, /getCustomerLeadDetail/, 'customer lead API should fetch detail');
assert.match(api, /getCustomerLeadFunnel/, 'customer lead API should fetch funnel data');
assert.match(api, /createCustomerLeadFollowup/, 'customer lead API should create followup records');
assert.match(api, /interface CustomerLeadActionPayload\s*{[\s\S]*?remark:\s*string;/, 'customer lead actions should require a remark');
assert.match(api, /updateCustomerLeadStatus\s*=\s*async\s*\(id:\s*string,\s*payload:\s*CustomerLeadActionPayload\)/, 'status update should use the required action payload');

assert.match(page, /handleViewDetail/, 'page should open detail');
assert.match(page, /智能客服漏斗/, 'page should render funnel dashboard');
assert.match(page, /客户摘要/, 'page should render generated customer summary');
assert.match(page, /自动判断/, 'page should render automatic intent analysis');
assert.match(page, /跟进记录/, 'page should render multiple followup records');
assert.match(page, /客户行为时间线/, 'page should render event timeline');
assert.match(page, /保存跟进/, 'page should save follow-up remark');
assert.match(page, /const describeEvent\s*=\s*\(item:\s*CustomerServiceEventItem\)[\s\S]*?item\.payload/, 'page should render event payload context');
assert.match(page, /buildActionPayload[\s\S]*?remark:\s*form\.remark\.trim\(\)/, 'page should require and submit the follow-up remark');
assert.match(styles, /customer-lead-modal/, 'detail modal styles should exist');
assert.match(styles, /customer-lead-timeline/, 'timeline styles should exist');
assert.match(styles, /customer-lead-funnel/, 'funnel styles should exist');
assert.match(styles, /customer-lead-followup-list/, 'followup styles should exist');

assert.match(route, /leads\.GET\("\/:id"/, 'backend should expose detail route');
assert.match(route, /leads\.GET\("\/funnel"/, 'backend should expose funnel route');
assert.match(route, /leads\.POST\("\/:id\/followups"/, 'backend should expose followup route');
assert.match(model, /ListEventsForLead/, 'backend should load events for lead detail');
assert.match(model, /customer_lead_followups/, 'backend should persist followup records');
assert.match(model, /BuildLeadSummary/, 'backend should generate customer summary');
assert.match(model, /AnalyzeLeadIntentFromInput/, 'backend should analyze customer intent');

console.log('customer leads v2/v3 checks passed');
