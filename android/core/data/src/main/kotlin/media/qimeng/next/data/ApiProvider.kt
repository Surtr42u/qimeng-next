package media.qimeng.next.data

import com.google.gson.Gson
import com.google.gson.JsonParser
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.transport.PinnedTls
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import retrofit2.Response
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import java.io.IOException
import java.util.concurrent.TimeUnit

class ApiFailure(val status: Int, override val message: String, val code: String? = null) : IOException(message)

internal fun <T> Response<T>.bodyOrThrow(): T {
    if (!isSuccessful) {
        // Do not surface URLs, response bodies, request headers, or credentials from a proxy.
        val errorCode = errorBody()?.use { body ->
            try {
                val chars = CharArray(4096)
                val count = body.charStream().use { it.read(chars) }
                if (count <= 0) null else JsonParser.parseString(String(chars, 0, count)).asJsonObject.get("code")?.asString
            } catch (_: Exception) { null }
        }
        val explanation = when {
            code() == 409 && errorCode == "recommendation_expired" -> "推荐列表已更新，请刷新后继续浏览。"
            code() == 409 && errorCode == "tag_deleted" -> "标签已被删除。请刷新并移除草稿中的旧标签；确需恢复时，使用“添加”明确新建。"
            code() == 409 && errorCode == "source_hash_mismatch" -> "传输字节与入队摘要不一致，未入库。请保留原件并重新选择文件。"
            code() == 409 && errorCode == "publication_pending" -> "服务器正在完成提交，当前不能取消；请保留来源并等待结果核对。"
            errorCode == "library_root_denied" -> "目录必须位于服务端已授权的媒体根内，请核对路径与文件访问权限。"
            errorCode == "library_has_trash" -> "库中仍有可恢复的回收站项目，请先恢复或在回收站处理。"
            errorCode == "library_scan_active" -> "扫描正在执行，请等待扫描结束。"
            errorCode == "library_upload_active" || errorCode == "library_batch_active" -> "此库有未结束的上传或接收批次，请先完成或取消。"
            errorCode == "library_publication_pending" -> "有文件正在发布，请先完成或恢复原操作。"
            errorCode == "library_protection_active" || errorCode == "library_backup_active" -> "正在备份此库，请等待备份结束。"
            errorCode == "library_cleanup_active" -> "已有来源回收许可，请等待回执或许可过期。"
            else -> when (code()) {
            401 -> "登录已失效，请重新登录"
            403 -> "当前连接没有执行此操作的权限"
            404 -> "对象不存在或已被移除"
            409 -> "服务器状态已变化或存在重复，请刷新核对后再决定"
            413 -> "服务器拒绝了本次传输大小"
            415 -> "服务器判定文件格式不受支持或声明类型不匹配，来源文件已保留"
            429 -> "服务器正忙，请稍后重试"
            in 500..599 -> "服务器暂时无法完成请求"
            else -> "请求失败（HTTP ${code()}）"
            }
        }
        throw ApiFailure(code(), explanation, errorCode)
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
    private data class ClientKey(val target: Session, val certificate: String?)
    @Volatile private var cachedClient: Pair<ClientKey, OkHttpClient>? = null

    fun anonymous(baseUrl: String, certificateDerBase64: String? = null): DefaultApi = build(baseUrl, trustedClient(baseUrl, certificateDerBase64))

    /** In-memory credential for one explicitly authenticated owner operation; no selected-session mutation. */
    fun scoped(baseUrl: String, certificate: String, credential: String): DefaultApi {
        val expected = baseUrl.toHttpUrl()
        val client = trustedClient(baseUrl, certificate).newBuilder().addInterceptor { chain ->
            if (!sameOrigin(chain.request().url, expected)) throw IOException("拒绝向其他服务器发送凭据")
            chain.proceed(chain.request().newBuilder().header("Authorization", "Bearer $credential").build())
        }.build()
        return build(baseUrl, client)
    }

    private fun trustedClient(baseUrl: String, certificate: String?): OkHttpClient {
        if (certificate == null) return baseClient
        require(baseUrl.toHttpUrl().isHttps) { "指定服务器证书需要 HTTPS" }
        val tls = PinnedTls(certificate)
        return baseClient.newBuilder().sslSocketFactory(tls.socketFactory, tls.trustManager).build()
    }

    fun client(target: Session): OkHttpClient {
        val certificate = try { settings.certificateFor(target) } catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录") }
        val key = ClientKey(target, certificate)
        cachedClient?.takeIf { it.first == key }?.let { return it.second }
        val expected = target.baseUrl.toHttpUrl()
        return trustedClient(target.baseUrl, certificate).newBuilder().addInterceptor { chain ->
            val url = chain.request().url
            if (!sameOrigin(url, expected)) throw IOException("拒绝向其他服务器发送媒体凭据")
            val currentCertificate = try { settings.certificateFor(target) }
            catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录此任务所属服务器") }
            if (currentCertificate != certificate) throw IOException("连接证书已更新，请使用新连接重试")
            val token = try { settings.tokenFor(target) }
            catch (_: Exception) { throw ApiFailure(401, "登录凭据不可用或连接已变更，请重新登录") }
            chain.proceed(chain.request().newBuilder().header("Authorization", "Bearer $token").build())
        }.build().also { cachedClient = key to it }
    }

    fun api(target: Session): DefaultApi = build(target.baseUrl, client(target))
    fun apiWithoutRetries(target: Session): DefaultApi = build(target.baseUrl, client(target).newBuilder().retryOnConnectionFailure(false).build())
    private fun build(baseUrl: String, client: OkHttpClient): DefaultApi = Retrofit.Builder()
        .baseUrl(baseUrl.toHttpUrl().resolve("api/v1/")!!).client(client)
        .addConverterFactory(GsonConverterFactory.create(Gson())).build().create(DefaultApi::class.java)
}

internal fun sameOrigin(left: HttpUrl, right: HttpUrl) = left.scheme == right.scheme && left.host == right.host && left.port == right.port
