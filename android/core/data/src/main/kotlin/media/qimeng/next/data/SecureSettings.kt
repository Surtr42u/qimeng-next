package media.qimeng.next.data

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import com.google.gson.Gson
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import media.qimeng.next.model.Preferences
import media.qimeng.next.model.TabBarMaterial
import media.qimeng.next.model.Session
import media.qimeng.next.model.sameTarget
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Tokens are Keystore-encrypted and excluded from Android backup. Queue records contain no token. */
class SecureSettings(context: Context) {
    private val storage = context.getSharedPreferences("connection", Context.MODE_PRIVATE)
    private val gson = Gson()
    private val mutableSession = MutableStateFlow<Session?>(null)
    private val mutablePreferences = MutableStateFlow(Preferences())
    private val mutableError = MutableStateFlow<String?>(null)
    private val mutableRecentServers = MutableStateFlow<List<String>>(emptyList())
    private val mutableTransportRevision = MutableStateFlow(0L)
    private val mutableSearchHistory = MutableStateFlow<List<String>>(emptyList())
    private val mutableAlbumColumns = MutableStateFlow(DEFAULT_ALBUM_COLUMNS)
    private val mutableHomeColumns = MutableStateFlow(DEFAULT_HOME_COLUMNS)
    private val mutableDefaultEndpoint = MutableStateFlow<String?>(null)
    val session = mutableSession.asStateFlow()
    val preferences = mutablePreferences.asStateFlow()
    val error = mutableError.asStateFlow()
    val recentServers = mutableRecentServers.asStateFlow()
    val transportRevision = mutableTransportRevision.asStateFlow()
    val searchHistory = mutableSearchHistory.asStateFlow()
    val albumColumns = mutableAlbumColumns.asStateFlow()
    /** 首页推荐/COS 两格共用的列数；与相册/收藏/历史的 [albumColumns] 分开保存。 */
    val homeColumns = mutableHomeColumns.asStateFlow()
    val defaultEndpoint = mutableDefaultEndpoint.asStateFlow()

    /**
     * 已解密令牌（仅内存）：每个请求的鉴权拦截器都读一次 [tokenFor]，而一次 AndroidKeyStore
     * 取密钥 + AES-GCM 解密是两次 Keystore 绑定调用（旧版鉴权拦截器读的是内存里的
     * ServerConfigDataSource.currentToken()，零成本）。首屏并发几十个请求时这些调用还会被
     * 本类的 [Synchronized] 串行化，肉眼可见地推迟第一批缩略图。只在 [saveSession] 成功
     * 后写入、[clearSession] 时清空——与磁盘上的密文同生命周期，不新增持久化面。
     */
    private var cachedToken: String? = null
    /** Keystore 里的会话密钥：进程内只 load 一次（密钥不轮换，轮换必然伴随重新登录）。 */
    private var cachedKey: SecretKey? = null

    init {
        try {
            storage.getString("session", null)?.let { mutableSession.value = gson.fromJson(it, Session::class.java) }
            storage.getString("preferences", null)?.let { raw ->
                val json = com.google.gson.JsonParser.parseString(raw).asJsonObject
                val material = json.get("material")?.asString?.let { name -> TabBarMaterial.entries.find { it.name == name } }
                    ?: if (json.get("glass")?.asBoolean == false) TabBarMaterial.SOLID else TabBarMaterial.LIQUID
                mutablePreferences.value = gson.fromJson(raw, Preferences::class.java).copy(material = material)
            }
            storage.getString("recent-servers", null)?.let { mutableRecentServers.value = gson.fromJson(it, Array<String>::class.java).toList() }
            storage.getString("search-history", null)?.let { mutableSearchHistory.value = gson.fromJson(it, Array<String>::class.java).toList() }
            mutableAlbumColumns.value = storage.getInt("album-columns", DEFAULT_ALBUM_COLUMNS).coerceIn(MIN_GRID_COLUMNS, MAX_GRID_COLUMNS)
            mutableHomeColumns.value = storage.getInt("home-columns", DEFAULT_HOME_COLUMNS).coerceIn(MIN_HOME_COLUMNS, MAX_GRID_COLUMNS)
            mutableDefaultEndpoint.value = storage.getString("default-endpoint", null)?.takeIf { it in setOf("local", "nas") }
        } catch (_: Exception) {
            mutableError.value = "连接配置无法读取，请重新登录。原有传输记录仍保留。"
        }
    }

    @Synchronized
    fun tokenFor(target: Session): String {
        check(sameTarget(target, mutableSession.value)) { "连接已变更，请重新登录此任务所属服务器" }
        cachedToken?.let { return it }
        val encoded = storage.getString("token", null) ?: error("登录凭据已移除")
        val bytes = Base64.decode(encoded, Base64.NO_WRAP)
        require(bytes.size > 12) { "登录凭据损坏，请重新登录" }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
        return cipher.doFinal(bytes.copyOfRange(12, bytes.size)).toString(Charsets.UTF_8).also { cachedToken = it }
    }

    @Synchronized
    fun saveSession(session: Session, token: String, certificateDerBase64: String? = null, rememberAddress: Boolean = false, loginEndpoint: String? = null) {
        require(loginEndpoint == null || loginEndpoint in setOf("local", "nas"))
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val encrypted = Base64.encodeToString(cipher.iv + cipher.doFinal(token.toByteArray()), Base64.NO_WRAP)
        val recent = if (rememberAddress) (listOf(session.baseUrl) + mutableRecentServers.value).distinct().take(5) else mutableRecentServers.value
        val editor = storage.edit().putString("session", gson.toJson(session)).putString("token", encrypted)
            .putString("certificate", certificateDerBase64).putString("recent-servers", gson.toJson(recent))
        if (loginEndpoint != null) {
            editor.putString("default-endpoint", loginEndpoint)
            // Session-scoped copy: the preference above can change later from settings, but cache
            // pools must keep treating this session the way its login actually reached the server.
            editor.putString("session-endpoint", loginEndpoint)
        } else if (mutableSession.value?.let { sameTarget(it, session) } != true) {
            // A silent relogin keeps the marker only for the same server; otherwise it would lie.
            editor.remove("session-endpoint")
        }
        check(editor.commit()) { "无法保存登录凭据" }
        cachedToken = token
        mutableSession.value = session
        mutableTransportRevision.value++
        mutableRecentServers.value = recent
        loginEndpoint?.let { mutableDefaultEndpoint.value = it }
        mutableError.value = null
    }

