package media.qimeng.next.data

import media.qimeng.next.model.AIAccessGrant
import media.qimeng.next.model.AIAccessPolicy
import media.qimeng.next.model.Page
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.AIDelegation
import media.qimeng.next.sdk.model.AIDelegationPolicy
import java.io.IOException

class AIAccessRepository internal constructor(private val settings: SecureSettings, private val provider: ApiProvider) {
    private fun api() = provider.api(settings.session.value ?: error("请先登录服务器"))
    suspend fun list(state: String, cursor: String? = null): Page<AIAccessGrant> {
        val page = api().listAIDelegations(cursor = cursor, limit = 50,
            state = DefaultApi.StateListAIDelegations.entries.first { it.value == state }).bodyOrThrow()
        return Page(page.items.map { it.toModel() }, page.nextCursor)
    }

    /** The one-time secret is only returned to the creation callback, never journaled. */
    suspend fun create(policy: AIAccessPolicy): Pair<AIAccessGrant, String> {
        val input = AIDelegationPolicy(label = policy.label, libraryIds = policy.libraryIds.toSet(), authorIds = policy.authorIds.toSet(),
            allAuthors = policy.allAuthors, allowedFields = policy.allowedFields.map { field -> AIDelegationPolicy.AllowedFields.entries.first { it.value == field } }.toSet(),
            candidateBlocks = policy.candidateBlocks.map { block -> AIDelegationPolicy.CandidateBlocks.entries.first { it.value == block } }.toSet(),
            allowClear = policy.allowClear, canIngest = policy.canIngest, canResearch = policy.canResearch, canOrganize = policy.canOrganize,
            maxResearchCalls = policy.maxResearchCalls, maxUpdates = policy.maxUpdates, maxTasksPerDay = policy.maxTasksPerDay,
            validDays = policy.validDays, maxCallsPerTask = policy.maxCallsPerTask, taskSeconds = policy.taskSeconds)
        val target = settings.session.value ?: error("请先登录服务器")
        val result = provider.apiWithoutRetries(target).createAIDelegation(input).bodyOrThrow()
        val token = result.credential.token ?: throw IOException("凭据未返回。请刷新连接列表核对；需要新凭据时先撤销对应授权。")
        return result.delegation.toModel() to token
    }
    suspend fun revoke(id: String) {
        val result = api().revokeAIDelegation(id)
        if (!result.isSuccessful) result.bodyOrThrow()
    }
}

private fun AIDelegation.toModel() = AIAccessGrant(id, label, state.value, expiresAt, libraryIds.toList(), authorIds.toList(), allAuthors,
    allowedFields.map { it.value }, candidateBlocks.map { it.value }, canIngest, canResearch, canOrganize, maxTasksPerDay, maxUpdates, maxResearchCalls)
