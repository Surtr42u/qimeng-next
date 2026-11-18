import type { ReactNode } from "react";
import { ArrowLeft } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";

export function ManagePageHeader({
  title,
  description,
  backTo,
  backLabel,
  actions,
}: {
  title: string;
  description: string;
  backTo?: string;
  backLabel?: string;
  actions?: ReactNode;
}) {
  const [params] = useSearchParams();
  const fromMine = params.get("from") === "mine";
  const fromService = params.get("from") === "service";
  return (
    <header className="manage-page-head">
      <Link
        className="manage-back"
        to={backTo ?? (fromMine ? "/mine" : fromService ? "/maintenance" : "/manage")}
      >
        <ArrowLeft aria-hidden="true" />
        {backLabel ?? (fromMine ? "返回我的" : fromService ? "返回服务与维护" : "返回数据管理")}
      </Link>
      <div className="manage-heading">
        <div>
          <h2 data-page-heading tabIndex={-1}>
            {title}
          </h2>
          <p>{description}</p>
        </div>
        {actions && <div className="manage-actions">{actions}</div>}
      </div>
    </header>
  );
}
