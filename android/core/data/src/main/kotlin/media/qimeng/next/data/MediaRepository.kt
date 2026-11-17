package media.qimeng.next.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.AssetUpdate
import media.qimeng.next.sdk.model.AuthorUpdate
import media.qimeng.next.sdk.model.ReactionUpdate
import media.qimeng.next.sdk.model.SessionCreate
import media.qimeng.next.sdk.model.TagsUpdate
import media.qimeng.next.sdk.model.LibraryCreate
import media.qimeng.next.sdk.model.RevisionRequest
import media.qimeng.next.sdk.model.MoveRequest
import okhttp3.HttpUrl.Companion.toHttpUrl
import java.io.IOException
import java.util.UUID
import media.qimeng.next.sdk.model.Asset as ApiAsset
import media.qimeng.next.sdk.model.Author as ApiAuthor
import media.qimeng.next.sdk.model.Batch as ApiBatch

/** Only this data boundary knows generated DTOs. UI and ViewModels use client models. */
class MediaRepository internal constructor(val settings: SecureSettings, private val provider: ApiProvider) {
    val session = settings.session
    private fun target() = session.value ?: error("请先登录服务器")

    suspend fun login(address: String, token: String) = withContext(Dispatchers.IO) {
        val root = normalizedServer(address)
        val api = provider.anonymous(root)
        val instance = api.getInstance().bodyOrThrow()
        require(instance.apiVersion == "0.1.0") { "服务器协议不兼容，请同步更新客户端与服务器" }
        val created = api.createSession(SessionCreate(token = token, label = "Android 客户端")).bodyOrThrow()
        val secret = created.token ?: throw IOException("服务器未返回登录凭据")
        settings.saveSession(Session(root, instance.id, instance.name), secret)
    }

    suspend fun logout() = withContext(Dispatchers.IO) {
        val result = provider.api(target()).deleteCurrentSession()
        if (!result.isSuccessful && result.code() != 401) throw ApiFailure(result.code(), "无法撤销登录，请重试")
        settings.clearSession()
    }

    suspend fun capabilities(): ServerCapabilities {
        val target = target()
        val instance = provider.api(target).getInstance().bodyOrThrow()
        check(instance.id == target.instanceId) { "地址已指向另一个媒体库，请重新登录" }
        return ServerCapabilities(instance.capabilities.aiConfigured, instance.capabilities.recommendations, instance.capabilities.rankings)
    }

    suspend fun libraries(): List<Library> = provider.api(target()).listLibraries().bodyOrThrow().items.map { Library(it.id, it.name) }
    suspend fun createLibrary(name: String, cos: Boolean): Library {
        val value = provider.api(target()).createLibrary(idempotencyKey = UUID.randomUUID().toString(),
            libraryCreate = LibraryCreate(name = name, kind = LibraryCreate.Kind.entries.first { it.value == if (cos) "cos" else "regular" })).bodyOrThrow()
        return Library(value.id, value.name)
    }

    suspend fun assets(query: AssetQuery, cursor: String? = null): Page<Asset> {
        val target = target()
        val page = provider.api(target).listAssets(cursor = cursor, limit = 50, libraryId = query.libraryId,
            q = query.text.takeIf { it.isNotBlank() }, mediaType = query.mediaType, kind = query.kind,
            authorId = query.authorId, tag = query.tag, favorite = query.favorite, liked = query.liked, sort = query.sort).bodyOrThrow()
        return Page(page.items.map { it.toModel(target) }, page.nextCursor)
    }

    suspend fun asset(id: String): Asset {
        val target = target()
        return provider.api(target).getAsset(id).bodyOrThrow().toModel(target)
    }

    suspend fun reaction(asset: Asset, favorite: Boolean, liked: Boolean): Asset {
        val target = target()
        return provider.api(target).updateReaction(id = asset.id, idempotencyKey = UUID.randomUUID().toString(),
            reactionUpdate = ReactionUpdate(favorite = favorite, liked = liked)).bodyOrThrow().toModel(target)
    }

    suspend fun tags(asset: Asset, tags: List<String>): Asset {
        val target = target()
        val key = UUID.nameUUIDFromBytes(com.google.gson.Gson().toJson(listOf(target.instanceId, asset.id, asset.tagsRevision, tags)).toByteArray()).toString()
        return provider.api(target).updateAssetTags(id = asset.id, idempotencyKey = key,
            tagsUpdate = TagsUpdate(expectedRevision = asset.tagsRevision, tags = tags)).bodyOrThrow().toModel(target)
    }

