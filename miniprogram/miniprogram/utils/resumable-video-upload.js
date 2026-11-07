'use strict';

const STORAGE_KEY = 'phase3_video_upload_session_v1';
const MAX_PART_ATTEMPTS = 3;
const MAX_PROCESSING_POLLS = 60;
const MAX_CHUNK_BYTES = 16 * 1024 * 1024;

function wxCall(run) {
  return new Promise((resolve, reject) => run({ success: resolve, fail: reject }));
}

function fileName(file) {
  const explicit = String(file && file.name || '').trim();
  if (explicit) return explicit;
  const path = String(file && file.tempFilePath || 'video.mp4');
  return path.split('/').pop() || 'video.mp4';
}

function sameSession(session, input) {
  return !!session
    && session.account_key === input.accountKey
    && session.owner_id === input.ownerID
    && session.rights === input.rights
    && session.fingerprint === input.fingerprint
    && Number(session.size) === input.size
    && session.upload_id;
}

function sameStoredOwner(session, expected) {
  return !!session && !!expected
    && session.account_key === expected.account_key
    && session.upload_id === expected.upload_id;
}

function removeOwnedSession(wxAPI, expected) {
  const current = wxAPI.getStorageSync(STORAGE_KEY);
  if (!sameStoredOwner(current, expected)) return false;
  wxAPI.removeStorageSync(STORAGE_KEY);
  return true;
}

function replaceSession(wxAPI, expected, next) {
  const current = wxAPI.getStorageSync(STORAGE_KEY);
  if (expected ? !sameStoredOwner(current, expected) : !!current) return false;
  wxAPI.setStorageSync(STORAGE_KEY, next);
  return true;
}

function ensureCurrent(isCurrent, phase) {
  if (isCurrent && !isCurrent()) throw new Error(`账号已变化，已停止当前视频${phase}`);
}

async function retryPart(api, uploadID, index, partPath, onProgress, isCurrent) {
  let lastError;
  for (let attempt = 1; attempt <= MAX_PART_ATTEMPTS; attempt += 1) {
    ensureCurrent(isCurrent, '上传');
    try {
      const result = await api.uploadVideoPart(uploadID, index, partPath, { onProgress: progress => { if (!isCurrent || isCurrent()) onProgress(progress); } });
      ensureCurrent(isCurrent, '上传');
      if (result && result.reset_required) throw Object.assign(new Error(result.error || '视频分片需要重置'), { resetRequired: true });
      return;
    } catch (error) {
      ensureCurrent(isCurrent, '上传');
      if (error && error.resetRequired) throw error;
      lastError = error;
    }
  }
  throw lastError || new Error('视频分片上传失败');
}

function wait(milliseconds) { return new Promise(resolve => setTimeout(resolve, milliseconds)); }

async function pollProcessing(api, uploadID, initial, options) {
  let result = initial;
  for (let attempt = 0; attempt < MAX_PROCESSING_POLLS; attempt += 1) {
    if (!result || !result.processing) return result;
    ensureCurrent(options.isCurrent, '处理');
    if (options.onPhase) options.onPhase('视频已上传，正在处理…');
    const interval = Number.isFinite(options.pollIntervalMs) ? Math.max(0, options.pollIntervalMs) : 2000;
    await wait(interval);
    ensureCurrent(options.isCurrent, '处理');
    result = await api.videoUploadStatus(uploadID);
    ensureCurrent(options.isCurrent, '处理');
  }
  throw new Error('视频已上传并在后台处理，请稍后重试，不会重复上传');
}

function resultAsset(result) { return result && (result.asset || result.media_id && result) || null; }

async function completeWithRetry(api, uploadID, initial, options) {
  let result = initial;
  for (let attempt = 0; attempt < MAX_PART_ATTEMPTS; attempt += 1) {
    if (!result || result.error && result.retryable) {
      ensureCurrent(options.isCurrent, '处理');
      result = await api.completeVideoUpload(uploadID);
      ensureCurrent(options.isCurrent, '处理');
    }
    result = await pollProcessing(api, uploadID, result, options);
    if (resultAsset(result) || result && result.reset_required || !(result && result.error && result.retryable)) return result;
  }
  return result;
}

