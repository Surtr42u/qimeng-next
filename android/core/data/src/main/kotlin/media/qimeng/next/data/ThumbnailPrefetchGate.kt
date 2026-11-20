package media.qimeng.next.data

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
