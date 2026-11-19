import type { Schema } from "../data/client";

type Policy = Schema<"AIDelegationPolicy">;
export const assetFieldNames: Record<Schema<"AssetMaintenanceField">, string> = {
  authors: "作者关联", taxonomy: "出处与角色", tags: "标签", rename: "文件名",
};
export const providerNames: Record<Schema<"ResearchProviderName">, string> = {
  fxtwitter: "FxTwitter（X 公开账号与帖子）", pixiv: "Pixiv", yandere: "yande.re", e621: "e621",
  wayback: "Wayback Machine", steam: "Steam 创意工坊", bing: "Bing 搜索",
};
export const fieldNames = {
  name: "作者名称",
  biography: "简短介绍",
  homepage: "主主页",
  aliases: "别名",
};
export const blockNames = {
  x: "X 公开资料",
  basic: "整理简介与备注",
  accounts: "账号与发布入口",
};

export function PolicySummary({ policy }: { policy: Policy }) {
  return (
    <dl className="facts">
      <div>
        <dt>读取</dt>
        <dd>全库资料</dd>
      </div>
      <div>
        <dt>入库与整理</dt>
        <dd>
          {policy.canIngest
            ? `接收文件，${policy.libraryIds.length} 个库`
            : "未授权接收"}
          {policy.canOrganize ? " · 允许智能整理" : ""}
          {policy.canRecycleSources ? " · 备份核验后可回收来源" : ""}
        </dd>
      </div>
      <div><dt>已有作品</dt><dd>{policy.canResearchAssets ? "可研究所选库的媒体预览" : "未授权研究媒体预览"} · {(policy.assetFields ?? []).map(field => assetFieldNames[field]).join("、") || "未授权修改作品字段"}{(policy.canResearchAssets || !!policy.assetFields?.length) && " · " + policy.libraryIds.length + " 个库"}</dd></div>
      <div>
        <dt>研究</dt>
        <dd>
          {policy.canResearch
            ? `允许，每任务最多 ${policy.maxResearchCalls} 次`
            : "未授权"}
        </dd>
      </div>
      {!!policy.researchProviders?.length && <div><dt>允许发送文字查询的公开来源</dt><dd>{policy.researchProviders.map(name => providerNames[name]).join("、")}</dd></div>}
      <div>
        <dt>作者范围</dt>
        <dd>
          {policy.allAuthors
            ? "全部作者"
            : `${policy.authorIds.length} 位指定作者`}
        </dd>
      </div>
      <div>
        <dt>资料候选</dt>
        <dd>
          {policy.candidateBlocks
            .map((block) => blockNames[block])
            .join("、") || "未授权"}
        </dd>
      </div>
      <div>
        <dt>直接维护字段</dt>
        <dd>
          {policy.allowedFields.map((field) => fieldNames[field]).join("、") ||
            "未授权"}
          {policy.allowClear ? " · 允许明确清空" : ""}
        </dd>
      </div>
      <div>
        <dt>任务预算</dt>
        <dd>
          每日 {policy.maxTasksPerDay} 个 · 每任务 {policy.maxCallsPerTask}{" "}
          次工具调用 / {policy.taskSeconds} 秒
        </dd>
      </div>
      <div>
        <dt>资料提交</dt>
        <dd>每任务最多 {policy.maxUpdates} 次</dd>
      </div>
    </dl>
  );
}
