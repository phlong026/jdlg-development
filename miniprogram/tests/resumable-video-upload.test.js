'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { uploadResumableVideo } = require('../miniprogram/utils/resumable-video-upload');

test('video upload resumes server parts and only keeps one chunk in memory', async () => {
  const calls = [];
  const storage = new Map();
  const temp = new Map();
  let activeBuffers = 0;
  let peakBuffers = 0;
  const fs = {
    readFile({ position, length, success }) {
      calls.push(['read', position, length]);
      activeBuffers += 1;
      peakBuffers = Math.max(peakBuffers, activeBuffers);
      success({ data: new ArrayBuffer(length) });
    },
    writeFile({ filePath, data, success }) {
      temp.set(filePath, data.byteLength);
      success();
    },
    unlink({ filePath, success }) {
      if (temp.delete(filePath)) activeBuffers -= 1;
      success();
    },
  };
  const wx = {
    env: { USER_DATA_PATH: '/user' },
    getFileSystemManager: () => fs,
    getFileInfo({ success }) { success({ size: 10, digest: 'sha1-video' }); },
    getStorageSync: key => storage.get(key),
    setStorageSync: (key, value) => storage.set(key, value),
    removeStorageSync: key => storage.delete(key),
  };
  storage.set('phase3_video_upload_session_v1', {
    account_key: 'same-account', owner_id: 'owner-a', rights: 'own', fingerprint: 'sha1-video', size: 10,
    upload_id: 'upload-1', chunk_size: 4, uploaded_parts: [0],
  });
  const progress = [];
  const api = {
    beginVideoUpload: async () => { throw new Error('已有会话不应重建'); },
    videoUploadStatus: async id => { calls.push(['status', id]); return { upload_id: id, chunk_size: 4, uploaded_parts: [0, 1] }; },
    uploadVideoPart: async (id, index, filePath, options) => {
      calls.push(['part', id, index, filePath]);
      options.onProgress(50);
    },
    completeVideoUpload: async id => ({ media_id: 'video-1', kind: 'video', upload_id: id }),
  };

  const asset = await uploadResumableVideo({
    api, wx, file: { tempFilePath: '/tmp/video.mp4', size: 10, name: 'video.mp4' },
    accountKey: 'same-account', ownerID: 'owner-a', rights: 'own', onProgress: value => progress.push(value),
  });

  assert.equal(asset.media_id, 'video-1');
  assert.deepEqual(calls.filter(call => call[0] === 'part').map(call => call[2]), [2]);
  assert.deepEqual(calls.filter(call => call[0] === 'read').map(call => call.slice(1)), [[8, 2]]);
  assert.equal(peakBuffers, 1);
  assert.equal(temp.size, 0, '临时分片必须清理');
  assert.equal(storage.has('phase3_video_upload_session_v1'), false, '完成后不得留下过期会话');
  assert.equal(progress.at(-1), 100);
});

test('video upload retries a failed chunk without restarting completed parts', async () => {
  const storage = new Map();
  const fs = {
    readFile({ length, success }) { success({ data: new ArrayBuffer(length) }); },
    writeFile({ success }) { success(); },
    unlink({ success }) { success(); },
  };
  const wx = {
    env: { USER_DATA_PATH: '/user' }, getFileSystemManager: () => fs,
    getFileInfo({ success }) { success({ size: 3, digest: 'sha1-retry' }); },
    getStorageSync: key => storage.get(key), setStorageSync: (key, value) => storage.set(key, value), removeStorageSync: key => storage.delete(key),
  };
  let attempts = 0;
  const api = {
    beginVideoUpload: async () => ({ upload_id: 'upload-2', chunk_size: 3, uploaded_parts: [] }),
    videoUploadStatus: async () => { throw new Error('新会话不应查状态'); },
    uploadVideoPart: async () => { attempts += 1; if (attempts < 2) throw new Error('短暂断网'); },
    completeVideoUpload: async () => ({ media_id: 'video-2', kind: 'video' }),
  };
  const asset = await uploadResumableVideo({ api, wx, file: { tempFilePath: '/tmp/retry.mp4', size: 3 }, accountKey: 'account-b', ownerID: 'owner-a', rights: 'authorized' });
  assert.equal(asset.media_id, 'video-2');
  assert.equal(attempts, 2);
});

function onePartWX(storage, digest = 'sha1-recovery') {
  return {
    env: { USER_DATA_PATH: '/user' },
    getFileSystemManager: () => ({
      readFile({ length, success }) { success({ data: new ArrayBuffer(length) }); },
      writeFile({ success }) { success(); },
      unlink({ success }) { success(); },
    }),
    getFileInfo({ success }) { success({ size: 3, digest }); },
    getStorageSync: key => storage.get(key),
    setStorageSync: (key, value) => storage.set(key, value),
    removeStorageSync: key => storage.delete(key),
  };
}

