package media.qimeng.next.model

data class LibraryScan(val id: String, val libraryId: String, val state: String, val processed: Int,
    val added: Int, val moved: Int, val changed: Int, val skipped: Int, val issueCount: Int)
