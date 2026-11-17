package media.qimeng.next.runtime

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

data class LocalFileAccessState(val granted: Boolean, val error: String? = null)
data class LocalFolderEntry(val name: String, val path: String)
data class LocalFolderPage(val currentPath: String, val rootPath: String, val parentPath: String?,
    val entries: List<LocalFolderEntry>, val nextCursor: String?)

/** Platform directory/permission capability only; registration and scanning remain normal Go API calls. */
interface LocalFiles {
    val access: StateFlow<LocalFileAccessState>
    fun refreshAccess()
    fun requestAccess()
    suspend fun folders(path: String? = null, cursor: String? = null): LocalFolderPage
}

object UnavailableLocalFiles : LocalFiles {
    override val access = MutableStateFlow(LocalFileAccessState(false))
    override fun refreshAccess() = Unit
    override fun requestAccess() = error("此安装包未包含本机文件访问模块")
    override suspend fun folders(path: String?, cursor: String?): LocalFolderPage = error("此安装包未包含本机文件访问模块")
}
