export function LegacySegments<T extends string>({ label, value, options, onChange }: {
  label: string; value: T; options: readonly { value: T; label: string }[]; onChange: (value: T) => void;
}) {
  return <div className="workspace-segments" role="group" aria-label={label}>
    {options.map(item => <button key={item.value} type="button" aria-pressed={item.value === value}
      className={item.value === value ? "active" : ""} onClick={() => onChange(item.value)}>{item.label}</button>)}
  </div>;
}
