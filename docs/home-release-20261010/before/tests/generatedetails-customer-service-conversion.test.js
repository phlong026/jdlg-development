const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');

function read(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), 'utf8');
}

const detailWxml = read('miniprogram/pages/generatedetails/generatedetails.wxml');
const detailTs = read('miniprogram/pages/generatedetails/generatedetails.ts');
const detailScss = read('miniprogram/pages/generatedetails/generatedetails.scss');
const serviceTs = read('miniprogram/pages/customerservice/customerservice.ts');
const customerModel = read('../service/model/customer_service.go');

for (const label of ['咨询建房顾问', '调整户型布局', '优化建筑外观', '精准测算造价', '添加微信获取完整方案']) {
  assert.match(detailWxml, new RegExp(label), `generatedetails should render CTA: ${label}`);
}

for (const action of ['consult', 'layout', 'facade', 'cost', 'wechat']) {
  assert.match(detailWxml, new RegExp(`data-action="${action}"`), `generatedetails should tag CTA action ${action}`);
}

assert.match(detailTs, /action_text=\$\{encodeURIComponent\(actionText\)\}/, 'generatedetails should pass action text to customer service');
assert.match(detailTs, /image_url=\$\{encodeURIComponent\(imageUrl\)\}/, 'generatedetails should pass current image to customer service');
assert.match(detailTs, /task_no=\$\{encodeURIComponent\(taskNo\)\}/, 'generatedetails should pass task number to customer service');
assert.match(detailScss, /customer-service-result-actions/, 'generatedetails should style the CTA group');
assert.match(detailScss, /grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)/, 'CTA group should use a stable two-column grid');

assert.match(serviceTs, /actionText/, 'customer service should receive result-page action text');
assert.match(serviceTs, /结果页诉求：/, 'customer service lead remark should include result-page action');
assert.match(serviceTs, /action_text:\s*consultationPlan\.actionText/, 'customer service event should record result-page action text');

assert.match(customerModel, /session_no VARCHAR\(128\)/, 'customer leads should store session number');
assert.match(customerModel, /findExistingLeadID/, 'customer leads should dedupe before insert');
assert.match(customerModel, /updateExistingLead/, 'customer leads should update an existing lead instead of duplicating it');

console.log('generatedetails customer service conversion checks passed');
