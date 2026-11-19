package media.qimeng.next.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import media.qimeng.next.model.*
import media.qimeng.next.sdk.model.AssetUpdate
import media.qimeng.next.sdk.model.AuthorUpdate
import media.qimeng.next.sdk.model.AssetFavoriteUpdate
import media.qimeng.next.sdk.model.AssetTodayLikeUpdate
import media.qimeng.next.sdk.model.SessionCreate
import media.qimeng.next.sdk.model.TagsUpdate
import media.qimeng.next.sdk.model.LibraryCreate
import media.qimeng.next.sdk.model.LibraryUpdate
import media.qimeng.next.sdk.model.ResolveLibraryRootRequest
import media.qimeng.next.sdk.model.Library as ApiLibrary
import media.qimeng.next.sdk.model.CreateDirectoryRequest
import media.qimeng.next.sdk.model.AuthorCreate
import media.qimeng.next.sdk.model.AuthorMergeInput
import media.qimeng.next.sdk.model.CollectionCreate
import media.qimeng.next.sdk.model.CreateTagRequest
import media.qimeng.next.sdk.model.RevisionRequest
import media.qimeng.next.sdk.model.MoveRequest
import media.qimeng.next.sdk.api.DefaultApi
import okhttp3.HttpUrl.Companion.toHttpUrl
import java.io.IOException
import java.util.UUID
import java.util.concurrent.atomic.AtomicLong
import media.qimeng.next.sdk.model.Asset as ApiAsset
import media.qimeng.next.sdk.model.Author as ApiAuthor
import media.qimeng.next.sdk.model.Batch as ApiBatch
import media.qimeng.next.sdk.model.BatchInput

/** Only this data boundary knows generated DTOs. UI and ViewModels use client models. */
class MediaRepository internal constructor(val settings: SecureSettings, private val provider: ApiProvider) {
    val session = settings.session
    private val authenticationGeneration = AtomicLong()
    private val authenticationLock = Any()
    private fun target() = session.value ?: error("请先登录服务器")

    /** Remembered endpoints are checked without bearer, cookies, password or a trust bypass. */
    suspend fun discoverRemembered(address: String): ServerCandidate? = withTimeoutOrNull(2_000) {
        try {
            val root = normalizedServer(address)
            val instance = provider.anonymous(root).getInstance().bodyOrThrow()
            if (instance.apiVersion == "0.1.0" && !instance.id.isNullOrBlank() && instance.id.length <= 128) ServerCandidate(root, instance.id) else null
        } catch (_: IOException) { null }
        catch (_: IllegalArgumentException) { null }
        catch (_: com.google.gson.JsonParseException) { null }
    }

    suspend fun login(address: String, token: String, certificateDerBase64: String? = null, expectedInstanceId: String? = null, rememberAddress: Boolean = false,
                      loginEndpoint: String? = null) =
        authenticate(address, SessionCreate(token = token, label = "Android 客户端"), certificateDerBase64, expectedInstanceId, rememberAddress, loginEndpoint)

    suspend fun loginWithPassword(address: String, password: String, certificateDerBase64: String? = null,
                                  expectedInstanceId: String? = null, rememberAddress: Boolean = true, loginEndpoint: String? = null) {
        require(password.isNotEmpty()) { "请输入服务器密码" }
        authenticate(address, SessionCreate(password = password, label = "Android 客户端"), certificateDerBase64, expectedInstanceId, rememberAddress, loginEndpoint)
    }

