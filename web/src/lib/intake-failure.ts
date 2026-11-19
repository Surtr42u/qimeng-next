import type { Schema } from '../data/client';

export function intakeFailure(input: Schema<'BatchInput'>): string | undefined {
  if (input.state !== 'failed' || input.uploadId) return undefined;
  if (input.errorCode === 'empty_input') return '文件为空，未创建上传。';
  if (input.errorCode === 'unsupported_media') return '不支持此文件格式，未创建上传。';
  return '原始输入校验失败，未创建上传。';
}
