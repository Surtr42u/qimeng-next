import { useId, useRef, useState } from "react";
import { Popover } from "radix-ui";
import { X } from "lucide-react";
import { useAuthors } from "../data/manage";
import type { Schema } from "../data/client";
import { ErrorNotice } from "./Feedback";

// Shared display structure from the existing manage/AuthorSuggestField.
export function AuthorSuggestField({
  value, selectedIds = [], disabled = false, label = "作者", hint,
  onChoose, onDraftChange,
}: {
  value?: Schema<"Author">;
  selectedIds?: string[];
  disabled?: boolean;
  label?: string;
  hint?: string;
  onChoose: (author: Schema<"Author"> | null) => void;
  onDraftChange?: (text: string) => void;
}) {
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const listId = useId();
  const display = value?.name ?? text;
  const query = useAuthors(display, undefined, open && !disabled, { kind: "regular" });
  const rows = (query.data?.pages.flatMap(page => page.items) ?? []).filter(author => !author.placeholder);
  const pick = (author: Schema<"Author">) => {
    onChoose(author); setText(""); onDraftChange?.(""); setOpen(false);
  };
  return (
    <div className="settings-field upload-attach-field">
      <span>{label}</span>
      <Popover.Root open={open && !disabled} onOpenChange={next => { if (!disabled) setOpen(next); }}>
        <Popover.Anchor asChild>
          <div className="attach-input-box">
            <input
              ref={input}
              aria-label={label}
              placeholder="输入作者名联想选择"
              autoComplete="off"
              value={display}
              disabled={disabled}
              aria-expanded={open && !disabled}
              aria-controls={open ? listId : undefined}
              onFocus={() => setOpen(true)}
              onChange={event => { if (value) onChoose(null); setText(event.target.value); onDraftChange?.(event.target.value); setOpen(true); }}
              onKeyDown={event => {
                if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                  event.preventDefault();
                  const exact = rows.find(author => author.name === display);
                  if (exact && !selectedIds.includes(exact.id)) pick(exact);
                }
              }}
            />
            {display && <button className="attach-clear" type="button" aria-label="清除已选作者" disabled={disabled} onClick={() => { onChoose(null); setText(""); onDraftChange?.(""); input.current?.focus(); }}><X size={14} /></button>}
          </div>
        </Popover.Anchor>
        <Popover.Portal>
          <Popover.Content className="search-pop search-pop--popper" side="bottom" align="start" sideOffset={8} onOpenAutoFocus={event => event.preventDefault()} onInteractOutside={event => { if (event.target instanceof Element && event.target.closest(".attach-input-box")) event.preventDefault(); }}>
            <ErrorNotice error={query.error} retry={() => void query.refetch()} />
            {query.isPending ? <p className="pop-suggest-hint">正在获取联想…</p> : (
              <div id={listId} className="pop-suggest-list" role="group" aria-label="作者联想">
                {rows.map(author => <button key={author.id} className="pop-suggest-item" type="button" disabled={disabled || selectedIds.includes(author.id)} onClick={() => pick(author)}><span className="pop-suggest-name">{author.name}</span><span className="pop-suggest-badge">{author.fileCount === undefined ? "" : `${author.fileCount} 个文件`}</span></button>)}
                {!rows.length && !query.isError && <p className="pop-suggest-hint">没有匹配的作者</p>}
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
