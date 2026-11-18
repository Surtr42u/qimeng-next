package media.qimeng.next.data

import org.junit.Assert.*
import org.junit.Test

class ServerAdvertisementTest {
    private val valid = mapOf("txtvers" to "1", "api" to "0.1.0", "id" to "synthetic-instance",
        "scheme" to "http", "path" to "/api/v1")
    private fun parse(values: Map<String, String> = valid, hosts: List<String> = listOf("192.0.2.18"), port: Int = 18488) =
        serverAdvertisement(values.mapValues { it.value.toByteArray() }, hosts, port)

    @Test fun usesSrvPortAndDoesNotInventDefaultEndpoint() {
        assertEquals("http://192.0.2.18:18488/", parse().single().address)
        assertTrue(parse(port = 0).isEmpty())
        assertTrue(parse(valid - "id").isEmpty())
        assertTrue(parse(valid + ("api" to "2.0")).isEmpty())
    }
    @Test fun secureOriginIsExplicitAndRejectsCredentialsPathsAndDowngrades() {
        assertEquals("https://library.example:9443/", parse(valid + ("origin" to "https://library.example:9443")).single().address)
        listOf("http://library.example", "https://owner:secret@library.example", "https://library.example/api", "https://library.example/?token=x")
            .forEach { assertTrue(parse(valid + ("origin" to it)).isEmpty()) }
    }
    @Test fun neverDropsIpv6ScopeToSendRequestsToADifferentInterface() {
        assertTrue(parse(hosts = listOf("fe80::1%wlan0")).isEmpty())
        assertEquals("http://[2001:db8::1]:18488/", parse(hosts = listOf("2001:db8::1")).single().address)
    }
    @Test fun booleanAndMalformedTxtValuesAreNotTrustedIdentifiers() {
        val bytes = valid.mapValues { it.value.toByteArray() }
        assertTrue(serverAdvertisement(bytes + ("id" to null), listOf("192.0.2.18"), 18488).isEmpty())
        assertTrue(serverAdvertisement(bytes + ("id" to byteArrayOf(0xc3.toByte(), 0x28)), listOf("192.0.2.18"), 18488).isEmpty())
    }
}
