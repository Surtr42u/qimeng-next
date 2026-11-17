import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { FolderInput, FolderPlus, Trash2 } from 'lucide-react';
import { useLibraries } from '../data/manage';
import { useDirectories } from '../data/catalog';
import { useAssets } from '../data/media';
import type { Asset } from '../data/media';
import { useFileActions } from '../data/file-actions';
import type { FileJob, FileTarget } from '../data/file-actions';
import { ManagePageHeader } from '../components/LegacyManageHeader';
import { Select, SelectContent, SelectItem, SelectTrigger } from '../components/LegacySelect';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { ErrorNotice, Loading } from '../components/Feedback';
import { CreateDirectoryDialog, FileJobsPanel, MoveFilesDialog } from './FileManagementDialogs';
import { UnsavedGuard } from '../lib/navigation';
import { bytes } from '../lib/format';

export function FileManagementPage() {
  const libraries = useLibraries();
  const [params, setParams] = useSearchParams();
  const libraryId = params.get('libraryId') ?? '';
  const directory = params.get('directory') ?? '';
  const directories = useDirectories(libraryId, directory, !!libraryId);
  const [createUnder, setCreateUnder] = useState<{ libraryId: string; parent: string } | null>(null);
  useEffect(() => setCreateUnder(null), [libraryId, directory]);
  const change = (nextDirectory: string, nextLibrary = libraryId) => setParams({ libraryId: nextLibrary, directory: nextDirectory });
  return <div className="page manage-workspace" id="page-maintenance-files-browse">
    <ManagePageHeader title="文件整理" description="浏览已入库目录；重命名与移动保留关联，删除先进入回收站" />
    <ErrorNotice error={libraries.error} stale={!!libraries.data} retry={() => void libraries.refetch()} />
    <div className="rank-card">
      <div className="rank-head"><h3>目录浏览</h3><span className="f-years"><Select value={libraryId} onValueChange={value => change('', value)}><SelectTrigger aria-label="选择库" placeholder="选择库…" /><SelectContent>{libraries.data?.items.map(library => <SelectItem key={library.id} value={library.id}>{library.name}</SelectItem>)}</SelectContent></Select></span></div>
      <p className="rank-note">选择目录查看文件；目录可新建子目录，文件操作支持重命名、移动和移入回收站。</p>
      {!libraryId ? <p className="grid-empty">先在上方选择一个库。</p> : <>
        <ErrorNotice error={directories.error} stale={!!directories.data} retry={() => void directories.refetch()} />
        {directory && <div className="pill-row"><button className="pill" onClick={() => change('')}>库根</button><button className="pill" onClick={() => change(directory.split('/').slice(0, -1).join('/'))}>上一级</button></div>}
        <ul className="dir-tree-list">
          <li><div className="dir-row"><button className="dir-node selected"><span className="dir-name">{directory || '库根'}</span></button><span className="dir-actions"><button className="dir-action-btn" title="在此目录下新建子目录" onClick={() => setCreateUnder({ libraryId, parent: directory })}><FolderPlus width={14} height={14} />新建子目录</button></span></div></li>
          {directories.data?.pages.flatMap(page => page.items).map(item => <li key={item.relativePath}><div className="dir-row" style={{ paddingLeft: 16 }}><button className="dir-node" onClick={() => change(item.relativePath)}><span className="dir-name">{item.name}</span></button><span className="dir-actions"><button className="dir-action-btn" title="在此目录下新建子目录" onClick={() => setCreateUnder({ libraryId, parent: item.relativePath })}><FolderPlus width={14} height={14} />新建子目录</button></span></div></li>)}
        </ul>
        {directories.isPending && <Loading text="目录加载中…" />}
        {directories.hasNextPage && <button className="pill" disabled={directories.isFetchingNextPage} onClick={() => void directories.fetchNextPage()}>更多目录</button>}
        <DirectoryFiles key={`${libraryId}:${directory}`} libraryId={libraryId} directory={directory} />
        {createUnder !== null && <CreateDirectoryDialog libraryId={createUnder.libraryId} parent={createUnder.parent} close={() => setCreateUnder(null)} />}
      </>}
    </div>
  </div>;
}

