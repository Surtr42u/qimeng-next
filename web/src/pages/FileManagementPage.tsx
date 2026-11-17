import { Link, useSearchParams } from 'react-router-dom';
import { useLibraries } from '../data/manage';
import { useDirectories } from '../data/catalog';
import { ManagePageHeader } from '../components/LegacyManageHeader';
import { Select, SelectContent, SelectItem, SelectTrigger } from '../components/LegacySelect';
import { ErrorNotice, Loading } from '../components/Feedback';

export function FileManagementPage() {
  const libraries = useLibraries();
  const [params, setParams] = useSearchParams();
  const libraryId = params.get('libraryId') ?? '';
  const directory = params.get('directory') ?? '';
  const directories = useDirectories(libraryId, directory, !!libraryId);
  const change = (nextDirectory: string, nextLibrary = libraryId) => setParams({ libraryId: nextLibrary, directory: nextDirectory });
  return <div className="page manage-workspace" id="page-maintenance-files-browse">
    <ManagePageHeader title="文件整理" description="浏览已入库目录；重命名与移动保留关联，删除先进入回收站" />
    <ErrorNotice error={libraries.error} retry={() => void libraries.refetch()} />
    <div className="rank-card"><div className="rank-head"><h3>目录浏览</h3><span className="f-years"><Select value={libraryId} onValueChange={value => change('', value)}><SelectTrigger aria-label="选择库" placeholder="选择库…" /><SelectContent>{libraries.data?.items.map(library => <SelectItem key={library.id} value={library.id}>{library.name}</SelectItem>)}</SelectContent></Select></span></div>
      <p className="rank-note">选择目录查看文件；目录可新建子目录，文件操作支持重命名、移动和移入回收站。</p>
      {!libraryId ? <p className="grid-empty">先在上方选择一个库。</p> : <><ErrorNotice error={directories.error} stale={!!directories.data} retry={() => void directories.refetch()} />{directories.isPending ? <Loading /> : <><div className="pill-row"><button className="pill" onClick={() => change('')}>库根</button>{directory && <button className="pill" onClick={() => change(directory.split('/').slice(0, -1).join('/'))}>上一级</button>}<span className="manage-path">{directory}</span></div><ul className="dir-tree-list">{directories.data?.pages.flatMap(page => page.items).map(item => <li key={item.relativePath}><button className="dir-name" onClick={() => change(item.relativePath)}>{item.name}</button></li>)}</ul>{directories.hasNextPage && <button className="pill" disabled={directories.isFetchingNextPage} onClick={() => void directories.fetchNextPage()}>更多目录</button>}<p className="rank-note" role="status">目录文件分页正在接入，当前尚未显示此目录的文件清单。</p><Link className="pill" to={`/album?libraryId=${encodeURIComponent(libraryId)}`}>浏览此库内容</Link></>}</>}
    </div>
  </div>;
}
