package media.qimeng.next.data

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import kotlinx.coroutines.runBlocking
import media.qimeng.next.model.Session
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import org.junit.Assert.assertEquals
import org.junit.Test
import java.util.UUID
import java.util.concurrent.TimeUnit

class AnalyticsQueryTest {
    @Test
    fun generatedStatisticsQueriesUseWireValuesAndPreserveOtherParameters() = runBlocking {
        val ranges = listOf("7d", "30d", "all")
        val paths = setOf("overview", "trends", "top-authors", "top-tags", "top-interests", "review", "most-viewed")
        val cursor = "page 2+/=?"
        MockWebServer().use { server ->
            server.dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    val url = requireNotNull(request.requestUrl)
                    val range = url.queryParameter("range")
                    if (range !in ranges) return MockResponse().setResponseCode(400).setBody("""{"code":"invalid_range"}""")
                    val body = when (url.encodedPath) {
                        "/api/v1/stats/overview" -> overview(range!!)
                        "/api/v1/stats/trends" -> """{"range":"$range","granularity":"day","items":[]}"""
                        "/api/v1/stats/top-interests" -> """{"range":"$range","items":[]}"""
                        "/api/v1/stats/review" -> """{"range":"$range","fromDay":"","toDay":"","previousFromDay":"",
                            "previousToDay":"","viewCount":0,"previousViewCount":0,"firstViews":0,"returnViews":0,"distinctAssets":0,
                            "activeDays":0,"totalSeconds":0,"imageSeconds":0,"animatedSeconds":0,"videoSeconds":0,
                            "comparisonAvailable":false,"hours":[],"changes":[]}"""
                        "/api/v1/stats/top-authors", "/api/v1/stats/top-tags", "/api/v1/stats/most-viewed" ->
                            """{"range":"$range","metric":"views","items":[]}"""
                        else -> return MockResponse().setResponseCode(404)
                    }
                    return MockResponse().setHeader("Content-Type", "application/json").setBody(body)
                }
            }
            server.start()
            val base = InstrumentationRegistry.getInstrumentation().targetContext
            val prefix = "statistics-query-${UUID.randomUUID()}"
            val context = object : ContextWrapper(base) {
                override fun getSharedPreferences(name: String, mode: Int): SharedPreferences =
                    base.getSharedPreferences("$prefix-$name", Context.MODE_PRIVATE)
            }
            val settings = SecureSettings(context)
            settings.saveSession(Session(server.url("/").toString(), "synthetic-instance", "Synthetic"), "synthetic-query-test")
            val analytics = AnalyticsRepository(settings, ApiProvider(settings))
            for (range in ranges) {
                assertEquals(range, analytics.report(range).overview.range)
                assertEquals(range, analytics.review(range).range)
                analytics.ranking("assets", range, "seconds", cursor, limit = 17)
                val requests = List(paths.size) { requireNotNull(server.takeRequest(5, TimeUnit.SECONDS)).requestUrl!! }
                val byPath = requests.associateBy { it.encodedPath.removePrefix("/api/v1/stats/") }
                assertEquals(paths, byPath.keys)
                assertEquals(setOf(range), requests.map { it.queryParameter("range") }.toSet())
                assertEquals("15", byPath.getValue("top-authors").queryParameter("limit"))
                assertEquals("10", byPath.getValue("top-tags").queryParameter("limit"))
                val ranked = byPath.getValue("most-viewed")
                assertEquals(cursor, ranked.queryParameter("cursor"))
                assertEquals("17", ranked.queryParameter("limit"))
                assertEquals("seconds", ranked.queryParameter("metric"))
            }
        }
    }

    private fun overview(range: String): String {
        fun dimensions(vararg keys: String) = keys.joinToString(prefix = "[", postfix = "]") { key ->
            """{"key":"$key","views":0,"plays":0,"seconds":0,"assets":0,"bytes":0}"""
        }
        return """{"range":"$range","inventory":{"assets":0,"images":0,"videos":0,"bytes":0,"favorites":0,"authors":0,"events":0,"trash":0},
            "totals":{"views":0,"plays":0,"seconds":0},"activeDays":0,"viewedAssets":0,"viewedAuthors":0,"viewedTags":0,
            "averageViewsPerFile":null,"mediaTypes":${dimensions("image", "animated_image", "video")},"sources":${dimensions("regular", "cos")}}"""
    }
}
