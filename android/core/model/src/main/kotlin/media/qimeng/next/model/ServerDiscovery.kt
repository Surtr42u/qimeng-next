package media.qimeng.next.model

/** A DNS-SD claim only. It conveys neither TLS trust nor permission to send credentials. */
data class ServerCandidate(val address: String, val instanceId: String)
data class ServerDiscoveryState(val candidates: List<ServerCandidate> = emptyList(), val searching: Boolean = false,
    val finished: Boolean = false, val error: String? = null)
