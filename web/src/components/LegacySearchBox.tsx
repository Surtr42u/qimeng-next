import { useEffect, useId, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useSearchSuggestions } from '../data/search';
import { ChevronDownIcon, ClearIcon, SearchIcon } from './LegacyShellIcons';

const historyKey = 'qimeng.search-history';
const types = { source: '出处', character: '角色', cosAuthor: 'COS作者', cosWork: 'COS作品', author: '作者' };
function storedHistory(): string[] {
  try { const value: unknown = JSON.parse(localStorage.getItem(historyKey) ?? '[]'); return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string').slice(0, 20) : []; }
  catch { return []; }
}

export function LegacySearchBox() {
  const location = useLocation();
  const navigate = useNavigate();
  const input = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLFormElement>(null);
  const id = useId();
  const [query, setQuery] = useState(new URLSearchParams(location.search).get('q') ?? '');
  const [debounced, setDebounced] = useState(query.trim());
  const [open, setOpen] = useState(false);
  const [opening, setOpening] = useState(0);
  const [history, setHistory] = useState(storedHistory);
  const [expanded, setExpanded] = useState(false);
  const [storageError, setStorageError] = useState(false);
  const [selected, setSelected] = useState(-1);
  const suggestion = useSearchSuggestions(debounced, opening, open);
  const suggestMode = !!query.trim();
  const current = query.trim() === debounced;
  const items = current ? suggestion.data?.items ?? [] : [];
  const recommended = [...new Set(items.map(item => item.name))];
  useEffect(() => { setQuery(new URLSearchParams(location.search).get('q') ?? ''); }, [location.pathname, location.search]);
  useEffect(() => { const timer = window.setTimeout(() => { setDebounced(query.trim()); setSelected(-1); }, 220); return () => window.clearTimeout(timer); }, [query]);
  useEffect(() => {
    if (!open) return;
    const closeOutside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false); };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [open]);
  const show = () => { if (!open) { setOpening(value => value + 1); setOpen(true); } };
  const store = (next: string[]) => { setHistory(next); try { localStorage.setItem(historyKey, JSON.stringify(next)); setStorageError(false); } catch { setStorageError(true); } };
  const search = (value: string) => {
    const word = value.trim(); if (!word) return;
    setQuery(word); setOpen(false); input.current?.blur();
    store([word, ...history.filter(item => item !== word)].slice(0, 20));
    navigate(`/search?q=${encodeURIComponent(word)}`);
  };
  return <form className="search" ref={root} role="search" onSubmit={event => { event.preventDefault(); search(selected >= 0 ? items[selected]?.name ?? query : query); }}>
    <input ref={input} type="text" placeholder="搜索你感兴趣的视频" aria-label="搜索媒体" aria-expanded={open} aria-controls={open ? id : undefined} aria-autocomplete="list" aria-activedescendant={selected >= 0 ? `${id}-${selected}` : undefined} autoComplete="off" maxLength={200} value={query} onChange={event => { setQuery(event.target.value); show(); }} onFocus={show} onClick={show} onKeyDown={event => {
      if (event.key === 'Escape') { event.preventDefault(); setOpen(false); setSelected(-1); }
      if ((event.key === 'ArrowDown' || event.key === 'ArrowUp') && suggestMode && items.length) { event.preventDefault(); setSelected(index => event.key === 'ArrowDown' ? (index + 1) % items.length : (index + items.length - 1) % items.length); }
    }} />
    {query && <button className="search-clear" type="button" aria-label="清空搜索词" onClick={() => { setQuery(''); input.current?.focus(); }}><ClearIcon /></button>}
    <button className="shell-search-submit" type="submit" aria-label="搜索"><SearchIcon className="search-icon" /></button>
    {open && <div className="search-pop" id={id}>
      {suggestMode ? !current || suggestion.isPending ? <p className="pop-suggest-hint">正在获取补全…</p> : suggestion.isError ? <p className="pop-suggest-hint" role="alert">搜索建议读取失败 <button className="pill" type="button" onClick={() => void suggestion.refetch()}>重试</button></p> : items.length ? <div className="pop-suggest-list" role="listbox">{items.map((item, index) => <button id={`${id}-${index}`} key={`${item.type}:${item.id}`} className="pop-suggest-item" type="button" role="option" aria-selected={selected === index} onClick={() => search(item.name)}><SearchIcon className="pop-suggest-icon" /><span className="pop-suggest-name">{item.name}</span><span className="pop-suggest-badge">{types[item.type]}</span></button>)}</div> : <p className="pop-suggest-hint">没有匹配的补全</p> : <>
        {history.length > 0 && <div className="pop-block"><div className="pop-head"><h3 className="pop-title">搜索历史</h3><button className="pop-clear" type="button" onClick={() => { store([]); setExpanded(false); }}>清空</button></div><div className="pop-history">{(expanded ? history : history.slice(0, 8)).map(word => <button key={word} className="pop-chip" type="button" onClick={() => search(word)}>{word}</button>)}</div>{history.length > 8 && <button className="pop-more" type="button" onClick={() => setExpanded(!expanded)}><span className="pop-more-text">{expanded ? '收起' : '展开更多'}</span><ChevronDownIcon /></button>}</div>}
        <div className="pop-block"><h3 className="pop-title">推荐搜索</h3>{suggestion.isPending ? <p className="pop-reco-hint">正在读取…</p> : suggestion.isError ? <p className="pop-reco-hint" role="alert">推荐搜索读取失败 <button className="pill" type="button" onClick={() => void suggestion.refetch()}>重试</button></p> : recommended.length ? <div className="pop-history">{recommended.map(word => <button key={word} className="pop-chip" type="button" onClick={() => search(word)}>{word}</button>)}</div> : <p className="pop-reco-hint">暂无推荐词——先扫描媒体库生成补全索引</p>}</div>
      </>}
      {storageError && <p className="pop-suggest-hint" role="status">本次搜索可用，浏览器无法保存搜索历史。</p>}
    </div>}
  </form>;
}
