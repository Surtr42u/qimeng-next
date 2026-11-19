import { SubscriptionEditor } from "./CreatorSubscription";
import { useAuthorUpdates } from "../data/author-updates";
import { useState } from "react";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router-dom";
import type { Schema } from "../data/client";
import {
  useAuthorProfile,
  useAuthorProfileCandidates,
} from "../data/author-profiles";
import { ErrorNotice, Loading } from "../components/Feedback";
import {
  AuthorAvatar,
  ProfileEvidenceList,
  ProfileLink,
  ProfileValuesView,
} from "./ProfilePresentation";
import { ProfileBlockEditor } from "./ProfileBlockEditor";
import { ProfileAvatarEditor } from "./ProfileAvatarEditor";
import { AuthorDetails } from "./AuthorDetails";
import { AuthorOperations } from "./AuthorOperations";
import { useAuthor } from "../data/manage";

export function AuthorProfilePage() {
  const { id = "" } = useParams();
  const author = useAuthor(id);
  return (
    <div className="page" id="page-author-profile">
      <header className="manage-page-head">
        <Link className="manage-back" to={`/authors/${encodeURIComponent(id)}`}>
          ← 返回作者主页
        </Link>
        <div className="manage-heading">
          <div>
            <h2 data-page-heading tabIndex={-1}>
              作者资料
            </h2>
            <p>{author.data?.name ?? "账号、平台与核验信息"}</p>
          </div>
        </div>
      </header>
      {author.isPending ? (
        <Loading />
      ) : author.data ? (
        <ProfileDetails author={author.data} />
      ) : (
        <ErrorNotice error={author.error} retry={() => void author.refetch()} />
      )}
    </div>
  );
}

function ProfileGroup({
  title,
  disabled,
  edit,
  children,
}: {
  title: string;
  disabled: boolean;
  edit: () => void;
  children: ReactNode;
}) {
  return (
    <section className="creator-readonly-group">
      <div className="creator-group-heading">
        <h3>{title}</h3>
        <button
          type="button"
          className="pill"
          disabled={disabled}
          onClick={edit}
          aria-label={`编辑${title}`}
        >
          编辑
        </button>
      </div>
      {children}
    </section>
  );
}

