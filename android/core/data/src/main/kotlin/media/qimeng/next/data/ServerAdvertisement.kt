package media.qimeng.next.data

import media.qimeng.next.model.ServerCandidate
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.nio.ByteBuffer
import java.nio.charset.CodingErrorAction

/** Parse the versioned advertisement without DNS lookups, API calls or persisted trust. */
internal fun serverAdvertisement(attributes: Map<String, ByteArray?>, hosts: List<String>, port: Int): List<ServerCandidate> {
    if (port !in 1..65535 || attributes.size > 32 || attributes.values.sumOf { it?.size ?: 0 } > 4096) return emptyList()
    fun text(key: String): String? = attributes[key]?.let {
        try { Charsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT)
            .decode(ByteBuffer.wrap(it)).toString() } catch (_: java.nio.charset.CharacterCodingException) { null }
    }
    if (text("txtvers") != "1" || text("api") != "0.1.0" || text("path") != "/api/v1") return emptyList()
    val id = text("id")?.takeIf { it.isNotBlank() && it.length <= 128 && it.none(Char::isISOControl) } ?: return emptyList()
    val scheme = text("scheme")?.takeIf { it == "http" || it == "https" } ?: return emptyList()
    if (attributes.containsKey("origin")) {
        val origin = text("origin")?.toHttpUrlOrNull() ?: return emptyList()
        if (!origin.isHttps || origin.username.isNotEmpty() || origin.password.isNotEmpty() || origin.encodedPath != "/" ||
            origin.query != null || origin.fragment != null) return emptyList()
        return listOf(ServerCandidate(origin.toString(), id))
    }
    return hosts.take(8).mapNotNull { host ->
        // Link-scoped IPv6 needs an interface-scoped transport, which the normal API client does not
        // provide. Keep other advertised addresses; never silently drop the scope and misroute it.
        if ('%' in host || host.isBlank()) return@mapNotNull null
        val address = try { HttpUrl.Builder().scheme(scheme).host(host).port(port).build() }
        catch (_: IllegalArgumentException) { return@mapNotNull null }
        ServerCandidate(address.toString(), id)
    }.distinctBy { it.address }
}
