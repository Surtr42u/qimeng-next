import { Link, Navigate, useLocation } from "react-router-dom";
import { useExactAuthorBookmark } from "../data/legacy-author";
import { ErrorNotice, Loading } from "../components/Feedback";

const routes: Record<string,string> = {
  "": "/", home:"/", albums:"/album", mine:"/mine", data:"/data", "data/review":"/data/review",
  maintenance:"/maintenance", "maintenance/storage":"/maintenance/storage", "maintenance/client-errors":"/maintenance/client-errors",
  settings:"/settings", search:"/search", authors:"/authors", "maintenance/files":"/manage",
  "maintenance/files/upload":"/manage/upload", "maintenance/files/browse":"/manage/files", "maintenance/files/vocabulary":"/manage/acquisition",
  "maintenance/files/vocabulary-edit":"/manage/vocabulary/rules", "maintenance/files/libraries":"/manage/libraries",
  "maintenance/files/backup":"/manage/backup", "maintenance/trash":"/manage/trash",
};
function decoded(value: string) { try { return decodeURIComponent(value); } catch { return ""; } }
export function LegacyBookmark() {
  const location=useLocation();
  const path=location.pathname.replace(/^\/app(?:\/|$)/,"").replace(/\/$/,"");
  const params=new URLSearchParams(location.search);
  const redirect=(to:string, search=params)=><Navigate replace state={location.state} to={{pathname:to,search:search.toString()}} />;
  if (Object.hasOwn(routes,path)) return redirect(routes[path]!);
  if(path==="maintenance/files/authors-txt"){params.set("tab","authors");return redirect("/manage/backup");}
  if(/^asset\/[^/]+(?:\/edit)?$/.test(path)||/^authors\/[^/]+\/profile$/.test(path))return redirect("/"+path);
  const rank = /^ranks\/([^/]+)$/.exec(path);
  if (rank) { params.set("kind",rank[1]==="authors"||rank[1]==="tags"?rank[1]:"content"); return redirect("/data/rankings"); }
  const collection=/^collection\/(author|tag)\/(.+)$/.exec(path);
  if (path==="collection"||collection) {
    const kind=collection?.[1] ?? (params.get("author") ? "author" : "tag");
    const name=collection?.[2] ? decoded(collection[2]) : params.get(kind) ?? "";
    if(name&&kind==="tag") return redirect("/collection/tag/"+encodeURIComponent(name),new URLSearchParams());
    if(name&&kind==="author"){
      const id=params.get("authorId"),section=params.get("section")==="updates"?"?section=updates":"";
      return id ? <Navigate replace to={"/authors/"+encodeURIComponent(id)+section} /> : <AuthorBookmark key={name} name={name} section={section} />;
    }
  }
  return <Navigate replace to="/" />;
}
function AuthorBookmark({ name, section }: { name:string;section:string }) {
  const query=useExactAuthorBookmark(name);
  if(query.data?.length===1)return <Navigate replace to={"/authors/"+encodeURIComponent(query.data[0]!)+section} />;
  return <div className="page" id="page-collection"><div className="page-head"><h2>作者主页</h2></div>
    {query.isPending ? <Loading /> : query.isError ? <ErrorNotice error={query.error} retry={()=>void query.refetch()} /> : query.data?.length
      ? <p role="alert">存在同名作者，请从<Link to="/authors">作者管理</Link>中选择明确的作者身份。</p> : <p className="grid-empty">该作者下暂无内容。</p>}
  </div>;
}
