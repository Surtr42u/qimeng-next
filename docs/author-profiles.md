# 作者公开资料与离线头像

作者核心身份继续管理名称、别名、关注和媒体关联。公开资料有 X、整理简介、账号三个独立修订块，首次为空时修订为 0。写入必须携带当前块修订与稳定幂等键；省略字段保留现值，空字符串或空数组明确清空。X 账号、原文或来源改变后，客户端不能把原译文或原头像继续当作新来源的资料。

X 原文与中文译文分开保存；改变原文或账号来源时，未明确核对的旧译文清除。公开昵称独立于核心身份名，特殊情况备注用于退圈、删号或存档身份线索；私人备注是影库内的整理信息，不会被当作公开简介。具有全库读取权限的已授权 AI 同样能读取备注，设置页需要如实说明该范围。

资料候选保存在独立队列中，含所依据的块版本、来源证据与可选作者维护任务身份。AI 只能在有效任务授权的作者、块和次数内提交候选；提交和额度消费在同一事务。候选不修改已确认块，Web 必须先填草稿、对照并明确保存。用户可编辑候选后保存，保留 candidateId 与实际确认版本；过期候选不会自动覆盖当前资料。每位作者最多 50 条未处理候选，历史记录游标分页。

头像获取只由管理员明确发起。服务端只接受 HTTPS 443、无 userinfo 的地址，不使用环境代理；DNS 的所有结果必须属于允许的公网地址，再把验证后的数字 IP 交给拨号器，TLS 仍检查原始主机。每次重定向重新验证地址，最多三次；拦截 IPv4 映射、CGNAT、保留/文档网段、内部和 IPv6 过渡地址。请求总超时 10 秒，最多两份同时解码，原图 4 MiB、400 万像素且单边不超过 4096。PNG/JPEG/GIF/WebP 解码后只保存 512 像素以内的静态 PNG；GIF 使用首帧，图片元数据不透传。来源与规范化图片各保存 SHA-256，数据库中的图片与资料绑定同事务提交。响应中不暴露包含源网址的网络错误。

读取头像需要同源认证，URL 由内容摘要标识，并采用 private/no-cache。客户端不把外域来源网址传给 img。改变或清除来源时解除旧头像关联；没有任何活动或归档引用的图片才清理。删除/合并作者时，同事务保留完整来源资料、候选和离线头像。合并目标中已有的块——包括人工清空的块——保持原样，只有缺失块继承，冲突计入操作报告；归档通过删除后的作者 ID 或合并目标仍可查看。

v2 迁入只读取旧协议定义，未复制旧实现。PrepareImportedAuthorProfile 在业务写入前校验作者身份、256 KiB JSON、已知字段预算与最多 2 MiB 的离线头像；图片必须有同条资料的来源绑定，不发任何外网请求。RestoreImportedAuthorProfile 把 nickname/biography/remark 映射为公开昵称/整理简介/特殊情况备注，把 xProfile 原文、译文、账号与 homepageUrl/feedUrl 映射为对应块和人工发布入口。已核实资料只恢复缺失块；已有块的区别保为候选或完整原始归档，缺少证据不会假造来源。人工空字符串不被补缺复活；同一 X 账号与相同原文才可补尚未出现的译文字段，同来源且缺图才可补离线图。autoRefresh/trackUpdates 不迁为执行任务。未知字段以原始 JSON 保档，整数不经浮点重编码。

实现位于 service/author_profiles*.go、httpapi/author_profiles*.go 与追加迁移 0011。测试覆盖块版本、显式清空、译文失效、候选权限/回执、私网与 DNS 重绑定、图片预算、来源变化、合并/删除及迁入保护；Web 另有草稿重基、候选不提前保存和真实 Go 上传头像闭环。所有格式化、编译与测试在云端执行，是否通过以对应 SHA 的 CI 记录为准。

独立实现参考：旧项目 api/openapi.yaml 的 CreatorProfile/CreatorXProfile/LegacyAuthor 结构、docs/domain/authors.md 的公开档案与 X 简介章节、docs/domain/migration.md 的作者公开档案章节。技术依据为 [Go HTTP Transport](https://pkg.go.dev/net/http#Transport)、[netip 地址分类](https://pkg.go.dev/net/netip#Addr.IsPrivate)、[image.DecodeConfig](https://pkg.go.dev/image#DecodeConfig)、[官方 WebP decoder](https://pkg.go.dev/golang.org/x/image/webp)。新增依赖锁为 golang.org/x/image v0.48.0，来自 [Go 模块代理官方记录](https://proxy.golang.org/golang.org/x/image/@v/v0.48.0.info)，go.sum 由云端生成。
