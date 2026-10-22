const CLOUD_IMAGE_DIRECTORY = `${wx.env.USER_DATA_PATH}/cloud-images`;
const DOWNLOAD_TIMEOUT_MS = 10000;
const pendingDownloads: Record<string, Promise<string> | undefined> = {};

function hashUrl(url: string): string {
  let hash = 0x811c9dc5;
  for (let index = 0; index < url.length; index += 1) {
    hash ^= url.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193);
  }
  return (hash >>> 0).toString(16).padStart(8, '0');
}

function safeFileName(fileName: string): string {
  const cleanName = String(fileName || 'image').replace(/[^a-zA-Z0-9._-]/g, '-');
  return cleanName || 'image';
}

function fileExists(filePath: string): Promise<boolean> {
  return new Promise((resolve) => {
    wx.getFileSystemManager().access({
      path: filePath,
      success: () => resolve(true),
      fail: () => resolve(false),
    });
  });
}

function ensureDirectory(): Promise<void> {
  return new Promise((resolve, reject) => {
    wx.getFileSystemManager().mkdir({
      dirPath: CLOUD_IMAGE_DIRECTORY,
      recursive: true,
      success: () => resolve(),
      fail: (error: any) => String(error?.errMsg || '').includes('file already exists') ? resolve() : reject(error),
    });
  });
}

function removeIncompleteFile(filePath: string): Promise<void> {
  return new Promise((resolve) => {
    wx.getFileSystemManager().unlink({
      filePath,
      success: () => resolve(),
      fail: () => resolve(),
    });
  });
}

async function downloadCloudImage(url: string, filePath: string): Promise<string> {
  await ensureDirectory();
  return new Promise((resolve) => {
    wx.downloadFile({
      url,
      filePath,
      timeout: DOWNLOAD_TIMEOUT_MS,
      success: async (response) => {
        if (response.statusCode === 200 && await fileExists(filePath)) {
          resolve(filePath);
          return;
        }
        await removeIncompleteFile(filePath);
        console.warn('下载云端图片失败', url, `HTTP ${response.statusCode}`);
        resolve('');
      },
      fail: async (error) => {
        await removeIncompleteFile(filePath);
        console.warn('下载云端图片失败', url, error);
        resolve('');
      },
    });
  });
}

export async function prepareCloudImage(url: string, fileName: string): Promise<string> {
  const cleanUrl = String(url || '').trim();
  if (!/^https:\/\//i.test(cleanUrl)) {
    console.warn('云端图片地址无效', cleanUrl);
    return '';
  }

  const filePath = `${CLOUD_IMAGE_DIRECTORY}/${hashUrl(cleanUrl)}-${safeFileName(fileName)}`;
  const pending = pendingDownloads[cleanUrl];
  if (pending) return pending;

  const preparation = (async () => {
    if (await fileExists(filePath)) return filePath;
    return downloadCloudImage(cleanUrl, filePath);
  })();
  pendingDownloads[cleanUrl] = preparation;
  try {
    return await preparation;
  } catch (error) {
    console.warn('准备云端图片失败', cleanUrl, error);
    return '';
  } finally {
    delete pendingDownloads[cleanUrl];
  }
}
