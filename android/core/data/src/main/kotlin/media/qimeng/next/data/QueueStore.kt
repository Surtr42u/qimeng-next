package media.qimeng.next.data

import android.content.Context
import android.util.AtomicFile
import com.google.gson.Gson
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import media.qimeng.next.model.QueueSnapshot
import java.io.File
import java.io.ByteArrayOutputStream

/** Publish after durable commit; a failed journal never masquerades as an empty queue. */
class QueueStore(context: Context) {
    private val file = AtomicFile(File(context.filesDir, "transfer-journal.json"))
    private val lock = Mutex()
    private val gson = Gson()
    private val mutableState = MutableStateFlow<QueueSnapshot?>(null)
    private val mutableError = MutableStateFlow<String?>(null)
    val state = mutableState.asStateFlow()
    val error = mutableError.asStateFlow()

    suspend fun load() = withContext(Dispatchers.IO) {
        lock.withLock {
            try {
                val data = if (file.baseFile.exists()) {
                    file.openRead().use { input ->
                        val output = ByteArrayOutputStream()
                        val buffer = ByteArray(8192)
                        while (output.size() <= 4 * 1024 * 1024) {
                            val count = input.read(buffer, 0, minOf(buffer.size, 4 * 1024 * 1024 + 1 - output.size()))
                            if (count < 0) break
                            output.write(buffer, 0, count)
                        }
                        val bytes = output.toByteArray()
                        check(bytes.size <= 4 * 1024 * 1024) { "传输记录过大" }
                        gson.fromJson(bytes.toString(Charsets.UTF_8), QueueSnapshot::class.java)
                            ?: error("传输记录为空")
                    }
                } else QueueSnapshot()
                mutableState.value = data
                mutableError.value = null
            } catch (failure: Exception) {
                mutableError.value = "传输记录读取失败，已停止发送。请重试读取。"
                throw failure
            }
        }
    }

    suspend fun update(transform: (QueueSnapshot) -> QueueSnapshot) = withContext(Dispatchers.IO) {
        lock.withLock {
            val previous = mutableState.value ?: error("传输记录尚未成功读取")
            val next = transform(previous)
            val bytes = gson.toJson(next).toByteArray()
            check(bytes.size <= 4 * 1024 * 1024) { "传输记录已满，请清理已结束的记录" }
            val stream = file.startWrite()
            try {
                stream.write(bytes)
                file.finishWrite(stream)
                mutableState.value = next
                mutableError.value = null
            } catch (failure: Exception) {
                file.failWrite(stream)
                mutableError.value = "无法保存传输状态，已暂停发送。重试前将核对服务器结果。"
                throw failure
            }
        }
    }
}
