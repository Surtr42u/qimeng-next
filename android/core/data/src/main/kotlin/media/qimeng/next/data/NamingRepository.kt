package media.qimeng.next.data

import media.qimeng.next.model.*
import media.qimeng.next.sdk.api.DefaultApi
import media.qimeng.next.sdk.model.NamingInputItem
import media.qimeng.next.sdk.model.NamingPreviewInput

/** Namespace rules and every number remain on the server, including advisory previews. */
class NamingRepository internal constructor(private val settings: SecureSettings, private val provider: ApiProvider) {
    suspend fun inspect(request: NamingRequest): NamingInspection {
        val api = provider.api(settings.session.value ?: error("请先登录服务器"))
        val suggestions = api.suggestNames(id = request.libraryId, directory = request.directory, q = request.family,
            mediaType = DefaultApi.MediaTypeSuggestNames.entries.first { it.value == request.numberClass }).bodyOrThrow()
        val matches = suggestions.items.map { NamingSuggestion(it.family, it.suggestedName) }
        if (request.family.isBlank() && request.collectionId == null) return NamingInspection(matches, suggestions.truncated, null, null)
        val context = api.getNamingContext(id = request.libraryId, directory = request.directory, family = request.family,
            collectionId = request.collectionId, mediaType = DefaultApi.MediaTypeGetNamingContext.entries.first { it.value == request.numberClass }).bodyOrThrow()
        val position = NamingPosition(context.family, context.directory, context.nextWorkNumber, context.currentWorkNumber,
            context.nextPage, context.nextChapter, context.numberClass.value)
        val preview = if (request.files.isEmpty()) null else {
            val intent = when {
                request.collectionId == null -> "new_work"
                context.nextChapter != null -> "append_drama"
                context.nextPage != null -> "append_pages"
                else -> error("服务器没有返回集合的续号上下文")
            }
            val result = try {
                api.previewNames(id = request.libraryId, namingPreviewInput = NamingPreviewInput(
                    intent = NamingPreviewInput.Intent.entries.first { it.value == intent }, directory = request.directory,
                    family = request.family.takeIf(String::isNotBlank), collectionId = request.collectionId,
                    items = request.files.map { NamingInputItem(fileName = it.name, note = request.note) })).bodyOrThrow()
            } catch (failure: ApiFailure) {
                if (failure.code != "unsupported_media") throw failure
                return NamingInspection(matches, suggestions.truncated, position, null, unsupportedInputs = true)
            }
            NamingPreview(result.items.map { it.relativePath }, result.reserved)
        }
        return NamingInspection(matches, suggestions.truncated, position, preview)
    }
}