    private suspend fun authenticate(address: String, credentials: SessionCreate, certificateDerBase64: String? = null,
                                     expectedInstanceId: String? = null, rememberAddress: Boolean = false, loginEndpoint: String? = null) = withContext(Dispatchers.IO) {
        val (attempt, connection) = synchronized(authenticationLock) { authenticationGeneration.incrementAndGet() to settings.transportRevision.value }
        val root = normalizedServer(address)
        val api = provider.anonymous(root, certificateDerBase64)
        val instance = api.getInstance().bodyOrThrow()
        check(expectedInstanceId == null || instance.id == expectedInstanceId) { "服务实例与启动回执不匹配" }
        require(instance.apiVersion == "0.1.0") { "服务器协议不兼容，请同步更新客户端与服务器" }
        val created = try { api.createSession(credentials).bodyOrThrow() }
        catch (failure: ApiFailure) {
            if (failure.status == 401 && credentials.password != null) throw ApiFailure(401, "密码错误，请重新输入")
            throw failure
        }
        val secret = created.token ?: throw IOException("服务器未返回登录凭据")
        synchronized(authenticationLock) {
            check(attempt == authenticationGeneration.get() && connection == settings.transportRevision.value) { "登录期间连接已改变，已保留较新的选择" }
            settings.saveSession(Session(root, instance.id, instance.name), secret, certificateDerBase64, rememberAddress, loginEndpoint)
        }
    }

    suspend fun logout() = withContext(Dispatchers.IO) {
        val attempt = synchronized(authenticationLock) { authenticationGeneration.incrementAndGet() }
        val result = provider.api(target()).deleteCurrentSession()
        if (!result.isSuccessful && result.code() != 401) throw ApiFailure(result.code(), "无法撤销登录，请重试")
        synchronized(authenticationLock) { if (attempt == authenticationGeneration.get()) settings.clearSession() }
    }
    suspend fun disconnect() = withContext(Dispatchers.IO) { synchronized(authenticationLock) { authenticationGeneration.incrementAndGet(); settings.clearSession() } }

    suspend fun capabilities(): ServerCapabilities {
        val target = target()
        val instance = provider.api(target).getInstance().bodyOrThrow()
        check(instance.id == target.instanceId) { "地址已指向另一个媒体库，请重新登录" }
        return ServerCapabilities(instance.capabilities.aiConfigured, instance.capabilities.recommendations, instance.capabilities.rankings)
    }

    suspend fun libraryPage(cursor: String? = null): Page<Library> {
        val page = provider.api(target()).listLibraries(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map(ApiLibrary::toModel), page.nextCursor, page.totalMatched)
    }
    suspend fun library(target: Session, id: String): Library = provider.api(target).getLibrary(id).bodyOrThrow().toModel()
    suspend fun createLibrary(name: String, cos: Boolean): Library {
        val value = provider.api(target()).createLibrary(idempotencyKey = UUID.randomUUID().toString(),
            libraryCreate = LibraryCreate(name = name, kind = LibraryCreate.Kind.entries.first { it.value == if (cos) "cos" else "regular" })).bodyOrThrow()
        return value.toModel()
    }
    suspend fun libraryRoot(target: Session, id: String): String = provider.api(target).getLibraryLocation(id).bodyOrThrow().rootPath
    suspend fun registerLibrary(target: Session, name: String, rootPath: String, cos: Boolean, key: String): Library {
        val api = provider.api(target)
        val resolved = api.resolveLibraryRoot(ResolveLibraryRootRequest(rootPath)).bodyOrThrow()
        val input = LibraryCreate(name = name, kind = LibraryCreate.Kind.entries.first { it.value == if (cos) "cos" else "regular" },
            storageId = resolved.storageId, rootRelativePath = resolved.rootRelativePath)
        return api.createLibrary(idempotencyKey = key, libraryCreate = input).bodyOrThrow().toModel()
    }
    suspend fun unregisterLibrary(target: Session, library: Library) {
        val reply = provider.api(target).deleteLibrary(id = library.id, expectedRevision = library.revision,
            idempotencyKey = "unregister-${library.id}-${library.revision}")
        if (!reply.isSuccessful) reply.bodyOrThrow()
    }
    suspend fun updateLibrary(library: Library, name: String, enabled: Boolean): Library {
        val value = provider.api(target()).updateLibrary(id = library.id, idempotencyKey = UUID.randomUUID().toString(),
            libraryUpdate = LibraryUpdate(expectedRevision = library.revision, name = name, enabled = enabled)).bodyOrThrow()
        return value.toModel()
    }
    suspend fun directories(target: Session, libraryId: String, parent: String, cursor: String?): DirectoryListing {
        val page = provider.api(target).listDirectories(id = libraryId, parent = parent, cursor = cursor, limit = 50).bodyOrThrow()
        return DirectoryListing(page.items.map { Directory(it.relativePath, it.name, it.fileCount, it.hasChildren) }, page.nextCursor, page.directFileCount)
    }
    suspend fun createDirectory(libraryId: String, path: String) {
        provider.api(target()).createDirectory(id = libraryId, idempotencyKey = UUID.randomUUID().toString(),
            createDirectoryRequest = CreateDirectoryRequest(relativePath = path)).bodyOrThrow()
    }
    suspend fun collections(libraryId: String, cursor: String? = null): Page<MediaCollection> {
        val page = provider.api(target()).listCollections(libraryId = libraryId, cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { MediaCollection(it.id, it.name, it.kind.value) }, page.nextCursor)
    }
    suspend fun createCollection(libraryId: String, name: String, kind: String) {
        provider.api(target()).createCollection(idempotencyKey = UUID.randomUUID().toString(),
            collectionCreate = CollectionCreate(libraryId = libraryId, name = name, kind = CollectionCreate.Kind.entries.first { it.value == kind })).bodyOrThrow()
    }

