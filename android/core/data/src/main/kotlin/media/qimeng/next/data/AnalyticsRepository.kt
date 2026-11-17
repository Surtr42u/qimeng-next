package media.qimeng.next.data

import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import media.qimeng.next.model.*
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.RecommendationPreferencesUpdate
import java.util.UUID
import media.qimeng.next.sdk.model.RecommendationWeights as ApiWeights
import media.qimeng.next.sdk.model.RecommendationPreferences as ApiPreferences

class AnalyticsRepository internal constructor(private val settings: SecureSettings, private val provider: ApiProvider) {
    private fun api() = provider.api(settings.session.value ?: error("请先登录服务器"))
    suspend fun report(range: String): AnalyticsReport = coroutineScope {
        val api = api()
        val overview = async { api.getStatsOverview(range = DefaultApi.RangeGetStatsOverview.entries.first { it.value == range }).bodyOrThrow() }
        val trends = async { api.getStatsTrends(range = DefaultApi.RangeGetStatsTrends.entries.first { it.value == range }).bodyOrThrow() }
        val authors = async { ranking("authors", range, "views", null, 15) }
        val tags = async { ranking("tags", range, "views", null, 10) }
        val interests = async { api.getTopInterests(range = DefaultApi.RangeGetTopInterests.entries.first { it.value == range }).bodyOrThrow() }
        val o = overview.await()
        require(o.mediaTypes.map { it.key }.containsAll(listOf("image", "animated_image", "video")) &&
            o.sources.map { it.key }.containsAll(listOf("regular", "cos"))) { "服务器未提供完整统计分类，请同步更新服务端" }
        AnalyticsReport(ActivityOverview(o.range, o.inventory.let { InventoryStats(it.assets, it.images, it.videos, it.bytes, it.favorites, it.authors, it.events, it.trash) },
            ActivityTotals(o.totals.views, o.totals.plays, o.totals.seconds), o.activeDays, o.viewedAssets, o.viewedAuthors, o.viewedTags,
            o.averageViewsPerFile, o.mediaTypes.map { ActivityDimension(it.key, it.views, it.plays, it.seconds, it.assets, it.bytes) },
            o.sources.map { ActivityDimension(it.key, it.views, it.plays, it.seconds, it.assets, it.bytes) }),
            trends.await().items.map {
                require(it.mediaTypes.map { d -> d.key }.containsAll(listOf("image", "animated_image", "video")) &&
                    it.sources.map { d -> d.key }.containsAll(listOf("regular", "cos"))) { "服务器未提供完整趋势分类，请同步更新服务端" }
                TrendPoint(it.label, it.views, it.plays, it.seconds,
                it.mediaTypes.map { d -> TrendDimension(d.key, d.views, d.plays, d.seconds) },
                it.sources.map { d -> TrendDimension(d.key, d.views, d.plays, d.seconds) }) },
            mapOf("authors" to authors.await().items, "tags" to tags.await().items),
            interests.await().items.map { TopInterest(it.kind.value, it.id, it.name, it.views) })
    }
    suspend fun ranking(kind: String, range: String, metric: String = "views", cursor: String? = null, limit: Int = 50): Page<ActivityRank> {
        val api = api()
        val page = when (kind) {
            "authors" -> api.getTopAuthors(range = DefaultApi.RangeGetTopAuthors.entries.first { it.value == range }, cursor = cursor, limit = limit).bodyOrThrow()
            "tags" -> api.getTopTags(range = DefaultApi.RangeGetTopTags.entries.first { it.value == range }, cursor = cursor, limit = limit).bodyOrThrow()
            "assets" -> api.getMostViewedAssets(range = DefaultApi.RangeGetMostViewedAssets.entries.first { it.value == range }, cursor = cursor, limit = limit,
                metric = DefaultApi.MetricGetMostViewedAssets.entries.first { it.value == metric }).bodyOrThrow()
            else -> error("未知统计分类")
        }
        return Page(page.items.map { ActivityRank(it.id, it.name, it.views, it.plays, it.seconds, it.assets, it.value, it.asset?.id) }, page.nextCursor)
    }
    suspend fun review(range: String): BrowsingReview {
        val value = api().getBrowsingReview(range = DefaultApi.RangeGetBrowsingReview.entries.first { it.value == range }).bodyOrThrow()
        return BrowsingReview(value.range, value.totalSeconds, value.firstViews, value.returnViews, value.distinctAssets, value.activeDays, value.comparisonAvailable,
            value.hours.map { ReviewHour(it.hour, it.views, it.seconds, it.imageSeconds, it.animatedSeconds, it.videoSeconds) },
            value.changes.map { InterestChange(it.kind.value, it.id, it.name, it.currentShare, it.previousShare, it.changePoints, it.currentViews, it.previousViews) },
            value.fromDay, value.toDay, value.previousFromDay, value.previousToDay, value.viewCount, value.previousViewCount,
            value.imageSeconds, value.animatedSeconds, value.videoSeconds)
    }
    suspend fun preferences() = api().getRecommendationPreferences().bodyOrThrow().toModel()
    suspend fun savePreferences(current: RecommendationPreferences, selected: RecommendationWeights): RecommendationPreferences {
        if (current.weights == selected) return current
        return api().updateRecommendationPreferences(idempotencyKey = UUID.randomUUID().toString(),
            recommendationPreferencesUpdate = RecommendationPreferencesUpdate(expectedRevision = current.revision, weights = selected.toApi())).bodyOrThrow().toModel()
    }
}

private fun ApiPreferences.toModel() = RecommendationPreferences(revision, weights.toModel(), presets.mapValues { it.value.toModel() })
private fun ApiWeights.toModel() = RecommendationWeights(tagRelevance, tagCollection, engagement, recency, likeScore, discovery, freshness, browseDepth, maxRandom)
private fun RecommendationWeights.toApi() = ApiWeights(tagRelevance, tagCollection, engagement, recency, likeScore, discovery, freshness, browseDepth, maxRandom)
