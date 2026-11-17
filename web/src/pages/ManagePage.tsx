import { Link } from "react-router-dom";
import { Icon } from "../components/Icon";
import type { IconName } from "../components/Icon";
import { PageHeading } from "../components/Feedback";

const groups: {
  title: string;
  items: { title: string; description: string; href: string; icon: IconName }[];
}[] = [
  {
    title: "入库与整理",
    items: [
      {
        title: "上传文件",
        description: "普通上传、智能批次与队列恢复",
        href: "/manage/upload",
        icon: "upload",
      },
      {
        title: "媒体库",
        description: "管理常规与 COS 媒体库",
        href: "/manage/libraries",
        icon: "server",
      },
      {
        title: "组图与 AI 短剧",
        description: "作品集合、页序与连续编号",
        href: "/manage/collections",
        icon: "album",
      },
      {
        title: "任务与报告",
        description: "逐项查看入库、等待、异常与后续处理",
        href: "/manage/tasks",
        icon: "task",
      },
    ],
  },
  {
    title: "资料与关联",
    items: [
      {
        title: "作者资料",
        description: "名称、别名、简介、主页与关注",
        href: "/authors",
        icon: "mine",
      },
      {
        title: "词表与关联",
        description: "标签、作品出处、角色与别名",
        href: "/manage/vocabulary",
        icon: "manage",
      },
      {
        title: "整理已有媒体",
        description: "打开媒体详情，维护标签、来源与位置",
        href: "/album",
        icon: "manage",
      },
    ],
  },
  {
    title: "备份与存储",
    items: [
      {
        title: "资料备份与归档",
        description: "一致快照、内容清单与独立备份状态",
        href: "/manage/backup",
        icon: "server",
      },
      {
        title: "回收站",
        description: "恢复媒体身份与关联，独立管理永久删除",
        href: "/manage/trash",
        icon: "trash",
      },
      {
        title: "服务与维护",
        description: "连接信息、运行能力与任务入口",
        href: "/maintenance",
        icon: "settings",
      },
      {
        title: "设置",
        description: "外观、连接与使用偏好",
        href: "/settings",
        icon: "sun",
      },
    ],
  },
];
export function ManagePage() {
  return (
    <>
      <PageHeading
        title="数据管理"
        subtitle="整理喜欢的内容，也照顾它们的来处与去向。"
      />
      {groups.map((group) => (
        <section key={group.title} className="manage-group">
          <h2>{group.title}</h2>
          <div className="manage-grid">
            {group.items.map((item) => (
              <Link
                key={item.href}
                to={item.href}
                className="manage-card panel"
              >
                <span className="row-icon">
                  <Icon name={item.icon} size={25} />
                </span>
                <div>
                  <h3>{item.title}</h3>
                  <p>{item.description}</p>
                </div>
              </Link>
            ))}
          </div>
        </section>
      ))}
    </>
  );
}