    suspend fun assets(query: AssetQuery, cursor: String? = null): Page<Asset> {
        val target = target()
        if (query.source == "history") {
            val page = history(cursor, query)
            return Page(page.items.map { row ->
                (row.asset ?: throw IOException("浏览历史缺少当前媒体投影")).copy(lastViewedAt = row.createdAt)
            }, page.nextCursor, page.totalMatched, page.dateCounts)
        }
        if (query.source == "recommendations") {
            val page = provider.api(target).getRecommendations(seed = query.seed, cursor = cursor, limit = 50,
                kind = query.kind?.let { value -> DefaultApi.KindGetRecommendations.entries.first { it.value == value } },
                mediaType = query.mediaType?.let { value -> DefaultApi.MediaTypeGetRecommendations.entries.first { it.value == value } }).bodyOrThrow()
            return Page(page.items.map { it.toModel(target) }, page.nextCursor)
        }
        if (query.source == "rankings") {
            val page = provider.api(target).getRankings(period = DefaultApi.PeriodGetRankings.entries.first { it.value == query.period },
                kind = query.kind?.let { value -> DefaultApi.KindGetRankings.entries.first { it.value == value } },
                mediaType = query.mediaType?.let { value -> DefaultApi.MediaTypeGetRankings.entries.first { it.value == value } }, cursor = cursor, limit = 50).bodyOrThrow()
            return Page(page.items.map { it.asset.toModel(target).copy(rankingScore = it.score) }, page.nextCursor)
        }
        val page = provider.api(target).listAssets(cursor = cursor, limit = 50, libraryId = query.libraryId,
            q = query.text.takeIf { it.isNotBlank() }, mediaType = query.mediaType?.let { type -> DefaultApi.MediaTypeListAssets.entries.first { it.value == type } },
            kind = query.kind?.let { kind -> DefaultApi.KindListAssets.entries.first { it.value == kind } },
            authorId = query.authorId, collectionId = query.collectionId, tag = query.tag, favorite = query.favorite, liked = query.liked,
            sort = DefaultApi.SortListAssets.entries.first { it.value == query.sort },
            order = DefaultApi.OrderListAssets.entries.first { it.value == query.order },
            workKeys = query.workKeys, roleKeys = query.roleKeys, tags = query.tags, history = query.history,
            tagMode = DefaultApi.TagModeListAssets.entries.first { it.value == query.tagMode },
            viewRange = DefaultApi.ViewRangeListAssets.entries.first { it.value == query.viewRange },
            playRange = DefaultApi.PlayRangeListAssets.entries.first { it.value == query.playRange },
            sizeRange = DefaultApi.SizeRangeListAssets.entries.first { it.value == query.sizeRange },
            timeRange = DefaultApi.TimeRangeListAssets.entries.first { it.value == query.timeRange }, fromYear = query.fromYear, toYear = query.toYear,
            dateCounts = cursor == null, dateCountsBy = if (cursor == null && query.sort == "favorited")
                DefaultApi.DateCountsByListAssets.entries.first { it.value == "favoritedAt" } else null,
            tzOffsetMinutes = java.util.TimeZone.getDefault().getOffset(System.currentTimeMillis()) / 60_000).bodyOrThrow()
        return Page(page.items.map { it.toModel(target) }, page.nextCursor, page.totalMatched,
            page.dateCounts.orEmpty().map { DateCount(it.date, it.fileCount) }, page.unknownDateCount)
    }

