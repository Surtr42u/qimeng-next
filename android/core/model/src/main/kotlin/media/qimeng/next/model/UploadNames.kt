package media.qimeng.next.model

/** System picker edits the stem. The provider's original extension stays visible and locked. */
fun lockedFileExtension(name: String): String {
    val dot = name.lastIndexOf('.')
    return if (dot > 0 && dot < name.lastIndex) name.substring(dot) else ""
}

fun UploadDraft.lockedExtension(): String = lockedFileExtension(originalName)

fun UploadDraft.withEditedStem(stem: String?): UploadDraft =
    if (stem == null) this else copy(name = stem.trim() + lockedExtension())

fun UploadDraft.validStem(stem: String): Boolean = validFileStem(stem, lockedExtension())

fun validFileStem(stem: String, extension: String): Boolean =
    stem.isNotBlank() && '/' !in stem && '\\' !in stem && stem.trim().length + extension.length <= 240
