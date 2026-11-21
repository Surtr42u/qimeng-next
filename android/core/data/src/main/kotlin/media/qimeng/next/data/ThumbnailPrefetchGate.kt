package media.qimeng.next.data

import java.util.concurrent.atomic.AtomicLong

internal enum class ThumbnailPrefetchDecision {

    /** Same server, same revision, same preview policy: skip listing and downloading entirely. */
    SKIP,

    /** First run, changed library, policy change, unknown server, or an unusable revision response. */
    FULL,
}

/**
 * Decides whether a prefetch round may be skipped.
 *
 * Every unknown is FULL: the expensive failure mode is an incorrect skip, while an extra full
 * round only re-reads disk (the per-entry warm-state check already short-circuits downloads).
 */
internal object ThumbnailPrefetchGate {

    fun decide(
        serverRevision: Long?,
        record: ThumbnailPrefetchRecord?,
        serverKey: String,
        thumbnailVersion: String,
    ): ThumbnailPrefetchDecision = when {
        serverRevision == null -> ThumbnailPrefetchDecision.FULL
        record == null -> ThumbnailPrefetchDecision.FULL
        record.serverKey != serverKey -> ThumbnailPrefetchDecision.FULL
        record.revision == serverRevision && record.thumbnailVersion == thumbnailVersion -> ThumbnailPrefetchDecision.SKIP
        else -> ThumbnailPrefetchDecision.FULL
    }
}

/**
 * 磁盘池写满后，一轮预取还能给多少「新键」让 Coil 的 LRU 淘汰最旧条目。
 *
 * 池到配额时 [ThumbnailDiskCache.store] 默认拒写（FULL），这是防止「库比配额大」时
 * 每轮重下被淘汰尾部的容量门。但两种情形必须放行：
 *
 * - **接管**（池里至少一半字节属于别的实例）：磁盘键按实例身份编址，那些条目本连接
 *   永远读不到，却正是「新键一律 FULL」的原因——换服务器后新库一张缩略图也预取不进。
 *   旧版 `SplitDiskCache` 不做容量门，新实例的全库预取经 Coil LRU 直接挤掉旧池；这里
 *   只在当前实例确实是池里的少数时放行，得到同一结果。
 * - **刷新**（当前实例已拥有池）：只有新增媒体需要位置。给小额定预算，容纳一次正常
 *   入库批次，又不会让超大库每轮重下自己的尾巴。
 *
 * 可见/视口请求不走本策略：它们由用户驱动、数量有界，直接允许淘汰（见 prefetchOne）。
 */
internal object ThumbnailTurnoverPolicy {

    /** 别的实例占池达到此百分比时按「接管」处理。 */
    const val FOREIGN_SHARE_PCT = 50

    /** 接管之外，每轮允许的淘汰写入新键数（≈512 × 30 KiB md JPEG ≈ 15 MiB）。 */
    const val REFRESH_ENTRIES = 512L

    /** 本轮淘汰写入的上限；[Long.MAX_VALUE] 表示接管，不设限。 */
    fun limit(ownBytes: Long, poolBytes: Long): Long {
        if (poolBytes <= 0) return REFRESH_ENTRIES
        val foreign = (poolBytes - ownBytes).coerceAtLeast(0)
        return if (foreign * 100 >= poolBytes * FOREIGN_SHARE_PCT) Long.MAX_VALUE else REFRESH_ENTRIES
    }
}

/** 一轮预取的淘汰写入额度，轮首固定；只有「池已满且该键不存在」时才消耗。 */
internal class ThumbnailTurnoverBudget(private val limit: Long) {

    private val used = AtomicLong()

    fun tryAcquire(): Boolean = used.incrementAndGet() <= limit
}
