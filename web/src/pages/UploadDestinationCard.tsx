import { DirectoryTree } from "../components/DirectoryTree";
import { useId, useState } from "react";
import type { ReactNode } from "react";
import { ChevronDown, FolderOpen, Plus } from "lucide-react";
import type { Schema } from "../data/client";
import { useDirectories } from "../data/catalog";
import { CreateDirectoryDialog } from "./FileManagementDialogs";

export function UploadDestinationCard({ libraries, libraryId, directory, targetsReady, onLibrary, onDirectory, more }: {
  libraries: Schema<"Library">[];
  libraryId: string;
  directory: string;
  targetsReady: boolean;
  onLibrary: (id: string) => void;
  onDirectory: (path: string) => void;
  more?: ReactNode;
}) {
  const [browsing, setBrowsing] = useState(false);
  const [creating, setCreating] = useState(false);
  const panelId = useId();
  const enabled = targetsReady && !!libraries.find(library => library.id === libraryId && library.enabled);
  const query = useDirectories(libraryId, directory, enabled && browsing);
  return (
    <section className="chart-card upload-setup-card">
      <div className="upload-section-head"><span className="upload-step">01</span><h3>上传位置</h3><FolderOpen aria-hidden /></div>
      <div className="upload-field-label">目标库 <span>必选</span></div>
      <div className="source-chips upload-libraries" role="group" aria-label="目标库">
        {libraries.map(library => <button className={`pill${libraryId === library.id ? " active" : ""}`} type="button" key={library.id} aria-pressed={libraryId === library.id} disabled={!library.enabled || !targetsReady} onClick={() => onLibrary(library.id)}>{library.name}{library.enabled ? "" : " · 已停用"}</button>)}
        {!libraries.length && <p className="rank-note">{targetsReady ? "暂无可选库，请先到库管理登记媒体文件夹。" : "目标库列表尚不可用，请查看上方提示。"}</p>}
        {more}
      </div>
      <div className="upload-field-label">目标目录</div>
      <div className="upload-directory-choice">
        <div><strong>{directory || "库根目录"}</strong><small>{libraryId ? "新文件保存在此位置" : "选定目标库后可修改目录"}</small></div>
        <button className="pill" type="button" disabled={!enabled} aria-expanded={browsing} aria-controls={panelId} onClick={() => { setBrowsing(!browsing); }}>{browsing ? "收起目录" : "选择目录"}<ChevronDown aria-hidden /></button>
      </div>
      {browsing && enabled && (
        <div className="upload-directory-panel" id={panelId}>
          <div className="dir-tree"><DirectoryTree libraryId={libraryId} selected={directory} onSelect={path => { onDirectory(path); setBrowsing(false); }} /></div>
          <button className="manage-text-link" type="button" disabled={!query.data || query.isError} onClick={() => setCreating(true)}><Plus aria-hidden />在当前目录下新建</button>
        </div>
      )}
      {creating && <CreateDirectoryDialog libraryId={libraryId} parent={directory} close={() => setCreating(false)} />}
    </section>
  );
}
