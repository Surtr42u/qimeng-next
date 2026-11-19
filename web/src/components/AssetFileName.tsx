import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import "./AssetFileName.css";

// Pure display and clipboard behavior from the existing detail/AssetFileName.
export function AssetFileName({ fileName }: { fileName: string }) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  const timeout = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timeout.current), []);
  const copy = () => {
    if (!navigator.clipboard) { setError("当前环境不支持剪贴板，请手动选择文件名复制"); return; }
    void navigator.clipboard.writeText(fileName).then(() => {
      setError(""); setCopied(true); clearTimeout(timeout.current);
      timeout.current = setTimeout(() => setCopied(false), 1500);
    }, () => setError("复制文件名失败，请手动选择复制"));
  };
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap", marginTop: 6, fontSize: 13, color: "var(--qm-text-muted)" }}>
      <span style={{ overflowWrap: "anywhere", wordBreak: "break-all" }}>{fileName}</span>
      <button type="button" className="pill asset-file-copy" onClick={copy} title="复制完整文件名">
        {copied ? <Check aria-hidden size={14} /> : <Copy aria-hidden size={14} />}{copied ? "已复制" : "复制文件名"}
      </button>
      {error && <span role="alert">{error}</span>}
    </div>
  );
}
