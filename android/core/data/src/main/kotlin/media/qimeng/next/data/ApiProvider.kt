package media.qimeng.next.data

import com.google.gson.Gson
import com.google.gson.JsonParser
import com.google.gson.annotations.SerializedName
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.transport.PinnedTls
import okhttp3.Call
import okhttp3.Dispatcher
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import retrofit2.Converter
import retrofit2.Response
import retrofit2.Retrofit
import retrofit2.converter.gson.GsonConverterFactory
import retrofit2.http.Query
import java.io.IOException
import java.lang.reflect.Type
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
            code() == 409 && errorCode == "legacy_like_day_unknown" -> "旧记录无法确认今天是否已计入点赞，已保留原累计票数。"
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
    private data class Clients(val api: OkHttpClient, val media: OkHttpClient, val background: OkHttpClient, val lookahead: OkHttpClient)
    @Volatile private var cachedClients: Pair<ClientKey, Clients>? = null

    fun anonymous(baseUrl: String, certificateDerBase64: String? = null): DefaultApi = build(baseUrl, trustedClient(baseUrl, certificateDerBase64))

    /** In-memory credential for one explicitly authenticated owner operation; no selected-session mutation. */
    fun scoped(baseUrl: String, certificate: String?, credential: String): DefaultApi {
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
        return clients(target, certificate).api
    }

    /**
     * 缩略图（可见项与 Coil 网络取图）专用客户端：与 API 共用一个连接池、拦截器与凭据校验，
     * 但**独立 Dispatcher**。OkHttp 的每主机并发上限是 dispatcher 级别的，共用时全库预取的
     * 4 条后台下载会占满 5 个名额，列表分页/首页刷新与可见缩略图都得排在其后（旧版 Coil 用
     * 独立 OkHttpClient，列表请求从不与预取排队）。
     */
    fun mediaClient(target: Session): OkHttpClient {
        val certificate = try { settings.certificateFor(target) } catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录") }
        return clients(target, certificate).media
    }

    /** 后台预取专用客户端：再分一个 Dispatcher，预取不会挤掉正在等待的可见缩略图。 */
    fun backgroundMediaClient(target: Session): OkHttpClient {
        val certificate = try { settings.certificateFor(target) } catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录") }
        return clients(target, certificate).background
    }

    /**
     * 视口前瞻专用客户端：独立 Dispatcher 且只给 2 个并发名额。可见缩略图从不在
     * 前瞻请求后面排队（两条队列互不占位），前瞻也不会像全库预取那样被 4 条后台
     * 请求饿住——它服务的正是马上要滚入的格子。
     */
    fun lookaheadMediaClient(target: Session): OkHttpClient {
        val certificate = try { settings.certificateFor(target) } catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录") }
        return clients(target, certificate).lookahead
    }

    private fun clients(target: Session, certificate: String?): Clients {
        val key = ClientKey(target, certificate)
        cachedClients?.takeIf { it.first == key }?.let { return it.second }
        val expected = target.baseUrl.toHttpUrl()
        val api = trustedClient(target.baseUrl, certificate).newBuilder().addInterceptor { chain ->
            val url = chain.request().url
            if (!sameOrigin(url, expected)) throw IOException("拒绝向其他服务器发送媒体凭据")
            val currentCertificate = try { settings.certificateFor(target) }
            catch (_: Exception) { throw ApiFailure(401, "连接已变更，请重新登录此任务所属服务器") }
            if (currentCertificate != certificate) throw IOException("连接证书已更新，请使用新连接重试")
            val token = try { settings.tokenFor(target) }
            catch (_: Exception) { throw ApiFailure(401, "登录凭据不可用或连接已变更，请重新登录") }
            chain.proceed(chain.request().newBuilder().header("Authorization", "Bearer $token").build())
        // 认证头的读取/校验失败不是传输失败：重试放在凭据拦截器内侧，只包住 proceed。
        }.addInterceptor(transportRetryInterceptor()).build()
        // newBuilder() 保留连接池/超时/拦截器，只换 Dispatcher——分流的只是并发名额，不是连接。
        return Clients(
            api,
            api.newBuilder().dispatcher(Dispatcher()).build(),
            api.newBuilder().dispatcher(Dispatcher()).build(),
            api.newBuilder().dispatcher(Dispatcher().apply { maxRequests = 2; maxRequestsPerHost = 2 }).build(),
        ).also { cachedClients = key to it }
    }

    fun api(target: Session): DefaultApi = build(target.baseUrl, client(target))
    fun apiWithoutRetries(target: Session): DefaultApi = build(target.baseUrl, client(target).newBuilder().retryOnConnectionFailure(false).build())
    private fun build(baseUrl: String, client: OkHttpClient): DefaultApi = Retrofit.Builder()
        .baseUrl(baseUrl.toHttpUrl().resolve("api/v1/")!!).client(client)
        .addConverterFactory(QueryEnumConverterFactory)
        .addConverterFactory(GsonConverterFactory.create(Gson())).build().create(DefaultApi::class.java)

    /**
     * 认证客户端的传输层重试：手机息屏唤醒、Wi-Fi 重关联或服务端刚重启时，第一条
     * TCP/TLS 连接常直接 IOException（服务端大概率没收到请求）。只重试幂等读
     * （GET/HEAD，见 [transportRetryDelayMs]），最多补 2 次（300ms、900ms）。
     *
     * 非 2xx 是「收到了应答」，不在这里重试（本拦截器只捕获 IOException；ApiFailure
     * 在 Retrofit 层由 bodyOrThrow 产生）。SSE 请求由 ServerEventConsumer 自己按
     * 3s→30s 退避重连，这里跳过，避免两层重连叠加。
     *
     * 只挂在 [clients] 构造的认证客户端上：登录走 anonymous 客户端，已有
     * MediaRepository.retryLoginTransport 的 3 次重试，不能再叠一层。
     */
    private fun transportRetryInterceptor(): Interceptor = Interceptor(::proceedWithTransportRetry)

    private fun proceedWithTransportRetry(chain: Interceptor.Chain): okhttp3.Response {
        val request = chain.request()
        var attempt = 0
        while (true) {
            try {
                return chain.proceed(request)
            } catch (failure: IOException) {
                val delayMs = transportRetryDelayMs(
                    method = request.method,
                    accept = request.header("Accept"),
                    cancelled = chain.call().isCanceled() || Thread.currentThread().isInterrupted,
                    attempt = attempt,
                ) ?: throw failure
                attempt++
                // OkHttp 拦截器是同步的：用可中断的分片等待，取消后立即放弃，不白等一整个退避。
                if (!awaitRetryWindow(chain.call(), delayMs)) throw failure
            }
        }
    }
}