    suspend fun facets(row: String, query: AssetQuery, cursor: String? = null, search: String? = null): FacetResult {
        val page = provider.api(target()).getAssetFacets(row = DefaultApi.RowGetAssetFacets.entries.first { it.value == row },
            cursor = cursor, limit = 50, libraryId = query.libraryId, q = query.text.takeIf(String::isNotBlank), facetQ = search,
            kind = query.kind?.let { value -> DefaultApi.KindGetAssetFacets.entries.first { it.value == value } },
            mediaType = query.mediaType?.let { value -> DefaultApi.MediaTypeGetAssetFacets.entries.first { it.value == value } },
            authorId = query.authorId, collectionId = query.collectionId, favorite = query.favorite, liked = query.liked,
            workKeys = query.workKeys, roleKeys = query.roleKeys, tag = query.tag, tags = query.tags, history = query.history,
            tagMode = DefaultApi.TagModeGetAssetFacets.entries.first { it.value == query.tagMode },
            viewRange = DefaultApi.ViewRangeGetAssetFacets.entries.first { it.value == query.viewRange },
            playRange = DefaultApi.PlayRangeGetAssetFacets.entries.first { it.value == query.playRange },
            sizeRange = DefaultApi.SizeRangeGetAssetFacets.entries.first { it.value == query.sizeRange },
            timeRange = DefaultApi.TimeRangeGetAssetFacets.entries.first { it.value == query.timeRange }, fromYear = query.fromYear, toYear = query.toYear,
            tzOffsetMinutes = java.util.TimeZone.getDefault().getOffset(System.currentTimeMillis()) / 60_000).bodyOrThrow()
        return FacetResult(page.items.map { FacetChoice(it.key, it.kind.value, it.id, it.name, it.count) }, page.nextCursor, page.totalMatched, page.totalOptions)
    }

    suspend fun searchSuggestions(query: String, recommend: Boolean = false, target: Session = target()): List<SearchWord> = provider.api(target)
        .getSearchSuggestions(q = query, recommend = recommend, limit = 10).bodyOrThrow().items.map { SearchWord(it.id, it.type.value, it.name) }

    suspend fun asset(id: String, target: Session = target()): Asset {
        val record = provider.api(target).getAsset(id).bodyOrThrow()
        check(sameTarget(target, session.value)) { "请先连接此媒体原来的服务器" }
        return record.toModel(target)
    }

    suspend fun selectableVideo(target: Session, id: String): Asset {
        val record = provider.api(target).getAsset(id).bodyOrThrow()
        check(!record.trashed && record.mediaType.value == "video") { "所选视频已不在当前影库，请重新选择" }
        return record.toModel(target)
    }

    suspend fun setFavorite(asset: Asset, favorite: Boolean, key: String): Asset = provider.api(asset.target)
        .setAssetFavorite(asset.id, key, AssetFavoriteUpdate(favorite)).bodyOrThrow().toModel(asset.target)

