package media.qimeng.next.screen

import media.qimeng.next.model.AssetQuery

/**
 * 计数兜底规则（胶囊旁的数字，全仓同一条，勿在各页另立口径）：
 *
 * 1. **已经显示过的数字不得回退成空白**——重新进入页面、切换 Tab、换筛选、刷新、
 *    列表被取消在途请求，都沿用上一次服务端给出的真实计数，直到下一次权威响应覆盖
 *    （旧版 `AlbumViewModel.reloadAll()` 只把 `isLoading` 叠在现有状态上、
 *    `_uiState.value.copy(...)` 保留 `totalMatched`，成功后才替换，即本规则来源）。
 * 2. **从未知道过才允许缺省**——胶囊计数首帧没有数字不算「闪烁」，也不得用占位数字
 *    或 0 冒充；网格骨架属于另一条「从未加载」语义，与胶囊计数互不影响。
 * 3. **禁止伪造 0**——旧版 facet 胶囊在候选未到时打印 `(0)`，这是谎报；新版缺省时
 *    只显示标签，绝不补 0。
 *
 * 本文件只回答一个问题：某个查询是否允许用「导航前就已拿到的计数」兜底。额外的任何
 * 筛选都会改变人群，此时旧数字属于另一组人群，不能拿来充当新查询的结果。
 */

/**
 * 查询是否只包含 [allowAuthor] 指定形态的作者约束，其余筛选维度全为默认值。
 *
 * - [allowAuthor] = false：相册/收藏/历史式查询，必须不带作者约束；
 * - [allowAuthor] = true：作者作品页查询，必须恰好带一个作者约束。
 *
 * 计数只取决于人群范围，与排序/方向/种子/时间窗口无关，故不参与判定。
 */
internal fun AssetQuery.isUnfilteredCountScope(allowAuthor: Boolean): Boolean {
    if (source != "assets" || libraryId != null) return false
    if (text.isNotEmpty() || mediaType != null || kind != null || tag != null) return false
    if (favorite != null || liked != null || history != null || collectionId != null) return false
    if (workKeys.isNotEmpty() || roleKeys.isNotEmpty() || tags.isNotEmpty()) return false
    if (viewRange != "all" || playRange != "all" || sizeRange != "all" || timeRange != "all") return false
    if (fromYear != null || toYear != null) return false
    return if (allowAuthor) authorId != null else authorId == null
}
