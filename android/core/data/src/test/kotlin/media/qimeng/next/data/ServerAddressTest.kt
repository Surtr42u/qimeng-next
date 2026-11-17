package media.qimeng.next.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class ServerAddressTest {
    @Test fun httpsRootAndLoopbackTunnelAreAccepted() {
        assertEquals("https://example.test/", normalizedServer(" https://example.test "))
        assertEquals("http://127.0.0.1:19876/", normalizedServer("http://127.0.0.1:19876"))
    }

    @Test fun credentialsAndUnencryptedRemoteOriginsAreRejectedBeforeAnyRequest() {
        listOf("https://user:secret@example.test", "http://example.test", "https://example.test/api", "https://example.test/?token=x")
            .forEach { candidate -> assertThrows(IllegalArgumentException::class.java) { normalizedServer(candidate) } }
    }
}
