export function sanitizeAIGenerationErrorMessage(raw: any): string {
  const message = String(raw || '').trim();
  if (!message) {
    return '这次生成没有成功，请调整内容后再试一次。';
  }

  const lower = message.toLowerCase();

  if (containsAny(lower, ['sensitive', '敏感', '违规', 'unsafe', 'forbidden content', 'filtered', 'audio_filtered', 'public_error_audio_filtered'])) {
    return '当前内容暂不支持生成，请调整描述或图片后再试一次。';
  }

  if (containsAny(lower, ['首帧', '尾帧', '图片', '图像', 'image', 'reference', 'decode', 'invalid image', '读取图片', '下载图片'])) {
    return '图片处理失败，请更换图片或稍后再试一次。';
  }

  if (containsAny(lower, ['timeout', 'timed out', 'deadline exceeded', 'context deadline exceeded', 'client.timeout', 'expired'])) {
    return '当前生成等待时间较长，请稍后再试一次。';
  }

  if (containsAny(lower, ['too many requests', 'rate limit', 'service unavailable', 'bad gateway', 'queue', 'busy', '429', '502', '503'])) {
    return '当前生成服务较忙，请稍后再试一次。';
  }

  if (containsAny(lower, ['api key', 'unauthorized', 'forbidden', 'permission', '未配置', '401', '403'])) {
    return '当前生成服务暂时不可用，请稍后再试一次。';
  }

  if (containsAny(lower, ['upload', '上传失败', '存储服务', 'concat', '拼接', '下载视频', '读取视频', 'http', 'https', 'api.', 'models/', 'bearer'])) {
    return '结果处理中断，请稍后再试一次。';
  }

  return message.length <= 40 ? message : '这次生成没有成功，请调整内容后再试一次。';
}

function containsAny(source: string, keywords: string[]): boolean {
  return keywords.some((keyword) => source.includes(String(keyword || '').toLowerCase()));
}