    suspend fun setTodayLike(asset: Asset, liked: Boolean, key: String): Asset = provider.api(asset.target)
        .setAssetTodayLike(asset.id, key, AssetTodayLikeUpdate(liked)).bodyOrThrow().toModel(asset.target)

    suspend fun tags(asset: Asset, tags: List<String>): Asset {
        val target = asset.target
        val key = UUID.nameUUIDFromBytes(com.google.gson.Gson().toJson(listOf(target.instanceId, asset.id, asset.tagsRevision, tags)).toByteArray()).toString()
        return provider.api(target).updateAssetTags(id = asset.id, idempotencyKey = key,
            tagsUpdate = TagsUpdate(expectedRevision = asset.tagsRevision, tags = tags)).bodyOrThrow().toModel(target)
    }

    suspend fun updateNote(asset: Asset, note: String): Asset {
        val target = asset.target
        return provider.api(target).updateAsset(id = asset.id, assetUpdate = AssetUpdate(expectedRevision = asset.revision, note = note))
            .bodyOrThrow().toModel(target)
    }
    suspend fun updateAuthors(asset: Asset, ids: List<String>, names: List<String>, revision: Long, authorsRevision: Long): Asset {
        val input = AssetUpdate(expectedRevision = revision, authorIds = ids, newAuthorNames = names, expectedAuthorsRevision = authorsRevision)
        val key = UUID.nameUUIDFromBytes(com.google.gson.Gson().toJson(listOf("asset-authors", asset.target.instanceId, asset.id, input)).toByteArray()).toString()
        return provider.api(asset.target).updateAsset(id = asset.id, idempotencyKey = key, assetUpdate = input).bodyOrThrow().toModel(asset.target)
    }
    suspend fun updateRelations(asset: Asset, sources: List<String>, characters: List<String>): Asset {
        val target = asset.target
        val sourceChanged = sources != asset.sourceIds
        val characterChanged = characters != asset.characterIds
        // An unchanged displayed source can be implied by a role being removed.
        return provider.api(target).updateAsset(id = asset.id, assetUpdate = AssetUpdate(expectedRevision = asset.revision,
            sourceIds = sources.takeIf { sourceChanged || !characterChanged },
            characterIds = characters.takeIf { characterChanged || !sourceChanged })).bodyOrThrow().toModel(target)
    }
    suspend fun updateAcquisitionSources(asset: Asset, sources: List<String>): Asset {
        val target = asset.target
        return provider.api(target).updateAsset(id = asset.id, assetUpdate = AssetUpdate(expectedRevision = asset.revision,
            acquisitionSources = sources)).bodyOrThrow().toModel(target)
    }

    suspend fun tags(cursor: String? = null): Page<String> = provider.api(target()).listTags(cursor = cursor, limit = 50).bodyOrThrow().let { Page(it.items, it.nextCursor) }
    suspend fun createTag(name: String): String = provider.api(target()).createTag(idempotencyKey = UUID.randomUUID().toString(),
        createTagRequest = CreateTagRequest(name = name)).bodyOrThrow().name
    suspend fun removeTag(asset: Asset, tag: String): Asset {
        val target = asset.target
        return provider.api(target).removeAssetTag(id = asset.id, tag = tag, idempotencyKey = UUID.randomUUID().toString(),
            expectedRevision = asset.tagsRevision).bodyOrThrow().toModel(target)
    }

    suspend fun history(cursor: String? = null, query: AssetQuery = AssetQuery()): Page<HistoryEntry> {
        val target = target()
        val page = provider.api(target).listHistory(cursor = cursor, limit = 50, dateCounts = cursor == null,
            kind = query.kind?.let { value -> DefaultApi.KindListHistory.entries.first { it.value == value } },
            mediaType = query.mediaType?.let { value -> DefaultApi.MediaTypeListHistory.entries.first { it.value == value } },
            workKeys = query.workKeys, roleKeys = query.roleKeys,
            tzOffsetMinutes = java.util.TimeZone.getDefault().getOffset(System.currentTimeMillis()) / 60_000).bodyOrThrow()
        return Page(page.items.map { HistoryEntry(it.eventId, it.assetId, it.fileName ?: "已移除的媒体", it.kind.value,
            it.positionMs ?: 0, it.createdAt, it.asset?.toModel(target), it.contentVersion) }, page.nextCursor, page.totalMatched,
            page.dateCounts.orEmpty().map { DateCount(it.date, it.fileCount) })
    }

