package media.qimeng.next.transport

import java.io.ByteArrayInputStream
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.cert.CertificateException
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.util.Base64
import javax.net.ssl.SSLContext
import javax.net.ssl.SSLSocketFactory
import javax.net.ssl.X509TrustManager

/** Exact leaf trust supplied by an authenticated private channel. Hostname checks stay with the HTTP client. */
class PinnedTls(certificateDerBase64: String) {
    val trustManager: X509TrustManager
    val socketFactory: SSLSocketFactory

    init {
        require(certificateDerBase64.length in 1..24_000) { "服务器证书长度无效" }
        val bytes = Base64.getDecoder().decode(certificateDerBase64)
        require(bytes.size in 1..16_384) { "服务器证书长度无效" }
        val certificate = ByteArrayInputStream(bytes).use { input ->
            val parsed = CertificateFactory.getInstance("X.509").generateCertificate(input) as X509Certificate
            require(input.available() == 0) { "服务器证书包含额外数据" }
            parsed
        }
        certificate.checkValidity()
        trustManager = object : X509TrustManager {
            override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
                val leaf = chain?.firstOrNull() ?: throw CertificateException("Missing server certificate")
                if (!MessageDigest.isEqual(bytes, leaf.encoded)) throw CertificateException("Server certificate differs from the private startup receipt")
                leaf.checkValidity()
            }
            override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?): Unit = throw CertificateException("Client authentication is not configured")
            override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf(certificate)
        }
        socketFactory = SSLContext.getInstance("TLS").apply { init(null, arrayOf(trustManager), SecureRandom()) }.socketFactory
    }
}
