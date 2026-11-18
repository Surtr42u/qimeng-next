import { DirectoryTree } from "../components/DirectoryTree";
import { useEffect, useRef, useState } from 'react';
import { Dialog } from 'radix-ui';
import { FolderPlus } from 'lucide-react';
import { useCreateDirectory, useDirectories } from '../data/catalog';
import { useFileActions } from '../data/file-actions';
import type { FileJob, FileTarget } from '../data/file-actions';
import { ErrorNotice } from '../components/Feedback';
import { UnsavedGuard } from '../lib/navigation';
import { newId } from '../lib/id';

export function CreateDirectoryDialog({ libraryId, parent, close }: {
  libraryId: string; parent: string; close: () => void;
}) {
  const [name, setName] = useState('');
  const mutation = useCreateDirectory(libraryId);
  const attempt = useRef({ path: '', key: newId() });
  const save = () => {
    if (!name.trim() || mutation.isPending) return;
    const relativePath = parent ? `${parent}/${name.trim()}` : name.trim();
    if (attempt.current.path !== relativePath) attempt.current = { path: relativePath, key: newId() };
    mutation.mutate({ relativePath, key: attempt.current.key }, { onSuccess: close });
  };
  return <Dialog.Root open onOpenChange={open => { if (!open && !mutation.isPending) close(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="detail-dialog-overlay" />
      <Dialog.Content className="detail-dialog">
        <Dialog.Title className="detail-dialog-title">新建子目录</Dialog.Title>
        <Dialog.Description className="detail-dialog-desc">在「{parent || '库根'}」下创建目录；同名目录已存在时同样成功。</Dialog.Description>
        <div className="tag-newrow" style={{ marginTop: 14 }}>
          <input className="tag-input" value={name} disabled={mutation.isPending} placeholder="目录名，回车确认" autoFocus onChange={event => setName(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); save(); } }} />
          <button className="confirm-btn confirm-btn--cancel" onClick={save} disabled={!name.trim() || mutation.isPending}><FolderPlus width={14} height={14} />创建</button>
        </div>
        <ErrorNotice error={mutation.error} />
        <div className="detail-dialog-foot"><button className="confirm-btn confirm-btn--cancel" disabled={mutation.isPending} onClick={close}>取消</button></div>
      </Dialog.Content>
    </Dialog.Portal>
    <UnsavedGuard dirty={mutation.isPending} />
  </Dialog.Root>;
}

export function MoveFilesDialog({ libraryId, directory, targets, close, report }: {
  libraryId: string; directory: string; targets: FileTarget[]; close: () => void; report: (jobs: FileJob[]) => void;
}) {
  const [destination, setDestination] = useState(directory);
  const [name, setName] = useState(targets[0]?.fileName ?? '');
  const listing = useDirectories(libraryId, destination, true);
  const actions = useFileActions();
  const batch = targets.length > 1;
  const failed = actions.jobs.some(job => job.state !== 'succeeded');
  useEffect(() => {
    if (actions.jobs.length && !actions.running && !failed) { report(actions.jobs); close(); }
  }, [actions.jobs, actions.running, failed, close, report]);
  const save = () => {
    if (actions.jobs.length) void actions.retry();
    else void actions.start(targets, { kind: 'move', directory: destination, ...(batch ? {} : { fileName: name.trim() }) });
  };
  const restartDraft = () => actions.reset();
  const locked = actions.running || actions.jobs.some(job => job.state === 'uncertain' || job.state === 'succeeded');
  const dismiss = () => { if (!actions.running) { report(actions.jobs); close(); } };
  return <Dialog.Root open onOpenChange={open => { if (!open) dismiss(); }}>
    <Dialog.Portal>
      <Dialog.Overlay className="detail-dialog-overlay" />
      <Dialog.Content className="detail-dialog move-dialog" onEscapeKeyDown={event => { if (actions.running) event.preventDefault(); }} onPointerDownOutside={event => { if (actions.running) event.preventDefault(); }}>
        <Dialog.Title className="detail-dialog-title">{batch ? `批量移动 ${targets.length} 个文件` : '整理文件'}</Dialog.Title>
        <Dialog.Description className="detail-dialog-desc">{batch ? '选择目标目录后逐个移动；与目标位置已有文件同名时该文件将被拒绝，不影响其余文件。' : '选择目标目录或修改文件名；目标位置已有同名文件时将被拒绝。'}</Dialog.Description>
        <div className="move-dir-box">
          <DirectoryTree libraryId={libraryId} selected={destination} disabled={locked} onSelect={path => { setDestination(path); restartDraft(); }} />
        </div>
        {!batch && <label className="move-name-row"><span>文件名</span><input className="tag-input" value={name} disabled={locked} placeholder="新文件名（含扩展名）" onChange={event => { setName(event.target.value); restartDraft(); }} /></label>}
        <FileJobsPanel jobs={actions.jobs} running={actions.running} action="移动" />
        <div className="detail-dialog-foot"><button className="confirm-btn confirm-btn--cancel" disabled={actions.running} onClick={() => { report(actions.jobs); close(); }}>取消</button><button className="confirm-btn confirm-btn--primary" disabled={actions.running || listing.isError || listing.isPending || !batch && !name.trim() || !actions.jobs.length && destination === directory && (batch || name === targets[0]?.fileName)} onClick={save}>{actions.running ? '保存中…' : actions.jobs.length ? '重试未确认或失败项' : batch ? `移动到「${destination || '库根'}」` : '保存'}</button></div>
      </Dialog.Content>
    </Dialog.Portal>
    <UnsavedGuard dirty={actions.running} />
  </Dialog.Root>;
}

export function FileJobsPanel({ jobs, running, action, dismiss, retry }: {
  jobs: FileJob[]; running: boolean; action: string; dismiss?: () => void; retry?: () => void;
}) {
  if (!jobs.length) return null;
  const done = jobs.filter(job => ['succeeded', 'failed', 'uncertain'].includes(job.state)).length;
  const failed = jobs.filter(job => ['failed', 'uncertain', 'cancelled'].includes(job.state));
  return <div className="batch-panel" role="status" aria-label={`批量操作进度：${action}`}>
    <div className="batch-panel-head"><b>{running ? `${action}中…` : failed.length ? `${action}已结束，${failed.length} 项需处理` : `${action}完成`}</b><span className="batch-panel-count">{done} / {jobs.length}</span>{!running && dismiss && <button className="pill" type="button" onClick={dismiss}>收起</button>}</div>
    <div className="progress"><i style={{ width: `${done / jobs.length * 100}%` }} /></div>
    {!!failed.length && <ul className="batch-panel-failures">{failed.map(job => <li key={job.target.id}><span className="batch-fail-label" title={job.target.fileName}>{job.target.fileName}</span><span className="batch-fail-reason">{job.message}</span></li>)}</ul>}
    {!!failed.length && retry && !running && <button className="pill" onClick={retry}>重试未确认或失败项</button>}
  </div>;
}
