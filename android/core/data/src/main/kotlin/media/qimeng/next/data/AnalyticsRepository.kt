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
        val o = overview.await()
        AnalyticsReport(ActivityOverview(o.range, o.inventory.let { InventoryStats(it.assets, it.images, it.videos, it.bytes, it.favorites, it.authors, it.events, it.trash) },
            ActivityTotals(o.totals.views, o.totals.plays, o.totals.seconds), o.activeDays, o.viewedAssets, o.viewedAuthors, o.viewedTags,
            o.averageViewsPerFile, o.mediaTypes.map { ActivityDimension(it.key, it.views, it.plays, it.seconds, it.assets, it.bytes) },
            o.sources.map { ActivityDimension(it.key, it.views, it.plays, it.seconds, it.assets, it.bytes) }),
            trends.await().items.map { TrendPoint(it.label, it.views, it.plays, it.seconds) })
    }
    suspend fun review(range: String): BrowsingReview {
        val value = api().getBrowsingReview(range = DefaultApi.RangeGetBrowsingReview.entries.first { it.value == range }).bodyOrThrow()
        return BrowsingReview(value.range, value.firstViews, value.returnViews, value.distinctAssets, value.activeDays, value.comparisonAvailable,
            value.hours.map { ReviewHour(it.hour, it.views, it.seconds, it.imageSeconds, it.animatedSeconds, it.videoSeconds) },
            value.changes.map { InterestChange(it.kind.value, it.id, it.name, it.currentShare, it.previousShare, it.changePoints, it.currentViews, it.previousViews) })
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
