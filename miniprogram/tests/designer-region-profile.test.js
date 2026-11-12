const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../miniprogram');

function setByPath(target, key, value) {
  const parts = key.split('.');
  let current = target;
  while (parts.length > 1) {
    const part = parts.shift();
    current[part] ||= {};
    current = current[part];
  }
  current[parts[0]] = value;
}

function createProfilePage(requestHandler = () => {}) {
  let page;
  const wx = {
    getStorageSync: key => key === 'token' ? 'token' : {},
    setStorageSync() {},
    showToast() {},
    navigateTo() {},
    request: requestHandler,
  };
  const requireStub = id => {
    if (id.includes('deviceFingerprint')) return { generateDeviceFingerprint: async () => '', getCachedDeviceFingerprint: () => '', cacheDeviceFingerprint() {} };
    if (id.includes('parameter')) return { generateRequestParams: () => ({}), paramsToHeaders: () => ({}) };
    if (id.includes('asset')) return {
      resolveAssetPath: value => value,
      normalizeCosUrl: value => String(value).replace('cos.ap-chongqing.myqcloud.com', 'static.jiadilingguang.com'),
    };
    return {};
  };
  vm.runInNewContext(
    fs.readFileSync(path.join(root, 'pages/myInformationmodification/myInformationmodification.js'), 'utf8'),
    { exports: {}, require: requireStub, Page: value => { page = value; }, wx, console, setTimeout },
  );
  page.data = JSON.parse(JSON.stringify(page.data));
  page.setData = values => Object.entries(values).forEach(([key, value]) => setByPath(page.data, key, value));
  page.getAuthHeaders = () => ({});
  return page;
}

test('设计师所在地使用原生地区选择并说明会公开', () => {
  const wxml = fs.readFileSync(path.join(root, 'pages/myInformationmodification/myInformationmodification.wxml'), 'utf8');
  assert.match(wxml, /mode="region"/);
  assert.match(wxml, /所在地/);
  assert.match(wxml, /公开展示/);
});

test('地区选择保存为空格连接的 designer_region', async () => {
  let savedBody;
  const page = createProfilePage(options => {
    savedBody = options.data;
    options.success({ statusCode: 200, data: { code: 0 } });
  });
  page.onDesignerRegionChange({ detail: { value: ['浙江省', '杭州市', '西湖区'] } });
  assert.equal(page.data.designerForm.designerRegion, '浙江省 杭州市 西湖区');
  await page.saveDesignerProfile();
  assert.equal(savedBody.designer_region, '浙江省 杭州市 西湖区');
  assert.equal(page.data.userInfo.designerRegion, '浙江省 杭州市 西湖区');
});

test('资料接口回填 designer_region', async () => {
  const page = createProfilePage(options => options.success({
    statusCode: 200,
    data: { code: 0, data: { id: 7, designer_region: '四川省 成都市 武侯区', avatar: 'https://bucket.cos.ap-chongqing.myqcloud.com/avatar.jpg' } },
  }));
  await page.loadUserInfo();
  assert.equal(page.data.designerForm.designerRegion, '四川省 成都市 武侯区');
  assert.equal(page.data.userInfo.designerRegion, '四川省 成都市 武侯区');
  assert.equal(page.data.userInfo.avatar, 'https://bucket.static.jiadilingguang.com/avatar.jpg');
});

test('设计师主页读取并展示公开所在地', () => {
  const source = fs.readFileSync(path.join(root, 'pages/designerhome/designerhome.js'), 'utf8');
  const wxml = fs.readFileSync(path.join(root, 'pages/designerhome/designerhome.wxml'), 'utf8');
  assert.match(source, /designer_region/);
  assert.match(wxml, /profile\.region/);
  assert.match(wxml, /所在地/);
});

test('主页头像加载失败只清除对应头像', () => {
  let page;
  const requireStub = id => {
    if (id.includes('asset')) return { resolveAssetPath: value => value, normalizeCosUrl: value => value };
    if (id.includes('enterpriseWechat')) return {
      DEFAULT_ENTERPRISE_WECHAT_QRCODE: '', DEFAULT_ENTERPRISE_SERVICE_PHONE: '',
      DEFAULT_CUSTOMER_SERVICE_CORP_ID: '', DEFAULT_CUSTOMER_SERVICE_URL: '',
    };
    if (id.includes('favoriteApi')) return {};
    return {};
  };
  vm.runInNewContext(
    fs.readFileSync(path.join(root, 'pages/designerhome/designerhome.js'), 'utf8'),
    { exports: {}, require: requireStub, Page: value => { page = value; }, wx: {}, console, setTimeout, clearTimeout },
  );
  page.data = JSON.parse(JSON.stringify(page.data));
  page.data.profile.avatar = 'profile.jpg';
  page.data.filteredReviews = [
    { id: 11, avatar: 'broken.jpg', name: '甲' },
    { id: 12, avatar: 'good.jpg', name: '乙' },
  ];
  page.data.allReviews = JSON.parse(JSON.stringify(page.data.filteredReviews));
  page.setData = values => Object.entries(values).forEach(([key, value]) => setByPath(page.data, key, value));

  page.onProfileAvatarError();
  assert.equal(page.data.profile.avatar, '');
  page.onReviewAvatarError({ currentTarget: { dataset: { index: 0, id: 11 } } });
  assert.equal(page.data.filteredReviews[0].avatar, '');
  assert.equal(page.data.filteredReviews[1].avatar, 'good.jpg');
  assert.equal(page.data.allReviews[0].avatar, '');
  assert.equal(page.data.allReviews[1].avatar, 'good.jpg');
});
