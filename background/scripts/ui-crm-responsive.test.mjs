// Compiled CSS contract tests only: these do not claim browser/screenshot verification.
import assert from 'node:assert/strict';
import test from 'node:test';
import * as sass from 'sass';
import { fileURLToPath } from 'node:url';
import postcss from 'postcss';
const css=postcss.parse(sass.compile(fileURLToPath(new URL('../src/pages/customerLeads.scss',import.meta.url)),{logger:sass.Logger.silent}).css);
function styles(selector,width=1200,height=900){
 const result={};css.walkRules(rule=>{if(!rule.selector.endsWith(' '+selector))return;
  for(let p=rule.parent;p;p=p.parent)if(p.type==='atrule'&&p.name==='media'&&!p.params.split(',').some(term=>[...term.matchAll(/max-(width|height):\s*(\d+)px/g)].every(([,axis,n])=>(axis==='width'?width:height)<=Number(n))))return;
  rule.walkDecls(d=>{result[d.prop]=d.value});
 });return result;
}
test('customer lead style compilation remains confined to both authorized CRM entrances',()=>{
 css.walkRules(rule=>{for(const route of ['/customer-leads','/customer-management'])assert.ok(rule.selector.includes(`data-admin-page="${route}"`),rule.selector)});
});
for(const [width,height] of [[320,568],[768,1024],[769,600],[900,500],[1200,480]])test(`lead dialog scroll contract at ${width}×${height}`,()=>{
 const action=styles('.customer-lead-action-modal',width,height),form=styles('.customer-lead-action-form',width,height),head=styles('.customer-lead-modal-header',width,height),foot=styles('.customer-lead-action-footer',width,height),overlay=styles('.customer-lead-modal-overlay',width,height);
 assert.match(action['max-height'],/100dvh/);assert.match(action['max-height'],/safe-area-inset-bottom/);assert.equal(action['min-height'],'0');assert.equal(action.overflow,'hidden');
 assert.equal(form['min-height'],'0');assert.equal(form['overflow-y'],'auto');assert.equal(form['overscroll-behavior'],'contain');assert.equal(head['flex-shrink'],'0');
 assert.equal(foot.position,'sticky');assert.equal(foot.bottom,'0');assert.match(foot.padding,/safe-area-inset-bottom/);assert.match(overlay.padding,/safe-area-inset-top/);assert.equal(overlay['--lead-modal-gap'],'12px');
 if(width<=900)assert.equal(form['grid-template-columns'],'1fr');
});
test('search can shrink below the previous 260px floor without losing its input width',()=>{
 for(const width of [320,365,480,769]){const search=styles('.customer-lead-search',width);assert.equal(search['min-width'],'0');assert.equal(search['max-width'],'100%');if(width<=480)assert.equal(search['flex-basis'],'100%')}
 assert.equal(styles('.customer-lead-search input')['width'],'100%');
});

// Resolve relevant compiled selectors across the shared CSS bundle and page sheet, in either loading order.
// This deliberately includes shorthand expansion and specificity rather than
// assuming the last source declaration (or a single sheet) describes the winner.
const shared=postcss.parse(['App.scss','responsive.scss','responsive-priority.scss','responsive-remaining.scss'].map(file=>sass.compile(fileURLToPath(new URL('../src/'+file,import.meta.url)),{logger:sass.Logger.silent}).css).join('\n'));
const specificity=selector=>{
 const effective=selector.replace(/:where\([^)]*\)/g,'');
 return [0,(effective.match(/\.[\w-]+|\[[^\]]+\]/g)||[]).length,0];
};
function cascade(sheets,route,target,width){
 const winners={};let order=0;
 for(const sheet of sheets)sheet.walkRules(rule=>{
  for(let p=rule.parent;p;p=p.parent)if(p.type==='atrule'&&p.name==='media'&&!p.params.split(',').some(term=>{
   if(/prefers-/.test(term))return false;
   return [...term.matchAll(/(max|min)-(width|height):\s*(\d+)px/g)].every(([,bound,axis,n])=>bound==='max'?(axis==='width'?width:600)<=Number(n):(axis==='width'?width:600)>=Number(n));
  }))return;
  for(const selector of rule.selectors){
   if(!selector.endsWith(' '+target))continue;
   const routes=[...selector.matchAll(/data-admin-page="([^"]+)"/g)].map(m=>m[1]);
   if(routes.length&&!routes.includes(route))continue;
   const weight=specificity(selector);
   rule.walkDecls(decl=>{
    order++;const props=decl.prop==='overflow'?['overflow-x','overflow-y']:[decl.prop];
    for(const prop of props){const candidate={value:decl.value,important:!!decl.important,weight,order,selector};const old=winners[prop];
     if(!old||Number(candidate.important)>Number(old.important)||(candidate.important===old.important&&(weight[1]>old.weight[1]||(weight[1]===old.weight[1]&&order>old.order))))winners[prop]=candidate;
    }
   });
  }
 });
 return winners;
}
for(const route of ['/customer-leads','/customer-management'])for(const width of [320,365,768])for(const pageLast of [false,true])test(`${route} ${width}px: combined CSS keeps only the body scrolling, page CSS ${pageLast?'last':'first'}`,()=>{
 const sheets=pageLast?[shared,css]:[css,shared];
 const action=cascade(sheets,route,'.customer-lead-action-modal',width),body=cascade(sheets,route,'.customer-lead-action-form',width);
 assert.deepEqual(specificity('.layout-content .customer-lead-action-modal'),[0,2,0]);
 assert.deepEqual(action['max-height'].weight,[0,3,0]);assert.match(action['max-height'].selector,new RegExp(`data-admin-page="${route}"`));
 assert.match(action['max-height'].value,/100dvh/);assert.match(action['max-height'].value,/safe-area-inset-bottom/);
 assert.equal(action['overflow-y'].value,'hidden');assert.deepEqual(action['overflow-y'].weight,[0,3,0]);assert.equal(body['overflow-y'].value,'auto');assert.equal(body['min-height'].value,'0');
});
test('mobile specificity override does not leak to another route',()=>{
 const action=cascade([css,shared],'/support-tickets','.customer-lead-action-modal',365);
 assert.equal(action['max-height'].value,'92dvh');assert.deepEqual(action['max-height'].weight,[0,2,0]);
});
