package media.qimeng.next.model

/** System picker edits the stem. The provider's original extension stays visible and locked. */
fun UploadDraft.lockedExtension(): String {
    val dot = originalName.lastIndexOf('.')
    return if (dot > 0 && dot < originalName.lastIndex) originalName.substring(dot) else ""
}

fun UploadDraft.withEditedStem(stem: String?): UploadDraft =
    if (stem == null) this else copy(name = stem.trim() + lockedExtension())

fun UploadDraft.validStem(stem: String): Boolean =
    stem.isNotBlank() && '/' !in stem && '\\' !in stem && stem.trim().length + lockedExtension().length <= 240
