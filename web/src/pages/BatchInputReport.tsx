import { Link } from 'react-router-dom';
import type { Schema } from '../data/client';
import { useCancelBatchInput } from '../data/manage';
import { ErrorNotice, StatusBadge } from '../components/Feedback';
import { bytes } from '../lib/format';
import { intakeFailure } from '../lib/intake-failure';

const labels: Record<Schema<'BatchInput'>['state'], string> = {
  pending: '尚未收到', receiving: '正在接收', staged: '已接收，待整理',
  ingested: '已入库', duplicate: '重复，未新增', cancelled: '已取消', failed: '失败',
};
export function BatchInputReport({ batch, inputs }: { batch: Schema<'Batch'>; inputs: Schema<'BatchInput'>[] }) {
  const cancel = useCancelBatchInput(batch.id);
  if (batch.manifestState === 'legacy_unverified') return <div className="notice notice--warning"><strong>原始输入数量未核验</strong><p>历史批次没有完整原始清单。下方上传记录不能用来推断当时选入的总数。</p></div>;
  if (batch.manifestState === 'open') return <div className="notice"><strong>原始输入清单尚未封存</strong><p>等待完整选择登记，当前没有开始传输新输入。</p></div>;
  return <section className="settings-card">
    <h3>原始输入</h3>
    <p>共 {batch.expectedInputs} 项，按选入顺序保留；未收到和已取消的条目仍在清单中。</p>
    <ErrorNotice error={cancel.error} />
    <div className="list-panel">{inputs.map(input => {
      const name = input.nameState === 'unavailable' ? `第${input.ordinal + 1}项（名称读取失败）` : input.originalFileName || `第${input.ordinal + 1}项（原名未记录）`;
      const rejected = intakeFailure(input);
      return <div className="list-row" key={input.id}>
        <span>{input.ordinal + 1}</span>
        <div className="row-main"><strong>{name}</strong>{input.sourceRelativePath && <span className="break-word">{input.sourceRelativePath}</span>}{input.size !== undefined && <small>{bytes(input.size)}</small>}{(rejected || input.errorCode) && <p role="status">{rejected ?? (input.errorCode === 'source_hash_mismatch' ? '源文件内容与选入时的快照不同' : input.errorCode === 'duplicate_content' ? '已有相同内容，未新增' : '本项处理异常，请展开下方上传结果核对。')}</p>}</div>
        <StatusBadge tone={input.state === 'ingested' ? 'success' : ['failed', 'duplicate'].includes(input.state) ? 'warning' : 'neutral'}>{labels[input.state]}</StatusBadge>
        {input.assetId && <Link className="pill" to={`/asset/${encodeURIComponent(input.assetId)}`}>查看媒体</Link>}
        {!rejected && !['ingested', 'cancelled'].includes(input.state) && <button className="pill" disabled={cancel.isPending} onClick={() => cancel.mutate({ inputId: input.id, fingerprint: batch.manifestFingerprint })}>取消本项</button>}
      </div>;
    })}</div>
  </section>;
}