async function uploadResumableVideo(options) {
  const api = options.api;
  const wxAPI = options.wx || wx;
  const file = options.file || {};
  const filePath = String(file.tempFilePath || file.path || '');
  if (!filePath) throw new Error('未获取到待上传的视频');
  if (!api || !api.beginVideoUpload || !api.videoUploadStatus || !api.uploadVideoPart || !api.completeVideoUpload) {
    throw new Error('视频可续传服务尚未完成升级');
  }

  const info = await wxCall(callback => wxAPI.getFileInfo({ filePath, digestAlgorithm: 'sha1', ...callback }));
  const size = Number(file.size || info.size || 0);
  const fingerprint = String(info.digest || '');
  if (!Number.isSafeInteger(size) || size <= 0 || !fingerprint) throw new Error('无法读取视频文件信息');

  const identity = {
    accountKey: String(options.accountKey || ''), ownerID: String(options.ownerID || ''), rights: String(options.rights || ''), fingerprint, size,
  };
  let session = wxAPI.getStorageSync(STORAGE_KEY);
  let expectedStoredSession = session || null;
  let upload;
  if (sameSession(session, identity)) {
    try {
      ensureCurrent(options.isCurrent, '上传');
      upload = await api.videoUploadStatus(session.upload_id);
      ensureCurrent(options.isCurrent, '上传');
    }
    catch (error) {
      if (!error || error.statusCode !== 404) throw error;
      ensureCurrent(options.isCurrent, '上传');
      removeOwnedSession(wxAPI, session);
      session = null;
      expectedStoredSession = null;
    }
  }
  if (!upload) {
    ensureCurrent(options.isCurrent, '上传');
    upload = await api.beginVideoUpload({
      owner_id: identity.ownerID,
      rights: identity.rights,
      filename: fileName(file),
      size,
      fingerprint,
      reset: options._resetRequested === true,
    });
    ensureCurrent(options.isCurrent, '上传');
  }
  const initialUploadID = String(upload && upload.upload_id || session && session.upload_id || '');
  if (upload && upload.processing) upload = await pollProcessing(api, initialUploadID, upload, options);
  if (upload && upload.error && upload.retryable && !upload.reset_required) upload = await completeWithRetry(api, initialUploadID, upload, options);
  const resumedAsset = resultAsset(upload);
  if (resumedAsset) {
    ensureCurrent(options.isCurrent, '上传');
    removeOwnedSession(wxAPI, { account_key: identity.accountKey, upload_id: initialUploadID });
    if (options.onProgress) options.onProgress(100);
    return resumedAsset;
  }
  if (upload && upload.reset_required) {
    if (options._resetRequested) throw new Error(upload.error || '视频分片重置失败');
    ensureCurrent(options.isCurrent, '上传');
    removeOwnedSession(wxAPI, { account_key: identity.accountKey, upload_id: initialUploadID });
    return uploadResumableVideo({ ...options, _resetRequested: true });
  }
  if (upload && upload.error) throw new Error(String(upload.error));

  const uploadID = String(upload && upload.upload_id || session && session.upload_id || '');
  const chunkSize = Number(upload && upload.chunk_size || session && session.chunk_size || 0);
  if (!uploadID || !Number.isSafeInteger(chunkSize) || chunkSize <= 0 || chunkSize > MAX_CHUNK_BYTES) throw new Error('视频可续传服务返回了无效分片配置');
  const uploaded = new Set((upload && upload.uploaded_parts || []).map(Number));
  const totalParts = Math.ceil(size / chunkSize);
  const nextSession = {
    account_key: identity.accountKey,
    owner_id: identity.ownerID,
    rights: identity.rights,
    fingerprint,
    size,
    upload_id: uploadID,
    chunk_size: chunkSize,
    uploaded_parts: [...uploaded],
  };
  ensureCurrent(options.isCurrent, '上传');
  if (!replaceSession(wxAPI, expectedStoredSession, nextSession)) throw new Error('上传会话已被新任务接管');
  session = nextSession;

  const fs = wxAPI.getFileSystemManager();
  const basePath = String(wxAPI.env && wxAPI.env.USER_DATA_PATH || '');
  for (let index = 0; index < totalParts; index += 1) {
    if (options.isCurrent && !options.isCurrent()) throw new Error('账号已变化，已停止当前视频上传');
    if (uploaded.has(index)) continue;
    const position = index * chunkSize;
    const length = Math.min(chunkSize, size - position);
    const partPath = `${basePath}/video-upload-${uploadID}-${index}.part`;
    let resetPart = false;
    try {
      const result = await wxCall(callback => fs.readFile({ filePath, position, length, ...callback }));
      await wxCall(callback => fs.writeFile({ filePath: partPath, data: result.data, ...callback }));
      await retryPart(api, uploadID, index, partPath, partProgress => {
        if (!options.onProgress) return;
        const progress = Math.max(0, Math.min(100, Number(partProgress && partProgress.progress !== undefined ? partProgress.progress : partProgress) || 0));
        options.onProgress(Math.min(99, Math.round((index + progress / 100) / totalParts * 100)));
      }, options.isCurrent);
      ensureCurrent(options.isCurrent, '上传');
    } catch (error) {
      if (error && error.resetRequired) resetPart = true;
      else throw error;
    } finally {
      try { await wxCall(callback => fs.unlink({ filePath: partPath, ...callback })); } catch (_) {}
    }
    ensureCurrent(options.isCurrent, '上传');
    if (resetPart) {
      if (options._resetRequested) throw new Error('视频分片重置后仍校验失败');
      removeOwnedSession(wxAPI, session);
      return uploadResumableVideo({ ...options, _resetRequested: true });
    }
    uploaded.add(index);
    const updatedSession = { ...session, uploaded_parts: [...uploaded] };
    if (!replaceSession(wxAPI, session, updatedSession)) throw new Error('上传会话已被新任务接管');
    session = updatedSession;
    if (options.onProgress) options.onProgress(Math.min(99, Math.round(uploaded.size / totalParts * 100)));
  }

  ensureCurrent(options.isCurrent, '处理');
  const completed = await completeWithRetry(api, uploadID, null, options);
  if (completed && completed.reset_required) {
    if (options._resetRequested) throw new Error(completed.error || '视频分片重置失败');
    ensureCurrent(options.isCurrent, '处理');
    removeOwnedSession(wxAPI, session);
    return uploadResumableVideo({ ...options, _resetRequested: true });
  }
  const asset = resultAsset(completed);
  if (!asset) throw new Error(completed && completed.error || '视频合并完成，但服务未返回可用素材');
  ensureCurrent(options.isCurrent, '处理');
  removeOwnedSession(wxAPI, session);
  if (options.onProgress) options.onProgress(100);
  return asset;
}

module.exports = { STORAGE_KEY, uploadResumableVideo };
