import { Link } from 'react-router-dom';
import { useStats } from '../data/manage';
import { useSession } from '../data/session';
import { ErrorNotice, Loading, PageHeading } from '../components/Feedback';
import { bytes } from '../lib/format';

export function DataPage() {
  const stats = useStats();
  return <><PageHeading title="数据" subtitle="当前库存与已记录的浏览活动。" /><ErrorNotice error={stats.error} stale={!!stats.data} retry={() => void stats.refetch()} />{stats.isPending ? <Loading /> : stats.data && <><div className="section-heading"><h2>当前媒体库</h2></div><div className="metric-grid"><div className="metric panel"><span>媒体文件</span><strong>{stats.data.assets.toLocaleString('zh-CN')}</strong><small>图片与视频</small></div><div className="metric panel"><span>原件体积</span><strong>{bytes(stats.data.bytes)}</strong><small>已入库内容</small></div><div className="metric panel"><span>我的收藏</span><strong>{stats.data.favorites.toLocaleString('zh-CN')}</strong><small>已收藏媒体</small></div></div><div className="data-panels"><section className="panel"><h2>内容构成</h2><dl className="facts"><div><dt>图片（含动图）</dt><dd>{stats.data.images}</dd></div><div><dt>视频</dt><dd>{stats.data.videos}</dd></div><div><dt>作者</dt><dd><Link to="/authors">{stats.data.authors}</Link></dd></div><div><dt>回收站</dt><dd><Link to="/manage/trash">{stats.data.trash}</Link></dd></div></dl></section><section className="panel"><h2>浏览记录</h2><p className="large-number">{stats.data.events}</p><p className="muted">服务端已记录的行为事件，不等同于浏览文件数。</p><Link className="button" to="/mine?tab=history">查看历史</Link></section></div></>}<section className="panel"><h2>浏览回顾</h2><p className="muted">窗口趋势、停留时长与兴趣变化等待服务器提供已确认口径。当前不以库存或事件总数替代回顾报告。</p></section></>;
}
export function MaintenancePage() {
  const { instance } = useSession();
  return <><PageHeading title="服务与维护" subtitle="当前连接与维护入口。" /><section className="panel"><h2>{instance.name}</h2><dl className="facts"><div><dt>协议</dt><dd>{instance.apiVersion}</dd></div><div><dt>智能执行器</dt><dd>{instance.capabilities.aiConfigured ? '已配置' : '未配置'}</dd></div></dl><p className="muted">已配置不代表执行器当前在线，逐批进度以任务报告为准。</p></section><div className="manage-grid"><Link className="panel manage-card" to="/manage/upload"><h2>上传与入库</h2></Link><Link className="panel manage-card" to="/manage/tasks"><h2>任务与报告</h2></Link><Link className="panel manage-card" to="/manage/trash"><h2>回收站</h2></Link><Link className="panel manage-card" to="/manage/backup"><h2>备份与归档</h2></Link></div></>;
}
