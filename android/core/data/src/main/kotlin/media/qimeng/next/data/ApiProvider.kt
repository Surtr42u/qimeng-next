package media.qimeng.next.data

import com.google.gson.Gson
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.api.DefaultApi
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import retrofit2.Response
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import java.io.IOException
import java.util.concurrent.TimeUnit

class ApiFailure(val status: Int, override val message: String) : IOException(message)

internal fun <T> Response<T>.bodyOrThrow(): T {
    if (!isSuccessful) {
        // Do not surface URLs, response bodies, request headers, or credentials from a proxy.
        val explanation = when (code()) {
            401 -> "登录已失效，请重新登录"
            403 -> "当前连接没有执行此操作的权限"
            404 -> "对象不存在或已被移除"
            409 -> "服务器状态已变化或存在重复，请刷新核对后再决定"
            413 -> "服务器拒绝了本次传输大小"
            429 -> "服务器正忙，请稍后重试"
            in 500..599 -> "服务器暂时无法完成请求"
            else -> "请求失败（HTTP ${code()}）"
        }
        errorBody()?.close()
        throw ApiFailure(code(), explanation)
    }
    return body() ?: throw IOException("服务器返回了缺失的数据")
}

fun normalizedServer(input: String): String {
    val url = input.trim().toHttpUrl()
    require(url.username.isEmpty() && url.password.isEmpty() && url.query == null && url.fragment == null) { "地址不能包含凭据、查询或片段" }
    require(url.encodedPath == "/") { "请输入服务器根地址" }
    require(url.isHttps || url.host in setOf("localhost", "127.0.0.1")) { "请使用 HTTPS 或本机隧道地址" }
    return url.toString()
}

internal class ApiProvider(private val settings: SecureSettings) {
    private val baseClient = OkHttpClient.Builder().followRedirects(false).followSslRedirects(false)
        .connectTimeout(15, TimeUnit.SECONDS).readTimeout(60, TimeUnit.SECONDS).writeTimeout(60, TimeUnit.SECONDS).build()
    @Volatile private var cachedClient: Pair<Session, OkHttpClient>? = null

    fun anonymous(baseUrl: String): DefaultApi = build(baseUrl, baseClient)

    fun client(target: Session): OkHttpClient {
        cachedClient?.takeIf { it.first == target }?.let { return it.second }
        val expected = target.baseUrl.toHttpUrl()
        return baseClient.newBuilder().addInterceptor { chain ->
            val url = chain.request().url
            if (!sameOrigin(url, expected)) throw IOException("拒绝向其他服务器发送媒体凭据")
            val token = settings.tokenFor(target)
            chain.proceed(chain.request().newBuilder().header("Authorization", "Bearer $token").build())
        }.build().also { cachedClient = target to it }
    }

    fun api(target: Session): DefaultApi = build(target.baseUrl, client(target))
    private fun build(baseUrl: String, client: OkHttpClient): DefaultApi = Retrofit.Builder()
        .baseUrl(baseUrl.toHttpUrl().resolve("api/v1/")!!).client(client)
        .addConverterFactory(GsonConverterFactory.create(Gson())).build().create(DefaultApi::class.java)
}

internal fun sameOrigin(left: HttpUrl, right: HttpUrl) = left.scheme == right.scheme && left.host == right.host && left.port == right.port
