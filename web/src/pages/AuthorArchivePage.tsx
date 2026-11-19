import { Link, useParams } from "react-router-dom";
import { ErrorNotice, Loading, PageHeading } from "../components/Feedback";
import { useQueryView } from "../lib/useQueryView";

export function AuthorArchivesPage() {
  const { id = "", archiveId = "" } = useParams();
  const records = useQueryView("author-archives", () => import("./AuthorArchiveRecords"));
  return (
    <>
      <PageHeading
        title="作者资料归档"
        subtitle="删除、合并与迁入时保留的资料与来源证据。"
      />
      <div className="actions">
        <Link className="button" to="/authors">
          作者列表
        </Link>
        <Link className="button" to={`/authors/${encodeURIComponent(id)}`}>
          查看当前作者
        </Link>
      </div>
      {records.data ? (
        <records.data.AuthorArchiveRecords id={id} archiveId={archiveId} />
      ) : records.isError ? (
        <ErrorNotice error={records.error} retry={() => void records.refetch()} />
      ) : (
        <>
          <Loading />
          {archiveId && <section className="section-space form-stack" aria-label="归档详情"><Loading /></section>}
        </>
      )}
    </>
  );
}