    suspend fun authors(cursor: String? = null, query: String? = null, kind: String? = null, sort: String = "default",
                        following: Boolean? = null, limit: Int = 50, target: Session = target()): Page<Author> {
        val page = provider.api(target).listAuthors(cursor = cursor, limit = limit, q = query, following = following,
            kind = kind?.let { value -> DefaultApi.KindListAuthors.entries.first { it.value == value } },
            sort = DefaultApi.SortListAuthors.entries.first { it.value == sort }).bodyOrThrow()
        return Page(page.items.map { it.toModel() }, page.nextCursor, page.totalMatched)
    }

    suspend fun author(id: String, target: Session = target()): Author = provider.api(target).getAuthor(id).bodyOrThrow().toModel()
    suspend fun follow(author: Author, target: Session = target()): Author {
        val input = AuthorUpdate(expectedRevision = author.revision, following = !author.followed)
        return provider.api(target).updateAuthor(id = author.id, idempotencyKey = stableAuthorKey(target, author.id, input), authorUpdate = input).bodyOrThrow().toModel()
    }
    suspend fun editAuthor(author: Author, name: String, aliases: List<String>, biography: String, homepage: String): Author {
        val target = target()
        val input = AuthorUpdate(expectedRevision = author.revision, name = name, aliases = aliases, biography = biography, homepage = homepage)
        return provider.api(target).updateAuthor(id = author.id, idempotencyKey = stableAuthorKey(target, author.id, input), authorUpdate = input).bodyOrThrow().toModel()
    }
    suspend fun createAuthor(name: String): Author = provider.api(target()).createAuthor(idempotencyKey = UUID.randomUUID().toString(),
        authorCreate = AuthorCreate(name = name)).bodyOrThrow().toModel()
    suspend fun createNamedAuthor(target: Session, names: List<String>, key: String): Author = provider.api(target).createAuthor(
        idempotencyKey = key, authorCreate = AuthorCreate(name = names.first(), aliases = names.drop(1))).bodyOrThrow().toModel()
    suspend fun renameAuthor(target: Session, author: Author, names: List<String>): Author {
        val input = AuthorUpdate(expectedRevision = author.revision, name = names.first(), aliases = names.drop(1))
        return provider.api(target).updateAuthor(id = author.id, idempotencyKey = stableAuthorKey(target, author.id, input), authorUpdate = input).bodyOrThrow().toModel()
    }
    suspend fun saveAuthorSources(target: Session, author: Author, sources: List<String>): Author {
        val input = AuthorUpdate(expectedRevision = author.revision, acquisitionSources = sources)
        return provider.api(target).updateAuthor(id = author.id, idempotencyKey = stableAuthorKey(target, author.id, input), authorUpdate = input).bodyOrThrow().toModel()
    }
    suspend fun deleteAuthor(author: Author, target: Session = target()) {
        val result = provider.api(target).deleteAuthor(id = author.id, idempotencyKey = "delete-author-${author.id}-${author.revision}", expectedRevision = author.revision)
        if (!result.isSuccessful) result.bodyOrThrow()
    }
    suspend fun mergeAuthors(source: Author, targetAuthor: Author): Pair<Author, String> {
        val result = provider.api(target()).mergeAuthors(idempotencyKey = UUID.randomUUID().toString(), authorMergeInput = AuthorMergeInput(
            sourceId = source.id, targetId = targetAuthor.id, expectedSourceRevision = source.revision, expectedTargetRevision = targetAuthor.revision)).bodyOrThrow()
        return result.author.toModel() to "已合并 ${result.affectedAssets} 件媒体的关联。${if (result.preservedConflicts.isEmpty()) "" else "目标档案保留；冲突字段：${result.preservedConflicts.joinToString()}"}"
    }

