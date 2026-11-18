package media.qimeng.next.data

import android.content.Context
import android.net.Uri
import com.google.gson.Gson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.AuthorAvatarFetch
import media.qimeng.next.sdk.model.AuthorProfileAccount
import media.qimeng.next.sdk.model.AuthorProfileAvatar
import media.qimeng.next.sdk.model.AuthorProfileBlock
import media.qimeng.next.sdk.model.AuthorProfileEvidence
import media.qimeng.next.sdk.model.AuthorProfileValues
import media.qimeng.next.sdk.model.AuthorProfileWrite
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.util.UUID

class AuthorProfileRepository internal constructor(private val context: Context, private val settings: SecureSettings, private val provider: ApiProvider) {
    private fun target() = settings.session.value ?: error("请先登录服务器")
    suspend fun feed(target: Session, id: String): AuthorFeed = provider.api(target).getAuthorUpdates(id).bodyOrThrow().asFeed()
    suspend fun saveFeed(target: Session, base: AuthorFeed, enabled: Boolean, url: String, key: String): AuthorFeed =
        provider.api(target).setAuthorUpdates(id = base.authorId, idempotencyKey = key,
            authorUpdatesInput = media.qimeng.next.sdk.model.AuthorUpdatesInput(expectedRevision = base.revision, trackUpdates = enabled, feedUrl = url))
            .bodyOrThrow().asFeed()
    suspend fun checkFeed(target: Session, base: AuthorFeed, key: String): AuthorFeed =
        provider.api(target).checkAuthorUpdates(id = base.authorId, idempotencyKey = key,
            revisionRequest = media.qimeng.next.sdk.model.RevisionRequest(base.revision)).bodyOrThrow().asFeed()
    suspend fun profile(id: String): ProfileDocument {
        val target = target()
        val value = provider.api(target).getAuthorProfile(id).bodyOrThrow()
        return ProfileDocument(target, value.authorId, value.editable, value.blocks.map { it.toModel(target) })
    }
    suspend fun summary(id: String): ProfileSummary {
        val target = target()
        val value = provider.api(target).getAuthorProfileSummary(id).bodyOrThrow()
        return ProfileSummary(value.avatar?.toModel(target), value.biographyOriginal, value.biographyZh)
    }
    suspend fun save(document: ProfileDocument, block: ProfileBlock, values: ProfileValues, evidence: List<ProfileEvidence>, candidateId: String?, reviewed: Boolean): ProfileBlock {
        val input = AuthorProfileWrite(expectedRevision = block.revision, propertyValues = values.toApi(),
            evidence = evidence.map { it.toApi() }, candidateId = candidateId, translationReviewed = reviewed)
        val key = UUID.nameUUIDFromBytes(Gson().toJson(listOf(document.target.instanceId, document.authorId, block.block, input)).toByteArray()).toString()
        return provider.api(document.target).updateAuthorProfileBlock(id = document.authorId,
            block = block.block,
            idempotencyKey = key, authorProfileWrite = input).bodyOrThrow().toModel(document.target)
    }
    suspend fun candidates(id: String, cursor: String? = null, archiveId: String? = null): Page<ProfileCandidate> {
        val page = provider.api(target()).listAuthorProfileCandidates(id = id, cursor = cursor, limit = 50, archiveId = archiveId).bodyOrThrow()
        return Page(page.items.map { ProfileCandidate(it.id, it.block.value, it.baseRevision, it.state.value, it.propertyValues.toModel(), it.evidence.map { e -> e.toModel() }, it.createdAt) }, page.nextCursor)
    }
    suspend fun dismiss(document: ProfileDocument, candidate: ProfileCandidate) {
        provider.api(document.target).dismissAuthorProfileCandidate(id = document.authorId, candidateId = candidate.id,
            idempotencyKey = UUID.randomUUID().toString()).bodyOrThrow()
    }
    suspend fun fetchAvatar(document: ProfileDocument, revision: Long, url: String): ProfileBlock = provider.api(document.target)
        .fetchAuthorProfileAvatar(id = document.authorId, idempotencyKey = UUID.randomUUID().toString(),
            authorAvatarFetch = AuthorAvatarFetch(expectedRevision = revision, sourceUrl = url)).bodyOrThrow().toModel(document.target)