test('a missing saved server session is cleared and begun again', async () => {
  const storage = new Map([['phase3_video_upload_session_v1', {
    account_key: 'account-c', owner_id: 'owner-a', rights: 'own', fingerprint: 'sha1-recovery', size: 3,
    upload_id: 'stale-upload', chunk_size: 3, uploaded_parts: [],
  }]]);
  let began = 0;
  const api = {
    videoUploadStatus: async () => { throw Object.assign(new Error('会话不存在'), { statusCode: 404 }); },
    beginVideoUpload: async input => { began += 1; assert.equal(input.reset, false); return { upload_id: 'fresh-upload', chunk_size: 3, uploaded_parts: [] }; },
    uploadVideoPart: async () => {},
    completeVideoUpload: async () => ({ media_id: 'fresh-video', kind: 'video' }),
  };
  const asset = await uploadResumableVideo({ api, wx: onePartWX(storage), file: { tempFilePath: '/tmp/recover.mp4', size: 3 }, accountKey: 'account-c', ownerID: 'owner-a', rights: 'own' });
  assert.equal(asset.media_id, 'fresh-video');
  assert.equal(began, 1);
});

test('a transient completion error retries completion without reuploading parts', async () => {
  const storage = new Map();
  let completes = 0;
  let parts = 0;
  const api = {
    beginVideoUpload: async () => ({ upload_id: 'complete-retry', chunk_size: 3, uploaded_parts: [] }),
    videoUploadStatus: async () => ({ upload_id: 'complete-retry', chunk_size: 3, uploaded_parts: [0] }),
    uploadVideoPart: async () => { parts += 1; },
    completeVideoUpload: async () => {
      completes += 1;
      return completes === 1 ? { upload_id: 'complete-retry', processing: false, error: '暂时处理失败', retryable: true } : { media_id: 'completed-video', kind: 'video' };
    },
  };
  const asset = await uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-complete'), file: { tempFilePath: '/tmp/complete.mp4', size: 3 }, accountKey: 'account-d', ownerID: 'owner-a', rights: 'own' });
  assert.equal(asset.media_id, 'completed-video');
  assert.equal(completes, 2);
  assert.equal(parts, 1);
});

test('reset-required status discards corrupt client state and starts a clean upload', async () => {
  const storage = new Map([['phase3_video_upload_session_v1', {
    account_key: 'account-e', owner_id: 'owner-a', rights: 'own', fingerprint: 'sha1-reset', size: 3,
    upload_id: 'corrupt-upload', chunk_size: 3, uploaded_parts: [0],
  }]]);
  let began = 0;
  const api = {
    videoUploadStatus: async () => ({ upload_id: 'corrupt-upload', reset_required: true, error: '分片校验失败' }),
    beginVideoUpload: async input => { began += 1; assert.equal(input.reset, true); return { upload_id: 'reset-upload', chunk_size: 3, uploaded_parts: [] }; },
    uploadVideoPart: async () => {},
    completeVideoUpload: async () => ({ media_id: 'reset-video', kind: 'video' }),
  };
  const asset = await uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-reset'), file: { tempFilePath: '/tmp/reset.mp4', size: 3 }, accountKey: 'account-e', ownerID: 'owner-a', rights: 'own' });
  assert.equal(asset.media_id, 'reset-video');
  assert.equal(began, 1);
});

