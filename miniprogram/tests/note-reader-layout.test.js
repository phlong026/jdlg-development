'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const {buildContentPresentation} = require('../miniprogram/utils/content-presentation');

test('reader gallery fills the viewport while editor and video keep their media frame', () => {
  const content = {title:'庭院记录', presentation:'gallery', body:'正文'};
  const media = [{media_id:'image-one',kind:'image',url:'wxfile://one',width:1500,height:1000}];
  const window = {windowWidth:390,windowHeight:844};
  assert.equal(buildContentPresentation(content,media,window,{fullWidthGallery:true}).media[0].width,390);
  assert.equal(buildContentPresentation(content,media,window).media[0].width,354);
  const video = [{media_id:'video-one',kind:'video',url:'wxfile://video',width:1500,height:1000}];
  assert.equal(buildContentPresentation({...content,presentation:'video'},video,window,{fullWidthGallery:true}).media[0].width,354);
});

test('presentation formats timestamps and hides invalid server values', () => {
  const content = {title:'记录',body:'正文',published_at:'2026-11-09T05:25:19.153277048Z'};
  const value = buildContentPresentation(content,[],{windowWidth:390});
  assert.match(value.publishedAt,/^2026-11-09 \d{2}:25$/);
  assert.equal(buildContentPresentation({...content,published_at:'bad date'},[],{}).publishedAt,'');
});
