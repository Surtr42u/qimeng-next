package media.qimeng.next.model

data class RecommendationWeights(val tagRelevance: Double, val tagCollection: Double, val engagement: Double,
    val recency: Double, val likeScore: Double, val discovery: Double, val freshness: Double, val browseDepth: Double, val maxRandom: Double)
data class RecommendationPreferences(val revision: Long, val weights: RecommendationWeights, val presets: Map<String, RecommendationWeights>)
data class ActivityTotals(val views: Long, val plays: Long, val seconds: Long)
data class ActivityDimension(val key: String, val views: Long, val plays: Long, val seconds: Long, val assets: Long, val bytes: Long)
data class ActivityOverview(val range: String, val inventory: InventoryStats, val totals: ActivityTotals, val activeDays: Int,
    val viewedAssets: Int, val viewedAuthors: Int, val viewedTags: Int, val averageViews: Double?,
    val mediaTypes: List<ActivityDimension>, val sources: List<ActivityDimension>)
data class TrendDimension(val key: String, val views: Long, val plays: Long, val seconds: Long)
data class TrendPoint(val label: String, val views: Long, val plays: Long, val seconds: Long,
    val mediaTypes: List<TrendDimension>, val sources: List<TrendDimension>)
data class ActivityRank(val id: String, val name: String, val views: Long, val plays: Long, val seconds: Long, val assets: Long, val value: Long, val assetId: String?)
data class ReviewHour(val hour: Int, val views: Long, val seconds: Long, val image: Long, val animated: Long, val video: Long)
data class InterestChange(val kind: String, val id: String, val name: String, val currentShare: Double, val previousShare: Double,
    val changePoints: Double, val currentViews: Long, val previousViews: Long)
data class BrowsingReview(val range: String, val totalSeconds: Long, val firstViews: Long, val returnViews: Long, val distinctAssets: Int, val activeDays: Int,
    val comparisonAvailable: Boolean, val hours: List<ReviewHour>, val changes: List<InterestChange>,
    val fromDay: String, val toDay: String, val previousFromDay: String, val previousToDay: String,
    val viewCount: Long, val previousViewCount: Long, val imageSeconds: Long, val animatedSeconds: Long, val videoSeconds: Long)
data class TopInterest(val kind: String, val id: String, val name: String, val views: Long)
data class AnalyticsReport(val overview: ActivityOverview, val trends: List<TrendPoint>, val top: Map<String, List<ActivityRank>>,
    val interests: List<TopInterest>)
