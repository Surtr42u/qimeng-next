package media.qimeng.next.embedded

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Environment
import androidx.core.content.ContextCompat
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import media.qimeng.next.runtime.LocalFileAccessState
import media.qimeng.next.runtime.LocalFiles
import media.qimeng.next.runtime.LocalFolderEntry
import media.qimeng.next.runtime.LocalFolderPage
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.nio.file.Files
import java.nio.file.LinkOption
import java.util.Locale
import java.util.PriorityQueue
import kotlin.coroutines.coroutineContext

internal object FileAccessState {
    val value = MutableStateFlow(LocalFileAccessState(false))
    fun granted(context: Context): Boolean = if (Build.VERSION.SDK_INT >= 30) Environment.isExternalStorageManager()
        else listOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE).all {
            ContextCompat.checkSelfPermission(context, it) == PackageManager.PERMISSION_GRANTED
        }
    fun refresh(context: Context) { value.value = LocalFileAccessState(granted(context)) }
    fun unavailable() { value.value = value.value.copy(error = "系统未提供文件授权入口，请在应用设置中允许访问后重试") }
}

class EmbeddedFileAccess(context: Context) : LocalFiles {
    private val app = context.applicationContext
    override val access = FileAccessState.value.asStateFlow()
    init { refreshAccess() }
    override fun refreshAccess() = FileAccessState.refresh(app)
    override fun requestAccess() {
        app.startActivity(Intent(app, EmbeddedFileAccessActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    override suspend fun folders(path: String?, cursor: String?): LocalFolderPage = withContext(Dispatchers.IO) {
        refreshAccess()
        check(access.value.granted) { "需要“所有文件访问”权限，才能浏览本机媒体目录" }
        val root = primaryRoot()
        val requested = if (path == null) root else File(path).absoluteFile
        val directory = requested.canonicalFile
        require(directory == root || directory.toPath().startsWith(root.toPath())) { "目录必须位于已授权的主存储中" }
        require(requested == directory) { "不能使用链接或非规范路径浏览目录" }
        check(directory.isDirectory) { "目录不可用，请核对权限和存储状态" }
        val ordering = compareBy<LocalFolderEntry> { it.name.lowercase(Locale.ROOT) }.thenBy { it.name }
        val after = cursor?.let { LocalFolderEntry(it, "") }
        val entries = PriorityQueue(201, ordering.reversed())
        try {
            Files.newDirectoryStream(directory.toPath()).use { stream ->
                for (child in stream) {
                    coroutineContext.ensureActive()
                    if (!Files.isDirectory(child, LinkOption.NOFOLLOW_LINKS) || Files.isSymbolicLink(child)) continue
                    val item = LocalFolderEntry(child.fileName.toString(), child.toAbsolutePath().toString())
                    if (after != null && ordering.compare(item, after) <= 0) continue
                    entries.add(item)
                    if (entries.size > 201) entries.poll()
                }
            }
        } catch (_: IOException) { throw IllegalStateException("读取目录失败，请核对权限后重试") }
        catch (_: SecurityException) { throw IllegalStateException("系统已撤回目录访问权限，请重新授权") }
        val page = entries.sortedWith(ordering)
        val visible = page.take(200)
        LocalFolderPage(directory.absolutePath, root.absolutePath, directory.parentFile?.takeUnless { directory == root }?.absolutePath,
            visible, visible.lastOrNull()?.name?.takeIf { page.size > 200 })
    }

    internal companion object {
        @Suppress("DEPRECATION")
        fun primaryRoot(): File = Environment.getExternalStorageDirectory().canonicalFile

        fun roots(context: Context, privateMedia: File): String = JSONObject().apply {
            put("local", privateMedia.canonicalPath)
            if (FileAccessState.granted(context)) put("phone", primaryRoot().absolutePath)
        }.toString()
    }
}