function DirectoryFiles({ libraryId, directory }: { libraryId: string; directory: string }) {
  const query = useAssets({ libraryId, directory, recursive: false, includeDisabled: true });
  const files = query.data?.pages.flatMap(page => page.items) ?? [];
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [move, setMove] = useState<FileTarget[]>();
  const [remove, setRemove] = useState<FileTarget[]>();
  const [moveReport, setMoveReport] = useState<FileJob[]>([]);
  const trash = useFileActions();
  const unavailable = query.isPending || query.isError || query.isPlaceholderData;
  const locked = unavailable || trash.running || !!move || !!remove;
  const selectedFiles = files.filter(file => selected.has(file.id));
  const allSelected = !!files.length && files.every(file => selected.has(file.id));
  const exit = () => { setSelecting(false); setSelected(new Set()); };
  useEffect(() => {
    if (!selecting || locked) return;
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') { setSelecting(false); setSelected(new Set()); } };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selecting, locked]);
  const reportMove = (jobs: FileJob[]) => {
    setMoveReport(jobs);
    setSelected(new Set(jobs.filter(job => job.state !== 'succeeded').map(job => job.target.id)));
  };
  const beginDelete = (targets: Asset[]) => { trash.reset(); setRemove(targets.map(file => ({ id: file.id, fileName: file.fileName, revision: file.revision }))); };
  const performDelete = async () => {
    if (!remove) return;
    await trash.start(remove, { kind: 'trash' });
    setRemove(undefined);
  };
  return <>
    <p className="dir-files-head"><span>{directory || '库根'}</span><b>已加载 {files.length} 项</b><button className={`pill${selecting ? ' active' : ''}`} type="button" disabled={locked} title="进入/退出多选（多选态可批量删除/移动，Esc 退出）" onClick={() => selecting ? exit() : setSelecting(true)}>{selecting ? '退出多选' : '多选'}</button></p>
    {selecting && files.length > 0 && <div className="ms-bar"><button className="pill" disabled={locked} onClick={() => setSelected(allSelected ? new Set() : new Set(files.map(file => file.id)))}>{allSelected ? '取消全选' : '全选已加载'}</button><span className="ms-bar-count">已选 <b>{selectedFiles.length}</b> / {files.length}</span><button className="pill" disabled={locked || !selectedFiles.length} onClick={() => setMove(selectedFiles)}>批量移动</button><button className="pill" disabled={locked || !selectedFiles.length} onClick={() => beginDelete(selectedFiles)}>批量删除</button><button className="pill ms-bar-exit" disabled={locked} onClick={exit}>退出多选</button></div>}
    <ErrorNotice error={query.error} stale={files.length > 0} retry={() => void query.refetch()} />
    {query.isPending ? <Loading /> : !files.length && !query.isError ? <p className="grid-empty">该目录下暂无文件。</p> : <ul className="dir-file-list">{files.map(file => <li key={file.id} className={`dir-row dir-file-row${selected.has(file.id) ? ' ms-row-selected' : ''}`}>
      {selecting && <input type="checkbox" className="qm-check" checked={selected.has(file.id)} disabled={locked} onChange={() => setSelected(current => { const next = new Set(current); next.has(file.id) ? next.delete(file.id) : next.add(file.id); return next; })} aria-label={`选择 ${file.fileName}`} />}
      <span className="dir-file-name" title={file.fileName}>{file.fileName}</span><span className="dir-file-meta">{file.mediaType === 'video' ? '视频' : file.mediaType === 'animated_image' ? '动图' : '图片'} · {bytes(file.size)}</span>
      {!selecting && <span className="dir-actions"><button className="dir-action-btn" title="移动或重命名" disabled={locked} onClick={() => setMove([file])}><FolderInput width={14} height={14} />移动/重命名</button><button className="dir-action-btn" title="移入回收站（可在维护页恢复）" disabled={locked} onClick={() => beginDelete([file])}><Trash2 width={14} height={14} />删除</button></span>}
    </li>)}</ul>}
    {query.hasNextPage && <div className="settings-actions"><button className="pill" disabled={query.isFetchingNextPage || query.isError || locked} onClick={() => void query.fetchNextPage()}>{query.isFetchingNextPage ? '读取中…' : '加载更多文件'}</button><span className="rank-note">批量操作仅作用于已加载并选中的文件。</span></div>}
    {remove && <ConfirmDialog title={`把选中的 ${remove.length} 个文件移入回收站？`} danger busy={trash.running} confirmLabel={`移入回收站（${remove.length}）`} onCancel={() => setRemove(undefined)} onConfirm={() => void performDelete()}><p>将逐个移入回收站并从媒体库移除，浏览、点赞等记录保留。可在维护页“回收站”恢复。</p></ConfirmDialog>}
    {move && <MoveFilesDialog libraryId={libraryId} directory={directory} targets={move} close={() => setMove(undefined)} report={reportMove} />}
    <FileJobsPanel jobs={trash.jobs} running={trash.running} action="删除" dismiss={trash.reset} retry={() => void trash.retry()} />
    <FileJobsPanel jobs={moveReport} running={false} action="移动" dismiss={() => setMoveReport([])} />
    <UnsavedGuard dirty={trash.running} />
  </>;
}
