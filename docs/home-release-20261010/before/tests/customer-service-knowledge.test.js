const fs = require('fs');
const path = require('path');
const assert = require('assert');

const root = path.resolve(__dirname, '..');
const source = fs.readFileSync(
  path.join(root, 'miniprogram/utils/customerServiceKnowledge.ts'),
  'utf8',
);
const servicePageSource = fs.readFileSync(
  path.join(root, 'miniprogram/pages/customerservice/customerservice.ts'),
  'utf8',
);

const expectations = [
  ['怎么收费', '50、300、500', '收费知识库应包含充值档位'],
  ['多少钱一张', '一张图 2 元', '收费知识库应包含单图价格'],
  ['AI出的图能用吗', '设计水平也比较高', 'AI 出图说明应同步 Word 里的设计水平表述'],
  ['新闽派和新中式', '现代元素组合融合', '新闽派说明应包含现代元素组合融合'],
  ['施工队推荐', '自有团队承建', '施工队问题应说明自有团队承建'],
  ['建三层大概多少钱', '造价生成', '建房造价应引导主页造价生成'],
  ['装修预算', '外装一般约 500-1000 元/㎡', '装修预算应包含外装区间'],
  ['几个房间', '5-8 个房间', '房间数量应更新为 5-8 个'],
  ['福建建房', '风貌管控', '地域问题应覆盖风貌管控'],
  ['砖混和框架哪个好', '闽南地区做框架结构比较多', '结构问题应包含闽南框架结构偏好'],
  ['软地基', '打桩', '软地基问题应建议打桩到持力层'],
  ['混凝土是商混还是自拌', 'C30', '混凝土问题应包含商混和 C30'],
  ['水电预埋', '不含穿线', '水电预埋问题应说明不含穿线'],
  ['钢筋品牌', '三钢', '材料品牌问题应包含钢筋品牌'],
  ['签合同吗', '设计合同', '合同问题应说明签正式设计合同'],
  ['售后怎么处理', '泉州有多家服务中心', '售后问题应包含泉州服务中心'],
  ['厅做多大', '大厅小房', '闽南风俗问题应覆盖大厅小房'],
  ['神龛', '风水位', '神龛问题应覆盖风水位'],
  ['开大门', '纳气', '大门问题应覆盖纳气表述'],
];

for (const [keyword, replyFragment, message] of expectations) {
  assert.match(source, new RegExp(keyword.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `${message}: missing keyword ${keyword}`);
  assert.match(source, new RegExp(replyFragment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `${message}: missing reply fragment ${replyFragment}`);
}

assert.match(source, /category:\s*'technical'/, 'knowledge base should include technical category');
assert.match(source, /category:\s*'custom'/, 'knowledge base should include custom category');
assert.match(source, /category:\s*'competitor'/, 'knowledge base should include competitor category');
assert.doesNotMatch(source, /现在元素/, 'knowledge base should use 现代元素 instead of the Word typo 现在元素');

const knowledgeMatchIndex = servicePageSource.indexOf('const knowledgeHit = matchKnowledge(text)');
const highIntentIndex = servicePageSource.indexOf("if (nextIntentLevel === 'high')", knowledgeMatchIndex);
assert.ok(knowledgeMatchIndex > -1, 'customer service page should call matchKnowledge');
assert.ok(highIntentIndex > knowledgeMatchIndex, 'knowledge base should be checked before unmatched high-intent handoff');

console.log('customer service knowledge checks passed');