    suspend fun trash(asset: Asset) {
        provider.api(asset.target).trashAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(), expectedRevision = asset.revision).bodyOrThrow()
    }
    suspend fun trashPage(cursor: String? = null): Page<Asset> {
        val target = target()
        val page = provider.api(target).listTrash(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { it.toModel(target) }, page.nextCursor)
    }
    suspend fun restore(asset: Asset) {
        provider.api(asset.target).restoreAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(),
            revisionRequest = RevisionRequest(expectedRevision = asset.revision)).bodyOrThrow()
    }
    suspend fun purge(asset: Asset) {
        val result = provider.api(asset.target).purgeAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(), expectedRevision = asset.revision)
        if (!result.isSuccessful) result.bodyOrThrow()
    }
    suspend fun rename(asset: Asset, relativePath: String): Asset {
        val target = asset.target
        return provider.api(target).moveAsset(id = asset.id, idempotencyKey = UUID.randomUUID().toString(),
            moveRequest = MoveRequest(expectedRevision = asset.revision, relativePath = relativePath)).bodyOrThrow().toModel(target)
    }

    suspend fun reports(cursor: String? = null): Page<TaskReport> {
        val target = target()
        val page = provider.api(target).listBatches(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { it.toModel(target) }, page.nextCursor)
    }
    suspend fun authorTasks(cursor: String? = null): Page<AuthorTaskReport> {
        val page = provider.api(target()).listAuthorTasks(cursor = cursor, limit = 50).bodyOrThrow()
        return Page(page.items.map { AuthorTaskReport(it.id, it.authorId, it.state.value, it.allowedFields,
            it.candidateBlocks.orEmpty().map { block -> block.value }, it.allowClear, it.maxUpdates, it.usedUpdates, it.createdAt, it.expiresAt) }, page.nextCursor)
    }
    suspend fun cancelAuthorTask(id: String) {
        val result = provider.api(target()).cancelAuthorTask(id)
        if (!result.isSuccessful) result.bodyOrThrow()
    }

    suspend fun report(id: String, target: Session = target()): TaskReport {
        val result = provider.api(target).getBatch(id).bodyOrThrow()
        check(sameTarget(target, session.value)) { "请先连接此报告原来的服务器" }
        val received = result.items.associateBy { it.upload.id }
        val items = if (result.inputs.isNotEmpty()) result.inputs.map { input ->
            val item = input.uploadId?.let(received::get)
            TaskItem(input.id, input.originalFileName ?: "第${input.ordinal + 1}项（名称读取失败）", input.state.value,
                input.intakeFailure() ?: if (item == null) {
                    if (input.state.value == "cancelled") "此输入已取消，原始清单保留" else "此输入尚未传到服务器，仍计入原始清单"
                } else listOfNotNull(item.upload.errorMessage,
                    if (result.batch.mode.value == "ordinary") "普通上传，无需研究" else researchMessage(item.researchOutcome)).joinToString(" · "), input.assetId)
        } else result.items.map {
            TaskItem(it.upload.id, it.upload.fileName, it.upload.state.value, "已收到的文件；旧批次的原始清单不可核验", it.upload.assetId)
        }
        return result.batch.toModel(target).copy(items = items)
    }
    suspend fun cancelReport(report: TaskReport): TaskReport {
        provider.api(report.target).cancelBatch(id = report.id, revisionRequest = RevisionRequest(expectedRevision = report.revision)).bodyOrThrow()
        return this.report(report.id, report.target)
    }

    suspend fun statistics(): InventoryStats {
        val value = provider.api(target()).getStats().bodyOrThrow()
        return InventoryStats(value.assets, value.images, value.videos, value.bytes, value.favorites, value.authors, value.events, value.trash)
    }

    fun mediaClient(target: Session) = provider.client(target)
}

