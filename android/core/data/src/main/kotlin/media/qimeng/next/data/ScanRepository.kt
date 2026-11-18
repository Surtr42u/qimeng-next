package media.qimeng.next.data

import media.qimeng.next.model.Library
import media.qimeng.next.model.LibraryScan
import media.qimeng.next.model.Session
import media.qimeng.next.sdk.model.RevisionRequest
import media.qimeng.next.sdk.model.ScanJob

class ScanRepository internal constructor(private val settings: SecureSettings, private val provider: ApiProvider) {
    private fun api() = provider.api(settings.session.value ?: error("请先登录服务器"))
    suspend fun start(target: Session, library: Library, operationId: String): LibraryScan = provider.api(target).startLibraryScan(id = library.id,
        idempotencyKey = operationId, revisionRequest = RevisionRequest(library.revision)).bodyOrThrow().model()
    suspend fun get(id: String): LibraryScan = api().getLibraryScan(id).bodyOrThrow().model()

    /** Explicit owned-process handoff; this independent connection never changes the selected server. */
    suspend fun startOwned(baseUrl: String, instanceId: String, certificate: String, bootstrap: String,
                           libraryId: String, revision: Long, operationId: String): String {
        val declared = provider.anonymous(baseUrl, certificate).getInstance().bodyOrThrow()
        check(declared.id == instanceId) { "本机实例核验失败，未提交扫描" }
        return provider.scoped(baseUrl, certificate, bootstrap).startLibraryScan(id = libraryId,
            idempotencyKey = operationId, revisionRequest = RevisionRequest(revision)).bodyOrThrow().id
    }
}

private fun ScanJob.model() = LibraryScan(id, libraryId, state.value, processed, added, moved, changed, skipped, issueCount)
