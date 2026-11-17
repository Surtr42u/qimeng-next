import { useState } from 'react';
import { Link } from 'react-router-dom';
import { SegmentedControl } from '@ui/GlassNavigation';
import { useSettings } from '../data/manage';
import { useLogout, useSession } from '../data/session';
import { PageHeading, ErrorNotice, Loading } from '../components/Feedback';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { RecommendationSettings } from './RecommendationSettings';
import { setTheme } from '../lib/preferences';
import type { Theme } from '../lib/preferences';
import { bytes, dateTime } from '../lib/format';

export function SettingsPage() {
  const { instance, session } = useSession();
  const settings = useSettings(); const logout = useLogout();
  const [theme, setChoice] = useState<Theme>(document.documentElement.dataset.theme === 'light' ? 'light' : 'dark');
  const [storageError, setStorageError] = useState(false); const [confirm, setConfirm] = useState(false);
  return <>
    <PageHeading title="设置" subtitle="让影库适合你的浏览习惯。" />
    <div className="settings-grid">
      <section className="panel form-stack settings-interface">
        <h2>界面</h2><label>外观</label>
        <SegmentedControl label="外观主题" value={theme} options={[{ value: 'dark', label: '暮色玻璃' }, { value: 'light', label: '晨雾玻璃' }]} onChange={value => { setChoice(value); setStorageError(!setTheme(value)); }} />
        {storageError && <p role="alert" className="notice notice--warning">外观已应用，但本地存储不可用，刷新后可能无法保留。</p>}
        <p className="muted">减少动态效果遵循系统偏好。玻璃不受支持时使用同布局的不透明面板。</p>
        <Link to="/authors">作者资料</Link><Link to="/manage/vocabulary">词表与关联</Link><Link to="/manage/libraries">媒体库管理</Link>
      </section>
      <RecommendationSettings />
      <section className="panel form-stack">
        <h2>影库与连接</h2>
        <dl className="facts"><div><dt>服务器</dt><dd>{instance.name}</dd></div><div><dt>当前设备</dt><dd>{session.label}</dd></div><div><dt>会话到期</dt><dd>{dateTime(session.expiresAt)}</dd></div><div><dt>协议版本</dt><dd>{instance.apiVersion}</dd></div></dl>
        <button className="button" onClick={() => setConfirm(true)}>退出此设备</button>
      </section>
      <section className="panel form-stack">
        <h2>传输与安全</h2><ErrorNotice error={settings.error} stale={!!settings.data} retry={() => void settings.refetch()} />
        {settings.isPending ? <Loading /> : settings.data && <dl className="facts"><div><dt>每片上限</dt><dd>{bytes(settings.data.maxChunkBytes)}</dd></div><div><dt>同时上传上限</dt><dd>{settings.data.maxActiveUploads}</dd></div><div><dt>单批条目上限</dt><dd>{settings.data.maxBatchItems}</dd></div><div><dt>来源清理</dt><dd>{settings.data.sourceCleanupEnabled ? '按备份与权限凭据执行' : '保留源文件'}</dd></div></dl>}
        <p className="muted">这些限制由当前服务器管理。</p>
      </section>
    </div>
    {confirm && <ConfirmDialog title="退出此设备？" busy={logout.isPending} confirmLabel="退出" onCancel={() => setConfirm(false)} onConfirm={() => logout.mutate()}><p>撤销当前设备会话。未完成上传会中断，原始文件仍保留。</p><ErrorNotice error={logout.error} /></ConfirmDialog>}
  </>;
}
