// crypto-js 4.2.0 按需子集（2026-11-04 主包瘦身：全量 bundle 240KB → 子集约 50KB）。
// 项目仅用 MD5 / HmacSHA256 / enc.Hex / enc.Utf8 / enc.Base64（见 utils/parameter.js）。
// 完整 bundle 备份在 git 历史；如需其他算法，从官方 crypto-js 拷入对应模块文件并在下方挂载。
const Core = require('./src/core');
require('./src/enc-base64');
const MD5 = require('./src/md5');
const HmacSHA256 = require('./src/hmac-sha256');

module.exports = {
  MD5,
  HmacSHA256,
  enc: Core.enc,
};