private fun ApiLibrary.toModel() = Library(id, name, kind.value, enabled, revision, fileCount, scanState.value, latestScanId)

private fun ApiAsset.toModel(target: Session): Asset {
    val root = target.baseUrl.toHttpUrl()
    val media = (root.resolve(contentUrl) ?: throw IOException("媒体地址无效")).newBuilder()
        .setQueryParameter("version", contentVersion.toString()).build()
    check(sameOrigin(media, root)) { "服务器返回了不同来源的媒体地址" }
    val thumbnail = thumbnailUrl?.let { (root.resolve(it) ?: throw IOException("缩略图地址无效")).newBuilder()
        .setQueryParameter("version", contentVersion.toString()).build() }
    check(thumbnail == null || sameOrigin(thumbnail, root)) { "服务器返回了不同来源的缩略图地址" }
    return Asset(id = id, libraryId = libraryId, target = target, name = fileName, originalName = originalFileName, relativePath = relativePath, mediaType = mediaType.value, version = contentVersion,
        revision = revision, tagsRevision = tagsRevision, bytes = propertySize, width = width, height = height, durationMs = durationMs,
        authorId = authorId.takeIf(String::isNotBlank), authorIds = authorIds, authorsRevision = authorsRevision, authorName = authorName, tags = tags, favorite = favorite, liked = liked,
        likeCount = likeCount, positionMs = positionMs, positionVersion = positionContentVersion, note = note,
        sourceIds = sourceIds.orEmpty(), characterIds = characterIds.orEmpty(), acquisitionSources = acquisitionSources.orEmpty(),
        collectionId = collectionId, page = page,
        contentUrl = media.toString(), thumbnailUrl = thumbnail?.toString().orEmpty(), addedAt = createdAt,
        fileDate = fileDate, workGroups = workFacets.orEmpty().map { AssetGroup(it.key, it.name) },
        roleGroups = roleFacets.orEmpty().map { AssetGroup(it.key, it.name) }, playCount = playCount, cosWorkName = cosWorkName, sourceName = source,
        favoritedAt = favoritedAt)
}

private fun ApiAuthor.toModel() = Author(id, name, kind?.value ?: "regular", aliases.orEmpty(), biography.orEmpty(), listOfNotNull(homepage), following, revision,
    fileCount, viewCount, acquisitionSources.orEmpty(), txtOrder, createdAt)
private fun ApiBatch.toModel(target: Session) = TaskReport(id, revision, mode.value, state.value,
    "${label} · ${if (manifestState.value == "sealed") "原始清单 $expectedInputs 项" else "原始清单尚未核验"} · ${if (backupState.value == "awaiting_external_backup") "等待独立备份确认" else "尚未取得备份覆盖凭据"} · 来源文件保留", createdAt, emptyList(), target)
internal fun BatchInput.intakeFailure(): String? {
    if (state.value != "failed" || uploadId != null) return null
    return when (errorCode) {
        "empty_input" -> "文件为空，未创建上传。"
        "unsupported_media" -> "不支持此文件格式，未创建上传。"
        else -> "原始输入校验失败，未创建上传。"
    }
}
private fun researchMessage(outcome: String): String? = when (outcome) {
    "", "not_run", "pending" -> "尚未执行研究"
    "unknown" -> "已研究，尚未查明"
    "candidates" -> "研究候选待确认"
    "conflict" -> "研究结果存在冲突"
    "not_needed" -> "普通上传，无需研究"
    else -> "研究状态待核对"
}
private fun stableAuthorKey(target: Session, id: String, input: AuthorUpdate) = UUID.nameUUIDFromBytes(
    com.google.gson.Gson().toJson(listOf("author-update", target.instanceId, id, input)).toByteArray()).toString()
