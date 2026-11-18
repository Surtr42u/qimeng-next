import { useEffect, useId, useRef, useState } from "react";
import { Popover } from "radix-ui";
import { X } from "lucide-react";
import { useAuthors } from "../data/manage";
import type { Schema } from "../data/client";
import { ErrorNotice } from "./Feedback";

export function AuthorSuggestField({
  value, selectedIds = [], disabled = false, label = "作者", hint, onChoose, onDraftChange, onCreate,
}: {
  value?: Schema<"Author">;
  selectedIds?: string[];
  disabled?: boolean;
  label?: string;
  hint?: string;
  onChoose: (author: Schema<"Author"> | null) => void;
  onDraftChange?: (text: string) => void;
  onCreate?: (name: string) => void;
}) {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [term, setTerm] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const anchor = useRef<HTMLDivElement>(null);
  const listId = useId();
  const display = value?.name ?? text;
  const search = display.trim();
  const expanded = open && !disabled && !value && !!search;
  useEffect(() => {
    const timer = window.setTimeout(() => setTerm(search), 180);
    return () => window.clearTimeout(timer);
  }, [search]);
  const query = useAuthors(term, undefined, expanded && !!term && term === search, { kind: "regular" });
  const rows = (query.data?.pages.flatMap(page => page.items) ?? []).filter(author => !author.placeholder);
  const matches = term === search && query.isSuccess;
  const exact = matches ? rows.filter(author => [author.name, ...(author.aliases ?? [])].some(name => name.localeCompare(search, undefined, { sensitivity: "accent" }) === 0)) : [];
  const canCreate = !!onCreate && matches && !exact.length;
  const clearText = () => { setText(""); onDraftChange?.(""); setOpen(false); };
  const pick = (author: Schema<"Author">) => { onChoose(author); clearText(); };
  const create = () => { if (canCreate) { onCreate?.(search); clearText(); } };
  return (
    <div className="settings-field upload-attach-field">
      <span>{label}</span>
      <Popover.Root open={expanded} onOpenChange={next => { if (!disabled) setOpen(next); }}>
        <Popover.Anchor asChild>
          <div className="attach-input-box" ref={anchor}>
            <input ref={input} aria-label={label} placeholder="输入作者名联想选择" autoComplete="off" maxLength={200}
              value={display} disabled={disabled} aria-expanded={expanded} aria-controls={expanded ? listId : undefined}
              onFocus={() => setOpen(true)}
              onChange={event => { if (value) onChoose(null); setText(event.target.value); onDraftChange?.(event.target.value); setOpen(true); }}
              onKeyDown={event => {
                if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  if (exact.length === 1 && !selectedIds.includes(exact[0]!.id)) pick(exact[0]!);
                  else create();
                }
              }}
            />
            {display && <button className="attach-clear" type="button" aria-label="清除已选作者" disabled={disabled} onClick={() => { onChoose(null); clearText(); input.current?.focus(); }}><X size={14} /></button>}
          </div>
        </Popover.Anchor>
        <Popover.Portal>
          <Popover.Content className="search-pop search-pop--popper" side="bottom" align="start" sideOffset={8}
            onOpenAutoFocus={event => event.preventDefault()}
            onInteractOutside={event => { if (event.target instanceof Node && anchor.current?.contains(event.target)) event.preventDefault(); }}>
            <ErrorNotice error={query.error} retry={() => void query.refetch()} />
            {query.isPending || term !== search ? <p className="pop-suggest-hint">正在获取联想…</p> : (
              <div id={listId} className="pop-suggest-list" role="group" aria-label="作者联想">
                {rows.map(author => <button key={author.id} className="pop-suggest-item" type="button" disabled={disabled || selectedIds.includes(author.id)} onClick={() => pick(author)}><span className="pop-suggest-name">{author.name}</span><span className="pop-suggest-badge">{author.fileCount === undefined ? "" : author.fileCount + " 个文件"}</span></button>)}
                {canCreate && <button type="button" className="pop-suggest-item" onClick={create}><span className="pop-suggest-name">新建：{search}</span><span className="pop-suggest-badge">保存时创建</span></button>}
                {!rows.length && !canCreate && !query.isError && <p className="pop-suggest-hint">没有匹配的作者</p>}
                {query.hasNextPage && <button className="pop-suggest-item" type="button" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>更多作者</button>}
              </div>
            )}
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
      <small>{value ? hint ?? "已选作者" : "输入名字从联想中点选要关联的作者"}</small>
    </div>
  );
}
