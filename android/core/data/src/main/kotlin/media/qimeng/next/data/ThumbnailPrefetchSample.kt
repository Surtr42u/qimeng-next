package media.qimeng.next.data

import kotlin.random.Random

/** A disk-cache key plus the content version it was stored under (both needed by warmState). */
internal data class ThumbnailPrefetchSample(val key: String, val version: Long) {

    fun encode(): String = "$version|$key"

    companion object {
        fun decode(value: String): ThumbnailPrefetchSample? {
            val separator = value.indexOf('|')
            if (separator <= 0 || separator == value.lastIndex) return null
            val version = value.substring(0, separator).toLongOrNull()?.takeIf { it >= 1 } ?: return null
            return ThumbnailPrefetchSample(value.substring(separator + 1), version)
        }
    }
}

/**
 * Pure sampling policy for revision-based prefetch skipping.
 *
 * Revision comparison alone is blind to LRU eviction, cleared app caches, and restored backups
 * that roll both sides back together. A full round therefore records a random sample of the
 * cache keys it just verified; a later skip probes those keys locally and falls back to a full
 * round when enough are missing.
 */
internal object ThumbnailPrefetchSampleGate {

    /** Number of cache keys recorded at the end of a full round. */
    const val SAMPLE_SIZE = 64

    /** Missing percent that invalidates the revision skip; a few LRU evictions are tolerated. */
    const val MISSING_THRESHOLD_PCT = 20

    fun pickSample(
        samples: List<ThumbnailPrefetchSample>,
        random: Random = Random.Default,
    ): Set<ThumbnailPrefetchSample> =
        if (samples.size <= SAMPLE_SIZE) samples.toSet()
        else samples.shuffled(random).take(SAMPLE_SIZE).toSet()

    fun shouldDowngradeToFull(sampled: Int, missing: Int): Boolean =
        sampled > 0 && missing * 100 >= sampled * MISSING_THRESHOLD_PCT
}