test('account change aborts after an in-flight part and never completes the old upload', async () => {
  const storage = new Map();
  let current = true;
  let completed = false;
  const api = {
    beginVideoUpload: async () => ({ upload_id: 'account-switch', chunk_size: 3, uploaded_parts: [] }),
    videoUploadStatus: async () => ({}),
    uploadVideoPart: async (_id, _index, _path, options) => { current = false; options.onProgress({ progress: 80 }); },
    completeVideoUpload: async () => { completed = true; return { media_id: 'wrong-video' }; },
  };
  await assert.rejects(uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-switch'), file: { tempFilePath: '/tmp/switch.mp4', size: 3 }, accountKey: 'account-f', ownerID: 'owner-a', rights: 'own', isCurrent: () => current }), /账号已变化/);
  assert.equal(completed, false);
});

test('account change during processing stops before the next status request', async () => {
  const storage = new Map();
  let current = true;
  let statusCalls = 0;
  const api = {
    beginVideoUpload: async () => ({ upload_id: 'poll-switch', chunk_size: 3, uploaded_parts: [0] }),
    videoUploadStatus: async () => { statusCalls += 1; return { processing: true }; },
    uploadVideoPart: async () => {},
    completeVideoUpload: async () => ({ upload_id: 'poll-switch', processing: true }),
  };
  await assert.rejects(uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-poll'), file: { tempFilePath: '/tmp/poll.mp4', size: 3 }, accountKey: 'account-g', ownerID: 'owner-a', rights: 'own', isCurrent: () => current, pollIntervalMs: 0, onPhase: () => { current = false; } }), /账号已变化/);
  assert.equal(statusCalls, 0);
});

test('a reset-required part response restarts with reset true and reuploads the part', async () => {
  const storage = new Map();
  let begins = 0;
  let parts = 0;
  const api = {
    beginVideoUpload: async input => {
      begins += 1;
      assert.equal(input.reset, begins === 2);
      return { upload_id: begins === 1 ? 'part-corrupt' : 'part-reset', chunk_size: 3, uploaded_parts: [] };
    },
    videoUploadStatus: async () => ({}),
    uploadVideoPart: async () => { parts += 1; return parts === 1 ? { reset_required: true, error: '分片损坏' } : {}; },
    completeVideoUpload: async () => ({ media_id: 'part-reset-video', kind: 'video' }),
  };
  const asset = await uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-part-reset'), file: { tempFilePath: '/tmp/part-reset.mp4', size: 3 }, accountKey: 'account-h', ownerID: 'owner-a', rights: 'own' });
  assert.equal(asset.media_id, 'part-reset-video');
  assert.equal(parts, 2);
});

test('a 404 returned after account switch cannot delete the new account session', async () => {
  const oldSession = { account_key: 'old-account', owner_id: 'owner-a', rights: 'own', fingerprint: 'sha1-status-race', size: 3, upload_id: 'old-upload', chunk_size: 3, uploaded_parts: [] };
  const newSession = { account_key: 'new-account', owner_id: 'owner-b', rights: 'own', fingerprint: 'sha1-new', size: 5, upload_id: 'new-upload', chunk_size: 3, uploaded_parts: [] };
  const storage = new Map([['phase3_video_upload_session_v1', oldSession]]);
  let current = true;
  const api = {
    videoUploadStatus: async () => {
      current = false;
      storage.set('phase3_video_upload_session_v1', newSession);
      throw Object.assign(new Error('旧会话已失效'), { statusCode: 404 });
    },
    beginVideoUpload: async () => { throw new Error('旧任务不应重建会话'); },
    uploadVideoPart: async () => {}, completeVideoUpload: async () => ({}),
  };
  await assert.rejects(uploadResumableVideo({ api, wx: onePartWX(storage, 'sha1-status-race'), file: { tempFilePath: '/tmp/status-race.mp4', size: 3 }, accountKey: 'old-account', ownerID: 'owner-a', rights: 'own', isCurrent: () => current }), /账号已变化/);
  assert.equal(storage.get('phase3_video_upload_session_v1').upload_id, 'new-upload');
});

test('account switch while unlinking a chunk cannot overwrite the new account session', async () => {
  const storage = new Map();
  const newSession = { account_key: 'new-account', owner_id: 'owner-b', rights: 'own', fingerprint: 'sha1-new', size: 5, upload_id: 'new-upload', chunk_size: 3, uploaded_parts: [] };
  let current = true;
  const wx = onePartWX(storage, 'sha1-unlink-race');
  wx.getFileSystemManager = () => ({
    readFile({ length, success }) { success({ data: new ArrayBuffer(length) }); },
    writeFile({ success }) { success(); },
    unlink({ success }) {
      current = false;
      storage.set('phase3_video_upload_session_v1', newSession);
      success();
    },
  });
  const api = {
    beginVideoUpload: async () => ({ upload_id: 'old-unlink-upload', chunk_size: 3, uploaded_parts: [] }),
    videoUploadStatus: async () => ({}), uploadVideoPart: async () => ({}), completeVideoUpload: async () => ({}),
  };
  await assert.rejects(uploadResumableVideo({ api, wx, file: { tempFilePath: '/tmp/unlink-race.mp4', size: 3 }, accountKey: 'old-account', ownerID: 'owner-a', rights: 'own', isCurrent: () => current }), /账号已变化/);
  assert.equal(storage.get('phase3_video_upload_session_v1').upload_id, 'new-upload');
  assert.deepEqual(storage.get('phase3_video_upload_session_v1').uploaded_parts, []);
});