    suspend fun updateNote(asset: Asset, note: String): Asset {
        val target = target()
        return provider.api(target).updateAsset(id = asset.id, assetUpdate = AssetUpdate(expectedRevision = asset.revision, note = note))
            .bodyOrThrow().toModel(target)
    }

    suspend fun tags(): List<String> = provider.api(target()).listTags().bodyOrThrow().items

    suspend fun history(cursor: String? = null): Page<HistoryEntry> {
        val page = provider.api(target()).listHistory(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { HistoryEntry(it.eventId, it.assetId, it.fileName ?: "已移除的媒体", it.kind.value,
            it.positionMs ?: 0, it.createdAt) }, page.nextCursor)
    }

    suspend fun authors(cursor: String? = null, query: String? = null): Page<Author> {
        val page = provider.api(target()).listAuthors(cursor = cursor, limit = 50, q = query).bodyOrThrow()
        return Page(page.items.map { it.toModel() }, page.nextCursor)
    }

    suspend fun author(id: String): Author = provider.api(target()).getAuthor(id).bodyOrThrow().toModel()
    suspend fun follow(author: Author): Author = provider.api(target()).updateAuthor(id = author.id,
        authorUpdate = AuthorUpdate(expectedRevision = author.revision, following = !author.followed)).bodyOrThrow().toModel()
    suspend fun editAuthor(author: Author, name: String, aliases: List<String>, biography: String, homepage: String): Author =
        provider.api(target()).updateAuthor(id = author.id, authorUpdate = AuthorUpdate(expectedRevision = author.revision,
            name = name, aliases = aliases, biography = biography, homepage = homepage)).bodyOrThrow().toModel()

    suspend fun trash(asset: Asset) {
        provider.api(target()).trashAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(), expectedRevision = asset.revision).bodyOrThrow()
    }
    suspend fun trashPage(cursor: String? = null): Page<Asset> {
        val target = target()
        val page = provider.api(target).listTrash(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { it.toModel(target) }, page.nextCursor)
    }
    suspend fun restore(asset: Asset) {
        provider.api(target()).restoreAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(),
            revisionRequest = RevisionRequest(expectedRevision = asset.revision)).bodyOrThrow()
    }
    suspend fun rename(asset: Asset, relativePath: String): Asset {
        val target = target()
        return provider.api(target).moveAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(),
            moveRequest = MoveRequest(expectedRevision = asset.revision, relativePath = relativePath)).bodyOrThrow().toModel(target)
    }

    suspend fun reports(cursor: String? = null): Page<TaskReport> {
        val page = provider.api(target()).listBatches(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { it.toModel() }, page.nextCursor)
    }

    suspend fun report(id: String): TaskReport {
        val result = provider.api(target()).getBatch(id).bodyOrThrow()
        return result.batch.toModel().copy(items = result.items.map {
            TaskItem(it.upload.id, it.upload.fileName, it.upload.state.value,
                listOfNotNull(it.upload.errorMessage, it.researchOutcome.takeIf(String::isNotBlank)).joinToString(" · "), it.upload.assetId)
        })
    }

    suspend fun statistics(): InventoryStats {
        val value = provider.api(target()).getStats().bodyOrThrow()
        return InventoryStats(value.assets, value.images, value.videos, value.bytes, value.favorites, value.authors, value.events, value.trash)
    }

    fun mediaClient() = provider.client(target())
}

private fun ApiAsset.toModel(target: Session): Asset {
    val root = target.baseUrl.toHttpUrl()
    val media = root.resolve(contentUrl) ?: throw IOException("媒体地址无效")
    check(sameOrigin(media, root)) { "服务器返回了不同来源的媒体地址" }
    return Asset(id = id, name = fileName, mediaType = mediaType.value, version = contentVersion,
        revision = revision, tagsRevision = tagsRevision, bytes = size, width = null, height = null, durationMs = null,
        authorId = authorId.takeIf(String::isNotBlank), authorName = authorName, tags = tags, favorite = favorite, liked = liked,
        positionMs = if (positionContentVersion == contentVersion) positionMs else 0, note = note,
        contentUrl = media.toString(), thumbnailUrl = media.toString(), addedAt = createdAt)
}

private fun ApiAuthor.toModel() = Author(id, name, aliases.orEmpty(), biography.orEmpty(), listOfNotNull(homepage), following, revision)
private fun ApiBatch.toModel() = TaskReport(id, mode.value, state.value,
    "${label} · 备份：${backupState.value} · 来源：${cleanupState.value}", createdAt, emptyList())
