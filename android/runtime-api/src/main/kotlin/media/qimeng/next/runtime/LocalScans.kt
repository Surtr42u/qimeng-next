package media.qimeng.next.runtime

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/** Host scheduling intent only; the Go service owns scan progress and every indexed asset. */
data class LocalScanRequest(val id: String, val instanceId: String, val libraryId: String, val expectedRevision: Long,
    val state: String = "queued", val jobId: String? = null, val message: String = "等待提交扫描")
data class LocalScanState(val chargeOnly: Boolean = true, val requests: List<LocalScanRequest> = emptyList(), val error: String? = null)

interface LocalScans {
    val state: StateFlow<LocalScanState>
    suspend fun setChargeOnly(enabled: Boolean)
    suspend fun request(instanceId: String, libraryId: String, expectedRevision: Long)
    suspend fun forget(instanceId: String, libraryId: String)
    fun resume()
}

/** Implemented by the App composition root; embedded never references generated SDK or core:data. */
interface LocalScanHost {
    val runtime: LocalRuntime
    suspend fun submitLocalScan(connection: LocalConnection, request: LocalScanRequest): String
}
class LocalScanFailure(message: String, val retryable: Boolean) : Exception(message)

object UnavailableLocalScans : LocalScans {
    override val state = MutableStateFlow(LocalScanState(error = "此安装包未包含本机扫描调度"))
    override suspend fun setChargeOnly(enabled: Boolean) = error("此安装包未包含本机扫描调度")
    override suspend fun request(instanceId: String, libraryId: String, expectedRevision: Long) = error("此安装包未包含本机扫描调度")
    override suspend fun forget(instanceId: String, libraryId: String) = Unit
    override fun resume() = Unit
}