function ProfileDetails({ author }: { author: Schema<"Author"> }) {
  const query = useAuthorProfile(author.id);
  const feed = useAuthorUpdates(author.id);
  const identity = useAuthor(author.id);
  const candidates = useAuthorProfileCandidates(author.id);
  const [editing, setEditing] = useState<
    | Schema<"AuthorProfileBlock">["block"]
    | "avatar"
    | "identity"
    | "subscription"
  >();
  const [candidate, setCandidate] =
    useState<Schema<"AuthorProfileCandidate">>();
  const profile = query.data;
  const x = profile?.blocks.find((block) => block.block === "x");
  const basic = profile?.blocks.find((block) => block.block === "basic");
  const accounts = profile?.blocks.find((block) => block.block === "accounts");
  const close = () => {
    setEditing(undefined);
    setCandidate(undefined);
  };
  const block = profile?.blocks.find(
    (item) =>
      item.block === editing,
  );
  const pending =
    candidates.data?.pages
      .flatMap((page) => page.items)
      .filter((item) => item.state === "pending") ?? [];
  return (
    <section
      className="rank-card creator-profile creator-details"
      aria-label="作者档案"
    >
      {editing === "subscription" ? <SubscriptionEditor id={author.id} name={author.name} avatar={x?.avatar} verified={accounts?.values.identityStatus === "verified"} close={close} /> : editing && block ? (
        <ProfileBlockEditor
          key={editing}
          block={block}
          candidate={candidate}
          reload={() => query.refetch()}
          close={close}
          authorName={author.name}
          avatar={x?.avatar}
        />
      ) : editing === "avatar" && x ? (
        <ProfileAvatarEditor
          block={x}
          name={author.name}
          reload={() => query.refetch()}
          close={close}
        />
      ) : editing === "identity" ? (
        <AuthorDetails
          author={identity.data ?? author}
          close={close}
          reload={() => identity.refetch()}
        />
      ) : (
        <>
          <div className="creator-profile-identity">
            <AuthorAvatar
              className="creator-profile-avatar"
              name={author.name}
              avatar={x?.avatar}
            />
            <div className="creator-profile-name">
              <h2>{author.name}</h2>
              <p>
                {accounts?.values.identityStatus === "verified"
                  ? "身份已确认"
                  : accounts?.values.identityStatus === "candidate"
                    ? "资料待核实"
                    : "资料待完善"}
              </p>
            </div>
          </div>
          <h3 className="creator-details-title">资料与维护</h3>
          {query.isPending && <Loading text="正在读取作者资料…" />}
          <ErrorNotice
            error={query.error}
            stale={!!profile}
            retry={() => void query.refetch()}
          />
          {profile && (
            <div className="creator-readonly-grid">
              <ProfileGroup
                title="X资料"
                disabled={!profile.editable}
                edit={() => setEditing("x")}
              >
                <p className="creator-profile-bio">
                  {x?.values.biographyOriginal || "尚未收录X简介。"}
                </p>
                {x?.values.biographyZh && (
                  <details className="creator-records">
                    <summary>查看中文译文</summary>
                    <p className="creator-profile-bio" lang="zh-CN">
                      {x.values.biographyZh}
                    </p>
                  </details>
                )}
                {x?.values.profileUrl && (
                  <ProfileLink value={x.values.profileUrl}>
                    前往 X ↗
                  </ProfileLink>
                )}
              </ProfileGroup>
              <ProfileGroup
                title="基础资料"
                disabled={!profile.editable}
                edit={() => setEditing("basic")}
              >
                <p className="creator-group-label">加入记录</p>
                <p>未记录清单顺序</p>
                <p>入库记录 {new Date(author.createdAt).toLocaleString()}</p>
                <p className="rank-note">原始加入日期未记录。</p>
                <p className="creator-group-label">公开昵称</p>
                <p>{basic?.values.publicName || "尚未填写"}</p>
                <p className="creator-group-label">整理简介</p>
                <p className="creator-profile-bio creator-bio-preview">
                  {basic?.values.summary?.split(/\n\s*\n/, 1)[0] || "尚未填写"}
                </p>
                {(basic?.values.summary ||
                  basic?.values.remark ||
                  basic?.values.note) && (
                  <details className="creator-records">
                    <summary>查看完整说明与核验记录</summary>
                    <p className="creator-profile-bio">
                      {basic.values.summary}
                    </p>
                    {basic.values.remark && (
                      <p className="creator-profile-bio">
                        {basic.values.remark}
                      </p>
                    )}
                    {basic.values.note && (
                      <p className="creator-profile-bio">{basic.values.note}</p>
                    )}
                    <ProfileEvidenceList evidence={basic.evidence} />
                  </details>
                )}
                <button
                  type="button"
                  className="pill"
                  disabled={!profile.editable}
                  onClick={() => setEditing("avatar")}
                >
                  离线头像
                </button>
              </ProfileGroup>
              <ProfileGroup
                title="账号来源"
                disabled={!profile.editable}
                edit={() => setEditing("accounts")}
              >
                <nav className="creator-platforms" aria-label="作者公开平台">
                  {accounts?.values.accounts
                    ?.filter((item) => item.url)
                    .map((item, index) => (
                      <ProfileLink key={index} value={item.url}>
                        {item.platform} ↗
                      </ProfileLink>
                    ))}
                  {author.homepage && (
                    <ProfileLink value={author.homepage}>
                      创作者主页 ↗
                    </ProfileLink>
                  )}
                </nav>
                <p className="creator-group-label">公开资料检查 · 已关闭</p>
                <p className="creator-profile-note">
                  通过已配置的 AI 接入核对来源，结果先进入候选。
                </p>
                <Link className="pill" to="/settings/ai">
                  AI 接入设置
                </Link>
                {accounts && (
                  <details className="creator-records">
                    <summary>查看核验记录</summary>
                    <ProfileEvidenceList evidence={accounts.evidence} />
                  </details>
                )}
              </ProfileGroup>
              <ProfileGroup
                title="作品订阅"
                disabled={!profile.editable}
                edit={() => setEditing("subscription")}
              >
                <ErrorNotice error={feed.error} stale={!!feed.data} retry={() => void feed.refetch()} />
                {feed.data ? <><p>{feed.data.feedUrl ? "已配置作品发布来源" : "尚未配置作品发布订阅"}</p><p className="creator-group-label">订阅检查 · {feed.data.trackUpdates ? "已开启" : "已关闭"}</p>{feed.data.feedUrl && <ProfileLink value={feed.data.feedUrl}>查看订阅来源 ↗</ProfileLink>}</> : feed.isPending && <Loading />}
              </ProfileGroup>
            </div>
          )}
          <ErrorNotice
            error={candidates.error}
            stale={pending.length > 0}
            retry={() => void candidates.refetch()}
          />
          {pending.map((item) => (
            <section
              key={item.id}
              className="creator-candidate"
              aria-label="待审核资料"
            >
              <h3>待核对的候选资料</h3>
              <p>核对候选资料与来源后保存。</p>
              <ProfileValuesView
                block={item.block}
                values={item.values}
                onlyPresent
              />
              <nav className="creator-platforms">
                {item.evidence.map((evidence, index) => (
                  <ProfileLink key={index} value={evidence.url}>
                    {evidence.title || evidence.url} ↗
                  </ProfileLink>
                ))}
              </nav>
              <button
                type="button"
                className="pill"
                disabled={!profile?.editable}
                onClick={() => {
                  setCandidate(item);
                  setEditing(item.block);
                }}
              >
                填入候选草稿，继续核对
              </button>
            </section>
          ))}
          {candidates.hasNextPage && (
            <button
              className="pill"
              disabled={candidates.isFetchingNextPage}
              onClick={() => void candidates.fetchNextPage()}
            >
              更多候选记录
            </button>
          )}
          <details className="creator-records">
            <summary>身份管理与历史归档</summary>
            <button className="pill" onClick={() => setEditing("identity")}>
              编辑身份资料
            </button>
            <Link
              className="pill"
              to={`/authors/${encodeURIComponent(author.id)}/archives`}
            >
              历史归档
            </Link>
            <AuthorOperations author={author} disabled={false} />
          </details>
        </>
      )}
    </section>
  );
}