/** 重试等待期间检查取消的粒度（毫秒）：足够细，又不会把等待拆成忙等。 */
private const val RETRY_CANCEL_CHECK_MS = 50L

/** 认证客户端幂等读的重试延迟：第 1、2 次传输失败后分别等 300ms、900ms。 */
internal val TRANSPORT_RETRY_DELAYS_MS = longArrayOf(300L, 900L)

/**
 * 认证客户端传输层重试的纯判定（单测锁定；由 [ApiProvider.clients] 的拦截器使用）。
 *
 * - 只重试 GET/HEAD：POST/PUT/PATCH/DELETE 可能已在服务端生效，重试会把「响应丢失」
 *   变成重复写入。
 * - `Accept: text/event-stream` 的 GET 不重试：SSE 消费者自己按 3s→30s 退避重连。
 * - 调用方协程取消会 cancel Call；取消后立即上抛，不继续睡眠/重发。
 * - [attempt] 是本请求已完成的传输失败次数（0 = 第一次失败）；额度用完返回 null。
 */
internal fun transportRetryDelayMs(method: String, accept: String?, cancelled: Boolean, attempt: Int): Long? {
    if (cancelled) return null
    if (method != "GET" && method != "HEAD") return null
    // 任何接受 SSE 的请求都由 ServerEventConsumer 自己重连：这里再试一次会叠成两条重连链。
    val acceptsEventStream = accept?.split(',')?.any { it.trim().equals("text/event-stream", ignoreCase = true) } == true
    if (acceptsEventStream) return null
    return TRANSPORT_RETRY_DELAYS_MS.getOrNull(attempt)
}

/** 分片等待退避窗口；取消/线程中断返回 false（调用方按原失败上抛）。 */
private fun awaitRetryWindow(call: Call, delayMs: Long): Boolean {
    val deadline = System.nanoTime() + delayMs * 1_000_000L
    while (true) {
        if (call.isCanceled() || Thread.currentThread().isInterrupted) return false
        val remainingMs = (deadline - System.nanoTime()) / 1_000_000L
        if (remainingMs <= 0) return true
        try {
            Thread.sleep(minOf(remainingMs, RETRY_CANCEL_CHECK_MS))
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            return false
        }
    }
}

internal object QueryEnumConverterFactory : Converter.Factory() {
    // OpenAPI names such as "7d" are legal query values but cannot be Kotlin enum identifiers,
    // so the wire value comes from @SerializedName.
    //
    // Reflection must not resolve the constant by `value.name`: in a release build R8 renames the
    // enum constant fields (the DEX holds `l`/`m`) while `Enum.name()` keeps the source name
    // ("regular"/"cos"), so `enumType.getField(value.name)` throws NoSuchFieldException while
    // Retrofit is still building the request. That surfaced as "操作未完成，请重试" on every list
    // query that passes an enum parameter, with no request ever leaving the app. Keying off the
    // constant instance and `Field.isEnumConstant` is independent of obfuscation.
    private val wireValues = HashMap<Class<*>, Map<Enum<*>, String>>()

    override fun stringConverter(type: Type, annotations: Array<Annotation>, retrofit: Retrofit): Converter<*, String>? {
        val enumType = type as? Class<*> ?: return null
        if (!enumType.isEnum || annotations.none { it is Query }) return null
        val values = wireValuesOf(enumType)
        return Converter<Enum<*>, String> { value -> values[value] ?: value.name }
    }

    private fun wireValuesOf(enumType: Class<*>): Map<Enum<*>, String> = synchronized(wireValues) {
        wireValues.getOrPut(enumType) {
            val values = LinkedHashMap<Enum<*>, String>()
            for (field in enumType.declaredFields) {
                if (!field.isEnumConstant) continue
                // A private/package-private enum class makes its public constant field
                // inaccessible from here; opening it is enough and is allowed on Android.
                val constant = try {
                    field.isAccessible = true
                    field.get(null) as? Enum<*>
                } catch (_: Exception) {
                    null
                } ?: continue
                values[constant] = field.getAnnotation(SerializedName::class.java)?.value ?: constant.name
            }
            values
        }
    }
}

internal fun sameOrigin(left: HttpUrl, right: HttpUrl) = left.scheme == right.scheme && left.host == right.host && left.port == right.port