    @Synchronized
    fun certificateFor(target: Session): String? {
        check(sameTarget(target, mutableSession.value)) { "连接已变更" }
        return storage.getString("certificate", null)
    }

    /** Endpoint used to sign into the selected session ("local"/"nas"), or null for older sessions. */
    @Synchronized
    fun loginEndpointFor(target: Session): String? {
        check(sameTarget(target, mutableSession.value)) { "连接已变更" }
        return storage.getString("session-endpoint", null)?.takeIf { it in setOf("local", "nas") }
    }

    fun savePreferences(preferences: Preferences) {
        check(storage.edit().putString("preferences", gson.toJson(preferences)).commit()) { "无法保存外观设置" }
        mutablePreferences.value = preferences
    }

    @Synchronized
    fun saveAlbumColumns(columns: Int) {
        val value = columns.coerceIn(MIN_GRID_COLUMNS, MAX_GRID_COLUMNS)
        if (value == mutableAlbumColumns.value) return
        check(storage.edit().putInt("album-columns", value).commit()) { "无法保存网格列数" }
        mutableAlbumColumns.value = value
    }

    /**
     * 首页推荐/COS 两格共用的列数（单键：两格始终同一档）。范围与相册三页同一保存纪律
     * （列数循环/捏合适用的 2~5 列），但下界是 1——首页顶栏提供单列大图流。值未变不重复写盘。
     */
    @Synchronized
    fun saveHomeColumns(columns: Int) {
        val value = columns.coerceIn(MIN_HOME_COLUMNS, MAX_GRID_COLUMNS)
        if (value == mutableHomeColumns.value) return
        check(storage.edit().putInt("home-columns", value).commit()) { "无法保存首页网格列数" }
        mutableHomeColumns.value = value
    }

    @Synchronized
    fun saveDefaultEndpoint(endpoint: String) {
        require(endpoint in setOf("local", "nas"))
        check(storage.edit().putString("default-endpoint", endpoint).commit()) { "无法保存默认登录设置" }
        mutableDefaultEndpoint.value = endpoint
    }

    @Synchronized
    fun recordSearch(word: String) {
        val value = word.trim()
        if (value.isEmpty()) return
        val next = (listOf(value) + mutableSearchHistory.value).distinct().take(20)
        check(storage.edit().putString("search-history", gson.toJson(next)).commit()) { "无法保存搜索历史" }
        mutableSearchHistory.value = next
    }
    @Synchronized
    fun clearSearchHistory() {
        check(storage.edit().remove("search-history").commit()) { "无法清除搜索历史" }
        mutableSearchHistory.value = emptyList()
    }

    fun uploadLibrary(target: Session): String? = storage.getString("upload-library-${target.instanceId}", null)
    fun saveUploadLibrary(target: Session, id: String) {
        check(storage.edit().putString("upload-library-${target.instanceId}", id).commit()) { "无法保存上传位置" }
    }

    @Synchronized
    internal fun sealDeviceSecret(identity: String, secret: String): String {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, key()); cipher.updateAAD(identity.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(cipher.iv + cipher.doFinal(secret.toByteArray(Charsets.UTF_8)), Base64.NO_WRAP)
    }
    @Synchronized
    internal fun openDeviceSecret(identity: String, encrypted: String): String {
        val bytes = Base64.decode(encrypted, Base64.NO_WRAP)
        check(bytes.size > 12) { "设备授权不可用，请重新配置" }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12))); cipher.updateAAD(identity.toByteArray(Charsets.UTF_8))
        return cipher.doFinal(bytes.copyOfRange(12, bytes.size)).toString(Charsets.UTF_8)
    }

    @Synchronized
    fun clearSession() {
        check(storage.edit().remove("session").remove("token").remove("certificate").remove("session-endpoint").commit()) { "无法清除登录凭据" }
        cachedToken = null
        mutableSession.value = null
        mutableTransportRevision.value++
    }

    private fun key(): SecretKey {
        cachedKey?.let { return it }
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey("qimeng.next.session", null) as? SecretKey)?.let { stored ->
            cachedKey = stored
            return stored
        }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder("qimeng.next.session", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey().also { cachedKey = it }
    }
}

/** 网格列数档位：与客户端列数循环/捏合范围一致（2~5 列）。 */
private const val MIN_GRID_COLUMNS = 2
private const val MAX_GRID_COLUMNS = 5
/**
 * 首页允许单列（顶栏 1 列 ↔ 2 列切换，旧版 KEY_HOME_COLUMNS 同为 1~2），捏合仍只在 2~5 档。
 * 读取与保存都必须用这个下界，否则用户选的单列在重启后被静默截成两列。
 */
private const val MIN_HOME_COLUMNS = 1
/** 相册/收藏/历史共用的缺省列数（旧版相册页默认 3 列）。 */
private const val DEFAULT_ALBUM_COLUMNS = 3
/** 首页推荐/COS 的缺省列数（沿用当前首页双列）。 */
private const val DEFAULT_HOME_COLUMNS = 2
