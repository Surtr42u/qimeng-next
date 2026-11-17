import type { Schema } from "../data/client";
import { profileFieldLabels, profileFieldNames } from "../lib/profile-draft";

type Evidence = Schema<"AuthorProfileEvidence">;
export function ProfileEvidenceEditor({
  block,
  evidence,
  change,
}: {
  block: Schema<"AuthorProfileBlock">["block"];
  evidence: Evidence[];
  change: (next: Evidence[]) => void;
}) {
  const update = (index: number, patch: Partial<Evidence>) =>
    change(
      evidence.map((item, at) => (at === index ? { ...item, ...patch } : item)),
    );
  return (
    <details className="profile-evidence-editor">
      <summary>来源与核实证据（{evidence.length}）</summary>
      {evidence.map((item, index) => (
        <fieldset className="form-stack profile-evidence-row" key={index}>
          <legend>证据 {index + 1}</legend>
          <label>
            公开来源网址
            <input
              type="url"
              required
              maxLength={4096}
              value={item.url}
              onChange={(event) => update(index, { url: event.target.value })}
            />
          </label>
          <label>
            来源标题
            <input
              maxLength={400}
              value={item.title}
              onChange={(event) => update(index, { title: event.target.value })}
            />
          </label>
          <div>
            <span>支持哪些字段</span>
            <div className="actions">
              {profileFieldNames[block].map((field) => (
                <label className="check-label" key={field}>
                  <input
                    type="checkbox"
                    checked={item.fields.includes(field)}
                    onChange={(event) =>
                      update(index, {
                        fields: event.target.checked
                          ? [...item.fields, field]
                          : item.fields.filter((value) => value !== field),
                      })
                    }
                  />
                  {profileFieldLabels[field]}
                </label>
              ))}
            </div>
          </div>
          <label>
            核实状态
            <select
              value={item.status}
              onChange={(event) =>
                update(index, {
                  status: event.target.value as Evidence["status"],
                  checkedAt:
                    event.target.value === "confirmed"
                      ? new Date().toISOString()
                      : undefined,
                })
              }
            >
              <option value="unverified">尚未核对</option>
              <option value="candidate">候选证据</option>
              <option value="confirmed">我已核对</option>
            </select>
          </label>
          <label>
            证据说明
            <textarea
              maxLength={4000}
              value={item.note}
              onChange={(event) => update(index, { note: event.target.value })}
            />
          </label>
          <button
            type="button"
            className="button button--small"
            onClick={() => change(evidence.filter((_, at) => at !== index))}
          >
            移除此证据
          </button>
        </fieldset>
      ))}
      <button
        type="button"
        className="button button--small"
        disabled={evidence.length >= 20}
        onClick={() =>
          change([
            ...evidence,
            { url: "", title: "", note: "", status: "unverified", fields: [] },
          ])
        }
      >
        添加来源证据
      </button>
    </details>
  );
}