    suspend fun uploadAvatar(document: ProfileDocument, revision: Long, uri: Uri): ProfileBlock = withContext(Dispatchers.IO) {
        require(uri.scheme == "content") { "请通过系统文件选择头像" }
        val limit = 4 * 1024 * 1024
        val bytes = context.contentResolver.openInputStream(uri)?.use { input ->
            val output = ByteArrayOutputStream()
            val buffer = ByteArray(8192)
            while (output.size() <= limit) {
                val count = input.read(buffer, 0, minOf(buffer.size, limit + 1 - output.size()))
                if (count < 0) break
                output.write(buffer, 0, count)
            }
            require(output.size() in 1..limit) { "头像文件必须大于零且不超过 4 MiB" }
            output.toByteArray()
        } ?: throw IOException("无法读取所选头像")
        provider.api(document.target).uploadAuthorProfileAvatar(id = document.authorId, idempotencyKey = UUID.randomUUID().toString(),
            expectedRevision = revision, body = bytes.toRequestBody("application/octet-stream".toMediaType())).bodyOrThrow().toModel(document.target)
    }
    suspend fun clearAvatar(document: ProfileDocument, revision: Long): ProfileBlock = provider.api(document.target)
        .clearAuthorProfileAvatar(id = document.authorId, idempotencyKey = UUID.randomUUID().toString(), expectedRevision = revision).bodyOrThrow().toModel(document.target)
    suspend fun archives(id: String, cursor: String?): Page<ProfileArchive> {
        val page = provider.api(target()).listAuthorProfileArchives(id = id, cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { ProfileArchive(it.id, it.authorId, it.authorName, it.reason, it.createdAt, it.blockCount, it.candidateCount, it.conflicts) }, page.nextCursor)
    }
    suspend fun archive(authorId: String, archiveId: String): ProfileDocument {
        val target = target()
        val value = provider.api(target).getAuthorProfileArchive(id = authorId, archiveId = archiveId).bodyOrThrow()
        return ProfileDocument(target, authorId, false, value.blocks.map { it.toModel(target) })
    }
}

private fun AuthorProfileBlock.toModel(target: Session) = ProfileBlock(block.value, revision, propertyValues.toModel(), evidence.map { it.toModel() }, source.value, updatedAt, avatar?.toModel(target))
private fun AuthorProfileValues.toModel() = ProfileValues(handle = handle, displayName = displayName, biographyOriginal = biographyOriginal,
    biographyZh = biographyZh, avatarSourceUrl = avatarSourceUrl, summary = summary, note = note,
    accounts = accounts?.map { ProfileAccount(it.platform, it.identifier, it.url, it.publishingUrl) }, profileUrl = profileUrl, publicName = publicName, remark = remark, identityStatus = identityStatus?.value)
private fun ProfileValues.toApi() = AuthorProfileValues(handle = handle, displayName = displayName, biographyOriginal = biographyOriginal,
    biographyZh = biographyZh, avatarSourceUrl = avatarSourceUrl, summary = summary, note = note,
    accounts = accounts?.map { AuthorProfileAccount(platform = it.platform, identifier = it.identifier, url = it.url, publishingUrl = it.publishingUrl) },
    profileUrl = profileUrl, publicName = publicName, remark = remark,
    identityStatus = identityStatus?.let { value -> AuthorProfileValues.IdentityStatus.entries.first { it.value == value } })
private fun AuthorProfileEvidence.toModel() = ProfileEvidence(url, title, fields, status.value, note, checkedAt)
private fun ProfileEvidence.toApi() = AuthorProfileEvidence(url = url, title = title, fields = fields,
    status = AuthorProfileEvidence.Status.entries.first { it.value == status }, note = note, checkedAt = checkedAt)
private fun AuthorProfileAvatar.toModel(target: Session): ProfileAvatar {
    val origin = target.baseUrl.toHttpUrl()
    val resolved = origin.resolve(url) ?: throw IOException("头像地址无效")
    require(sameOrigin(origin, resolved) && resolved.encodedPath.startsWith("/api/v1/profile-avatars/")) { "拒绝加载非当前服务器的头像" }
    return ProfileAvatar(resolved.toString(), "${target.baseUrl}:${target.instanceId}:avatar:$sourceHash:$contentHash", this.origin.value, sourceUrl, fetchedAt)
}

private fun media.qimeng.next.sdk.model.AuthorUpdates.asFeed() = AuthorFeed(authorId, revision, trackUpdates, feedUrl, checkedAt,
    items.map { AuthorUpdateItem(it.id, it.title, it.url.toString(), it.publishedAt) })
