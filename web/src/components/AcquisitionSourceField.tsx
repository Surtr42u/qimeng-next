import { useState } from 'react';
import { useAcquisitionVocabulary } from '../data/acquisition';
import { ErrorNotice } from './Feedback';

// Keeps the old SourceSelectField presentation. Suggested individual words are now Go-owned.
export function AcquisitionSourceField({ selected, onChange, disabled = false, label = '来源 / 出处', hint = '记录获取渠道、平台或个人链接，不参与作品匹配。移除全部并保存将清空记录。输入后按回车加入。' }: {
  selected: string[]; onChange: (next: string[]) => void; disabled?: boolean; label?: string; hint?: string;
}) {
  const vocabulary = useAcquisitionVocabulary();
  const [input, setInput] = useState('');
  const suggestions = vocabulary.data?.suggestions ?? [];
  const custom = selected.filter(value => !suggestions.includes(value));
  const toggle = (value: string) => onChange(selected.includes(value) ? selected.filter(item => item !== value) : [...selected, value]);
  const add = () => { const value = input.trim(); if (value && !selected.includes(value)) onChange([...selected, value]); setInput(''); };
  return <div className={`settings-field upload-attach-field${disabled ? ' disabled' : ''}`}>
    <span>{label}</span>
    <div className="source-chips">{[...suggestions, ...custom].map(value => <button type="button" key={value} className={`pill${selected.includes(value) ? ' active' : ''}`} disabled={disabled} aria-pressed={selected.includes(value)} onClick={() => toggle(value)}>{value}</button>)}{vocabulary.isPending && !selected.length && <span className="source-empty-hint">词表加载中…</span>}</div>
    <ErrorNotice error={vocabulary.error} stale={!!vocabulary.data} retry={() => void vocabulary.refetch()} />
    <div className="tag-newrow"><input className="tag-input" type="text" placeholder="新站点或 URL，回车加入" autoComplete="off" disabled={disabled} maxLength={2000} value={input} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); add(); } }} /><button type="button" className="pill" disabled={disabled || !input.trim()} onClick={add}>添加</button></div>
    <small>{hint}</small>
  </div>;
}
