import { useRef, useState } from "react";
import { Link } from "react-router-dom";
import { ChevronDown, FileUp, ListChecks, UploadCloud } from "lucide-react";
import { useCollections, useLibraries, useSettings } from "../data/manage";
import type { Schema } from "../data/client";
import { useSession } from "../data/session";
import { useUploadQueue } from "../data/upload";
import { isQueueTerminal, QueuedPreparationError } from "../data/upload-queue";
import { AcquisitionSourceField } from "../components/AcquisitionSourceField";
import { AuthorSuggestField } from "../components/AuthorSuggestField";
import { ManagePageHeader } from "../components/LegacyManageHeader";
import { ErrorNotice, Loading } from "../components/Feedback";
import { droppedFiles } from "../lib/drop-files";
import { UploadDestinationCard } from "./UploadDestinationCard";
import { UploadRow } from "./UploadQueueList";
import { NamingControls } from "./NamingControls";
import { previewNamingFiles } from "../data/naming";
import type { NamingFilePreview } from "../data/naming";

export function UploadPage() {
  const libraries = useLibraries();
  const settings = useSettings();
  const { instance } = useSession();
  const upload = useUploadQueue();
  const [libraryId, setLibraryId] = useState("");
  const [directory, setDirectory] = useState("");
  const [mode, setMode] = useState<"ordinary" | "intelligent">("ordinary");
  const [author, setAuthor] = useState<Schema<"Author">>();
  const [authorDraft, setAuthorDraft] = useState("");
  const [sources, setSources] = useState<string[]>([]);
  const [collectionId, setCollectionId] = useState("");
  const [note, setNote] = useState("");
  const [automaticName, setAutomaticName] = useState(false);
  const [family, setFamily] = useState("");
  const [namePreview, setNamePreview] = useState<NamingFilePreview[]>([]);
  const [retryFiles, setRetryFiles] = useState<readonly File[]>();
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState("");
  const [over, setOver] = useState(false);
  const intake = useRef(false);
  const filePicker = useRef<HTMLInputElement>(null);
  const directoryPicker = useRef<HTMLInputElement>(null);
  const collectionQuery = useCollections(libraryId || undefined, !!libraryId);
  const collections = collectionQuery.data?.pages.flatMap(page => page.items) ?? [];
  const selected = libraries.data?.items.find(item => item.id === libraryId);
  const targetsReady = !!libraries.data && !libraries.isError;
  const canAttach = selected?.kind === "regular" && selected.enabled;
  const blocked = !targetsReady ? "目标库列表尚不可用，请先重新读取"
    : !libraryId ? "先选择目标库，再添加文件"
    : !selected?.enabled ? "所选目标库已停用或不在列表中，请重新选择"
    : settings.isError ? "上传设置读取失败，请重新读取"
    : !settings.data ? "正在读取上传设置…"
    : !settings.data.upload.autoAccept ? "服务器已暂停接收上传，请先在设置中开启"
    : canAttach && authorDraft.trim() && !author ? "作者尚未选定，请从联想结果选择，或清空作者后上传"
    : mode === "ordinary" && automaticName && !family.trim() && !collectionId ? "请填写作品基名"
    : !upload.queue || !upload.initialized ? "正在恢复上传队列…"
    : busy || (upload.preparing ? "正在登记本批文件…" : "");
  const chooseLibrary = (id: string) => {
    if (id === libraryId) return;
    setLibraryId(id); setDirectory(""); setCollectionId(""); setFamily(""); setNamePreview([]); setRetryFiles(undefined);
    if (libraries.data?.items.find(item => item.id === id)?.kind !== "regular") {
      setAuthor(undefined); setAuthorDraft(""); setSources([]);
    }
  };
  const add = async (input: readonly File[] | DataTransfer) => {
    if (blocked || intake.current || !selected || !upload.queue || !settings.data) {
      if (blocked) setError(new Error(blocked));
      return;
    }
    intake.current = true;
    setError(null); setRetryFiles(undefined);
    let files: readonly File[] | undefined;
    try {
      setBusy("正在读取选定文件…");
      const limit = Math.min(100, settings.data.maxBatchItems);
      files = Array.isArray(input) ? input : await droppedFiles(input as DataTransfer, limit);
      if (!files.length) return;
      if (files.length > limit) throw new Error("本批最多 " + limit + " 个文件，请分批选取。");
      const naming = {
        libraryId: selected.id,
        relativeDirectory: directory || undefined,
        family: mode === "ordinary" && (automaticName || collectionId) ? family.trim() || undefined : undefined,
        collectionId: collectionId || undefined,
        note,
      };
      if (mode === "ordinary" && (automaticName || collectionId)) {
        setBusy("正在预览选定文件的名称…");
        setNamePreview(await previewNamingFiles(files, naming, collections.find(item => item.id === collectionId)?.kind));
      } else setNamePreview([]);
      setBusy("正在登记本批文件…");
      await upload.queue.add(files, {
        ...naming, mode, authorId: canAttach ? author?.id : undefined,
        authorAcquisitionSources: canAttach && author && sources.length ? sources : undefined,
      }, selected.name, canAttach ? author?.name : undefined);
    } catch (failure) {
      setError(failure);
      if (!(failure instanceof QueuedPreparationError)) setRetryFiles(files);
    } finally { intake.current = false; setBusy(""); }
  };
  const pick = (element: HTMLInputElement) => {
    const files = Array.from(element.files ?? []);
    element.value = ""; void add(files);
  };
  const entries = upload.entries;
  const unsettled = entries.some(item => !isQueueTerminal(item.phase));
  const count = (phase: string) => entries.filter(item => item.phase === phase).length;
  const authorLabel = !canAttach ? "按库规则关联作者" : author?.name ?? (authorDraft ? "作者待确认" : "不指定作者");
  return (
    <div className="page manage-workspace" id="page-maintenance-files-upload">
      <ManagePageHeader title="上传文件" description="选好位置，直接添加文件；作者和来源按需设置" actions={<Link className="manage-text-link" to="/manage/files">整理已入库文件 →</Link>} />
      <ErrorNotice error={libraries.error ?? settings.error} stale={!!libraries.data && !!settings.data} retry={() => { void libraries.refetch(); void settings.refetch(); }} />
      <div className="upload-workbench">
        <div className="upload-layout">
          <div className="upload-setup">
            <UploadDestinationCard key={libraryId} libraries={libraries.data?.items ?? []} libraryId={libraryId} directory={directory} targetsReady={targetsReady && !busy} onLibrary={chooseLibrary} onDirectory={setDirectory} />
            <details className="chart-card upload-metadata-card">
              <summary><span className="upload-step">02</span><span><strong>作者与来源</strong><small>{author ? author.name + " · " + sources.length + " 个来源" : authorDraft ? "作者待确认 · 请选择建议中的已有作者" : "可选 · 不指定作者也能上传"}</small></span><ChevronDown aria-hidden /></summary>
              <div className="upload-metadata-body">
                <p className="rank-note">设置只用于之后添加的文件，已入队任务保持原关联。</p>
                <AuthorSuggestField key={canAttach ? "regular" : "closed"} label="批次作者（可选，联想选择）" hint="已选作者，上传成功后自动关联" value={author} disabled={!targetsReady || !canAttach || !!busy} onDraftChange={setAuthorDraft} onChoose={next => { if (next?.id !== author?.id) setSources([]); setAuthor(next ?? undefined); }} />
                {canAttach && authorDraft && !author && <p className="upload-err" role="status">请从建议中选定已有作者，或清空作者后直接上传。</p>}
                <AcquisitionSourceField label="批次来源（可选）" hint={author ? "选中的获取渠道会追加到该作者的来源记录；留空不改动。" : "先选定作者，再设置获取来源"} disabled={!canAttach || !author || !!busy} selected={sources} onChange={setSources} />
                {!canAttach && <p className="rank-note">{selected ? "当前库按目录或服务端规则关联作者，不支持手动挂靠。" : "选择支持作者关联的目标库后，可在这里设置。"}</p>}
                <Link className="manage-text-link" to="/manage/acquisition">管理来源建议词与作者镜像</Link>
              </div>
            </details>
          </div>
          <section className="chart-card upload-intake">
            <div className="upload-section-head"><span className="upload-step">03</span><h3>添加文件</h3><UploadCloud aria-hidden /></div>
            <div className={"upload-drop" + (over ? " over" : "")} role="region" aria-label="拖放上传区" onDragOver={event => { event.preventDefault(); setOver(true); }} onDragLeave={() => setOver(false)} onDrop={event => { event.preventDefault(); setOver(false); void add(event.dataTransfer); }}>
              <span className="upload-drop-icon"><FileUp aria-hidden /></span><h4>把文件或文件夹拖到这里</h4><p>文件夹会递归读取，选择后立即上传</p>
              <button className="save-btn" type="button" disabled={!!blocked} onClick={() => filePicker.current?.click()}>选择文件并立即上传</button>
              <span className="upload-limit">类型与大小由服务器校验</span>
            </div>
            <div className="upload-destination-summary" role="group" aria-label="本批上传摘要"><span className="upload-field-label">本批文件将上传到</span><strong>{selected?.name ?? "尚未选择目标库"}</strong><span className="upload-summary-path">{directory || "库根目录"}</span><div className="upload-summary-tags"><span>{authorLabel}</span>{canAttach && author && !!sources.length && <span>来源：{sources.join("、")}</span>}{mode === "intelligent" && <span>智能整理</span>}</div></div>
            <div className={"upload-ready" + (!blocked ? " upload-ready--ok" : "")} role="status">{blocked || "已就绪 · 修改左侧设置只影响下一批文件"}</div>
            <details className="upload-options">
              <summary>上传方式与作品命名</summary>
              <div className="form-stack">
                <label>上传方式<select value={mode} disabled={!!busy} onChange={event => setMode(event.target.value as typeof mode)}><option value="ordinary">普通上传</option><option value="intelligent">智能整理</option></select></label>
                {mode === "intelligent" && <p className="rank-note">{instance.capabilities.aiConfigured ? "接收后由本批智能任务逐项处理。" : "当前未配置 AI 执行器，接收后将等待执行器。"}</p>}
                <label>组图或 AI 短剧<select value={collectionId} disabled={!!busy || !selected} onChange={event => setCollectionId(event.target.value)}><option value="">不加入集合</option>{collections.map(collection => <option key={collection.id} value={collection.id}>{collection.name} · {collection.kind === "short_drama" ? "短剧" : "组图"}</option>)}</select></label>
                {collectionQuery.hasNextPage && <button className="pill" type="button" disabled={collectionQuery.isFetchingNextPage} onClick={() => void collectionQuery.fetchNextPage()}>更多集合</button>}
                <ErrorNotice error={collectionQuery.error} retry={() => void collectionQuery.refetch()} />
                {mode === "ordinary" && <NamingControls libraryId={libraryId} directory={directory} collectionId={collectionId} enabled={automaticName} family={family} setEnabled={setAutomaticName} setFamily={setFamily} busy={!!busy} />}
                <label>本批个人备注<textarea value={note} disabled={!!busy} onChange={event => setNote(event.target.value)} maxLength={10000} rows={2} /></label>
                <div className="pill-row"><button className="pill" type="button" disabled={!!blocked} onClick={() => directoryPicker.current?.click()}>选择文件夹并立即上传</button><Link className="manage-text-link" to="/manage/tasks">任务与报告</Link></div>
              </div>
            </details>
            <ErrorNotice error={error} />
            {retryFiles && <button className="pill" type="button" disabled={!!blocked} onClick={() => void add(retryFiles)}>按当前目标重试选定文件</button>}
          </section>
        </div>
        <input ref={filePicker} type="file" multiple hidden aria-label="选择要上传的文件" onChange={event => pick(event.target)} />
        <input ref={directoryPicker} type="file" multiple hidden {...{ webkitdirectory: "" }} aria-label="选择要上传的文件夹" onChange={event => pick(event.target)} />
        <section className="chart-card upload-queue" aria-label="上传任务">
          <div className="upload-queue-head"><div><h3><ListChecks aria-hidden />上传队列 <span>{entries.length}</span></h3><p className="rank-note">{entries.length ? "成功 " + count("ingested") + " · 失败 " + count("failed") + " · 待整理 " + count("staged") + " · 待重选 " + count("waiting_file") : "本次会话的进度与结果"}</p></div><div className="upload-queue-actions">
            {unsettled && <button className="pill" type="button" onClick={() => void upload.queue?.cancelAll()}>全部取消</button>}
            {entries.some(item => isQueueTerminal(item.phase)) && <button className="pill" type="button" title="仅移除已结束任务的列表记录，不删除已上传文件" onClick={() => void upload.queue?.clearFinished()}>清理已结束记录</button>}
          </div></div>
          {upload.persistenceError && <div className="manage-notice manage-notice--warning" role="alert"><span>{upload.persistenceError}</span><button className="pill" type="button" onClick={() => void upload.queue?.retryStorage()}>重试队列存储</button></div>}
          {namePreview.length > 0 && <details><summary>本次选取的命名预览（{namePreview.length} 项）</summary><p className="rank-note">入队时的预览不占用编号，最终名称以服务器回执为准。</p>{namePreview.map((item, index) => <p key={index}>{item.sourceName} → {item.relativePath}</p>)}</details>}
          {!upload.initialized && settings.data ? <Loading text="正在恢复队列…" /> : !entries.length ? <div className="upload-queue-empty"><ListChecks aria-hidden /><div><strong>暂无上传任务</strong><p>添加文件后，在这里查看进度、结果和恢复操作。</p></div></div> : <ul className="upload-task-list">{entries.map(entry => <UploadRow key={entry.id} entry={entry} />)}</ul>}
        </section>
      </div>
    </div>
  );
}
