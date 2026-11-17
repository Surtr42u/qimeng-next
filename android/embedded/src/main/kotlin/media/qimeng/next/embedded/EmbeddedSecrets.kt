package media.qimeng.next.embedded

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Host bootstrap credential, independent of the user's revocable API session. */
internal class EmbeddedSecrets(context: Context) {
    private val prefs = context.getSharedPreferences("embedded-host", Context.MODE_PRIVATE)
    private val alias = "qimeng.next.embedded.bootstrap"
    val confirmedInstance: String? get() = prefs.getString("instance", null)
    var enabled: Boolean
        get() = prefs.getBoolean("enabled", false)
        set(value) { check(prefs.edit().putBoolean("enabled", value).commit()) { "无法保存本机运行设置" } }

    @Synchronized
    fun token(allowCreate: Boolean): String {
        val stored = prefs.getString("bootstrap-v1", null)
        val keyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        if (stored != null) {
            val key = keyStore.getKey(alias, null) as? SecretKey ?: error("本机凭据密钥不可用，请保留数据并恢复凭据")
            val bytes = Base64.decode(stored, Base64.NO_WRAP)
            require(bytes.size > 12) { "本机凭据损坏" }
            return Cipher.getInstance("AES/GCM/NoPadding").run {
                init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
                doFinal(bytes.copyOfRange(12, bytes.size)).toString(Charsets.UTF_8)
            }
        }
        check(allowCreate) { "已有本机数据但启动凭据缺失，不会自动替换" }
        val key = (keyStore.getKey(alias, null) as? SecretKey) ?: KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
        }.generateKey()
        val token = Base64.encodeToString(ByteArray(32).also { SecureRandom().nextBytes(it) }, Base64.NO_WRAP or Base64.URL_SAFE)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key) }
        val encoded = Base64.encodeToString(cipher.iv + cipher.doFinal(token.toByteArray()), Base64.NO_WRAP)
        check(prefs.edit().putString("bootstrap-v1", encoded).commit()) { "无法保存本机启动凭据" }
        return token
    }

    fun confirm(instanceId: String) {
        val old = confirmedInstance
        check(old == null || old == instanceId) { "本机实例身份已改变，请先核对数据恢复结果" }
        check(prefs.edit().putString("instance", instanceId).commit()) { "无法保存本机实例身份" }
    }
}
