import { FolderPlus } from "lucide-react";
import { useDirectories } from "../data/catalog";
import type { Schema } from "../data/client";
import { useNearViewport } from "../lib/visibility";
import { ErrorNotice, Loading } from "./Feedback";

type Props = { libraryId: string; selected: string; onSelect: (path: string) => void; disabled?: boolean; onCreate?: (path: string) => void };
export function DirectoryTree(props: Props) {
  return <ul className="dir-tree-list"><DirectoryRow {...props} depth={0} /></ul>;
}
function DirectoryRow({ node, depth, ...props }: Props & { node?: Schema<"Directory">; depth: number }) {
  const { ref, visible } = useNearViewport<HTMLButtonElement>("viewport");
  const path = node?.relativePath ?? "";
  const selected = path === props.selected;
  const active = !node || node.hasChildren !== false && (visible || selected || props.selected.startsWith(path + "/"));
  const children = useDirectories(props.libraryId, path, active);
  const count = node?.fileCount ?? children.data?.pages[0]?.directFileCount;
  const row = <button ref={ref} className={"dir-node" + (selected ? " selected" : "")} type="button" aria-pressed={selected} disabled={props.disabled}
    style={props.onCreate ? undefined : { paddingLeft: depth * 16 + 8 }} onClick={() => props.onSelect(path)}>
    <span className="dir-name">{node?.name ?? "库根"}</span><span className="dir-count">{count ?? "—"} 文件</span>
  </button>;
  return <li>
    {props.onCreate ? <div className="dir-row" style={{ paddingLeft: depth * 16 }}>{row}<span className="dir-actions"><button className="dir-action-btn" type="button" title="在此目录下新建子目录" disabled={props.disabled} onClick={() => props.onCreate?.(path)}><FolderPlus width={14} height={14} />新建子目录</button></span></div> : row}
    {active && <><ErrorNotice error={children.error} stale={!!children.data} retry={() => void children.refetch()} />
      {!node && children.isPending && <Loading text="目录加载中…" />}
      {!!children.data?.pages.some(page => page.items.length) && <ul className="dir-tree-list">{children.data.pages.flatMap(page => page.items).map(item => <DirectoryRow key={item.relativePath} {...props} depth={depth + 1} node={item} />)}</ul>}
      {children.hasNextPage && <button className="pill" type="button" disabled={children.isFetchingNextPage || props.disabled} onClick={() => void children.fetchNextPage({ cancelRefetch: false })}>更多目录</button>}
    </>}
  </li>;
}
