import { useState } from "react";
import type { ReactNode } from "react";
import type { Schema } from "../data/client";
import {
  profileAvatarURL,
  profileFieldLabels,
  profileFieldNames,
  publicProfileLink,
} from "../lib/profile-draft";

export function AuthorAvatar({
  name,
  avatar,
  large = false,
  className,
}: {
  name: string;
  avatar?: Schema<"AuthorProfileAvatar">;
  large?: boolean;
  className?: string;
}) {
  const url = profileAvatarURL(avatar?.url);
  const [failed, setFailed] = useState("");
  return (
    <span className={className ?? `avatar${large ? " avatar--large" : ""}`}>
      {url && failed !== url ? (
        <img
          src={url}
          alt={`${name}的离线头像`}
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setFailed(url)}
        />
      ) : (
        <span aria-label={avatar ? "离线头像不可用" : undefined}>
          {Array.from(name)[0] ?? "?"}
        </span>
      )}
    </span>
  );
}

export function ProfileValuesView({
  block,
  values,
  onlyPresent = false,
}: {
  block: Schema<"AuthorProfileBlock">["block"];
  values: Schema<"AuthorProfileValues">;
  onlyPresent?: boolean;
}) {
  const fields: (keyof Schema<"AuthorProfileValues">)[] =
    profileFieldNames[block];
  return (
    <dl className="profile-fields">
      {fields
        .filter((field) => !onlyPresent || values[field] !== undefined)
        .map((field) => (
          <div key={field}>
            <dt>{profileFieldLabels[field]}</dt>
            <dd>
              {field === "accounts" ? (
                values.accounts?.length ? (
                  <ul className="profile-accounts">
                    {values.accounts.map((account, index) => (
                      <li key={index}>
                        <strong>{account.platform}</strong> {account.identifier}
                        <div className="actions">
                          {account.url && (
                            <ProfileLink value={account.url}>
                              账号主页
                            </ProfileLink>
                          )}
                          {account.publishingUrl && (
                            <ProfileLink value={account.publishingUrl}>
                              作品发布入口
                            </ProfileLink>
                          )}
                        </div>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <span className="muted">未填写</span>
                )
              ) : (
                <span className="preserve-lines">
                  {values[field] || <span className="muted">未填写</span>}
                </span>
              )}
            </dd>
          </div>
        ))}
    </dl>
  );
}

export function ProfileLink({
  value,
  children,
}: {
  value: string;
  children?: ReactNode;
}) {
  const href = publicProfileLink(value);
  return href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-link"
    >
      {children ?? value}
    </a>
  ) : (
    <span>{value}（网址不可用）</span>
  );
}

export function ProfileEvidenceList({
  evidence,
}: {
  evidence: Schema<"AuthorProfileEvidence">[];
}) {
  if (!evidence.length) return <p className="muted">尚未记录来源证据。</p>;
  return (
    <ul className="profile-evidence">
      {evidence.map((item, index) => (
        <li key={index}>
          <div>
            <ProfileLink value={item.url}>{item.title || item.url}</ProfileLink>
            <span className="badge">
              {item.status === "confirmed"
                ? "人工已核对"
                : item.status === "candidate"
                  ? "候选证据"
                  : "待核对"}
            </span>
          </div>
          <p className="muted">
            {item.fields
              .map(
                (field) =>
                  profileFieldLabels[
                    field as keyof typeof profileFieldLabels
                  ] ?? field,
              )
              .join("、")}
            {item.checkedAt
              ? ` · ${new Date(item.checkedAt).toLocaleString()}`
              : ""}
          </p>
          {item.note && <p className="preserve-lines">{item.note}</p>}
        </li>
      ))}
    </ul>
  );
}
