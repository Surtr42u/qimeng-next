import { useId, useState } from "react";
import { ChevronDown, FolderOpen, Plus } from "lucide-react";
import type { Schema } from "../data/client";
import { useDirectories } from "../data/catalog";
import { ErrorNotice, Loading } from "../components/Feedback";
import { CreateDirectoryDialog } from "./FileManagementDialogs";

export function UploadDestinationCard({ libraries, libraryId, directory, targetsReady, onLibrary, onDirectory }: {
  libraries: Schema<"Library">[];
  libraryId: string;
  directory: string;
  targetsReady: boolean;
  onLibrary: (id: string) => void;
  onDirectory: (path: string) => void;
}) {
  const [browsing, setBrowsing] = useState(false);
  const [parent, setParent] = useState(directory);
  const [creating, setCreating] = useState(false);
  const panelId = useId();
  const enabled = targetsReady && !!libraries.find(library => library.id === libraryId && library.enabled);
  const query = useDirectories(libraryId, parent, enabled && browsing);
  const items = query.data?.pages.flatMap(page => page.items) ?? [];
  return (
    <section className="chart-card upload-setup-card">
      <div className="upload-section-head"><span className="upload-step">01</span><h3>上传位置</h3><FolderOpen aria-hidden /></div>
      <div className="upload-field-label">目标库 <span>必选</span></div>
      <div className="source-chips upload-libraries" role="group" aria-label="目标库">
        {libraries.map(library => <button className={`pill${libraryId === library.id ? " active" : ""}`} type="button" key={library.id} aria-pressed={libraryId === library.id} disabled={!library.enabled || !targetsReady} onClick={() => onLibrary(library.id)}>{library.name}{library.enabled ? "" : " · 已停用"}</button>)}
        {!libraries.length && <p className="rank-note">{targetsReady ? "暂无可选库，请先到库管理登记媒体文件夹。" : "目标库列表尚不可用，请查看上方提示。"}</p>}
      </div>
      <div className="upload-field-label">目标目录</div>
      <div className="upload-directory-choice">
        <div><strong>{directory || "库根目录"}</strong><small>{libraryId ? "新文件保存在此位置" : "选定目标库后可修改目录"}</small></div>
        <button className="pill" type="button" disabled={!enabled} aria-expanded={browsing} aria-controls={panelId} onClick={() => { setParent(directory); setBrowsing(!browsing); }}>{browsing ? "收起目录" : "选择目录"}<ChevronDown aria-hidden /></button>
      </div>
      {browsing && enabled && (
        <div className="upload-directory-panel" id={panelId}>
          <ErrorNotice error={query.error} stale={!!query.data} retry={() => void query.refetch()} />
          {query.isPending ? <Loading text="正在读取目录…" /> : (
            <div className="dir-tree">
              <div className="pill-row"><button className="pill" type="button" onClick={() => setParent("")}>库根</button>{parent && <button className="pill" type="button" onClick={() => setParent(parent.split("/").slice(0, -1).join("/"))}>上一级</button>}</div>
              <ul>
                <li><button className={`dir-node${directory === parent ? " selected" : ""}`} type="button" onClick={() => { onDirectory(parent); setBrowsing(false); }}>{parent || "库根目录"}</button></li>
                {items.map(item => <li key={item.relativePath}><button className="dir-node" type="button" onClick={() => setParent(item.relativePath)}><span className="dir-name">{item.name}</span></button></li>)}
              </ul>
              {query.hasNextPage && <button className="pill" type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>更多目录</button>}
            </div>
          )}
          <button className="manage-text-link" type="button" disabled={!query.data || query.isError} onClick={() => setCreating(true)}><Plus aria-hidden />在当前目录下新建</button>
        </div>
      )}
      {creating && <CreateDirectoryDialog libraryId={libraryId} parent={parent} close={() => setCreating(false)} />}
    </section>
  );
}
