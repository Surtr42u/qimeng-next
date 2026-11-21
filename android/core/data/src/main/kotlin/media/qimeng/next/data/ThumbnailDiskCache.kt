package media.qimeng.next.data

import coil3.disk.DiskCache
import media.qimeng.next.model.ThumbnailUsage
import okio.Buffer
import okio.ByteString.Companion.encodeUtf8
import okio.Path.Companion.toOkioPath
import java.io.File
import java.io.FileNotFoundException
import java.nio.file.Files
import java.nio.file.NoSuchFileException

private const val MiB = 1024L * 1024
internal const val MAX_THUMBNAIL_BYTES = 4 * MiB
private const val MIN_FREE_BYTES = 64 * MiB
private val entryKeyPattern = Regex("[0-9a-f]{64}:[0-9a-f]{64}")

internal fun thumbnailHash(value: String): String = value.encodeUtf8().sha256().hex()

/** 预览是已验证后原子落盘的完整 JPEG；这里只按首尾两字节复核，不复制正文。 */
internal fun validJpeg(bytes: Buffer): Boolean {
    if (bytes.size < 4) return false
    val last = bytes.size - 1
    return bytes.get(0L) == 0xff.toByte() && bytes.get(1L) == 0xd8.toByte() &&
        bytes.get(last - 1) == 0xff.toByte() && bytes.get(last) == 0xd9.toByte()
}

internal enum class ThumbnailDiskResult { READY, MISSING, FULL }

/** The two Coil LRU caches own only JPEG previews. The entry metadata binds each file to a content version. */
internal class ThumbnailDiskCache(private val directory: File, maxBytes: Long) {
    private val disk = DiskCache.Builder().directory(directory.toOkioPath()).maxSizeBytes(maxBytes).build()
    @Volatile var generation = 0L
        private set

    /**
     * Reads are deliberately not synchronized on this instance: Coil's DiskCache owns its own
     * journal lock and only exposes committed entries, and prefetch no longer shares the visible
     * flight. Holding a class monitor across a multi-megabyte file read serialized all visible
     * tiles of one pool — the same reason [usage] walks without the monitor.
     *
     * Returns a [Buffer] instead of a ByteArray so the bytes go disk → decoder without an extra
     * ByteArray round trip (the previous path copied file → ByteArray → Buffer).
     */
    fun read(key: String, version: Long): Buffer? {
        val snapshot = disk.openSnapshot(key) ?: return null
        val data = snapshot.use {
            val entry = metadata(it.metadata.toFile()) ?: return@use null
            if (entry.version != version || entry.key != key) return null
            try {
                if (dataBytes(it.data.toFile()) !in 4L..MAX_THUMBNAIL_BYTES) return@use null
                val buffer = Buffer()
                disk.fileSystem.source(it.data).use { source -> buffer.writeAll(source) }
                buffer.takeIf(::validJpeg)
            } catch (_: FileNotFoundException) {
                null
            } catch (_: NoSuchFileException) {
                null
            }
        }
        if (data == null) disk.remove(key)
        return data
    }

    /** 同 [read]：探测只读已提交条目，不占类监视器。 */
    fun warmState(key: String, version: Long, ignoreCapacity: Boolean = false): ThumbnailDiskResult {
        val current = disk.openSnapshot(key)?.use {
            val entry = metadata(it.metadata.toFile()) ?: return@use null
            if (entry.key != key || dataBytes(it.data.toFile()) !in 4L..MAX_THUMBNAIL_BYTES) return@use null
            entry.version
        }
        if (current == version) return ThumbnailDiskResult.READY
        // FULL 只表示「没有这条 + 池已满」。调用方决定是否让 Coil 的 LRU 为它淘汰最旧条目
        // （见 [store] 的 allowEviction 与 ThumbnailTurnoverPolicy）。
        if (current == null && !ignoreCapacity && atCapacity) return ThumbnailDiskResult.FULL
        return ThumbnailDiskResult.MISSING
    }

    /** 池已到配额（Coil 的 journal 统计，含全部实例的条目）。 */
    val atCapacity: Boolean get() = disk.size >= disk.maxSize - 1024

    /** 整个物理池（所有实例）的占用字节；用于判断当前实例是否只是池里的少数。 */
    val usedBytes: Long get() = disk.size

    @Synchronized
    fun store(key: String, version: Long, jpeg: Buffer, capturedGeneration: Long, background: Boolean,
              allowEviction: Boolean = false, authorized: () -> Boolean): ThumbnailDiskResult {
        if (generation != capturedGeneration || !authorized()) return ThumbnailDiskResult.MISSING
        val previous = disk.openSnapshot(key)?.use { metadata(it.metadata.toFile()) }
        if (previous != null && previous.version > version) return ThumbnailDiskResult.MISSING
        val text = "$key\n$version"
        // allowEviction：配额写满时仍落盘，由 Coil 的 LRU 淘汰最旧条目。呼叫方只在
        // 「这条马上会被看」或「池里主要是别的实例的条目」时打开它，避免超大库每轮
        // 重下被淘汰的尾部（见 ThumbnailTurnoverPolicy）。
        if (background && !allowEviction && previous == null && disk.size + jpeg.size + text.length > disk.maxSize) {
            return ThumbnailDiskResult.FULL
        }
        if (directory.usableSpace < MIN_FREE_BYTES + jpeg.size) return ThumbnailDiskResult.FULL
        val editor = disk.openEditor(key) ?: return ThumbnailDiskResult.MISSING
        try {
            disk.fileSystem.write(editor.metadata) { writeUtf8(text) }
            // clone() 只共享片段、不复制正文：调用方的 Buffer 仍可交给解码器。
            disk.fileSystem.write(editor.data) { writeAll(jpeg.clone()) }
        } catch (failure: Exception) {
            editor.abort()
            throw failure
        }
        if (!authorized()) {
            editor.abort()
            return ThumbnailDiskResult.MISSING
        }
        editor.commit()
        return warmState(key, version)
    }

    /**
     * Best-effort statistics for one instance. Deliberately unsynchronized: a multi-second walk
     * must not hold the cache monitor and stall live reads/writes (prefetch progress). Coil only
     * renames committed entries into place, so the walk never reads a partial file; a snapshot
     * taken during a concurrent store or clear can be stale until the next revision-triggered scan.
     */
    fun usage(instanceKey: String): ThumbnailUsage {
        var files = 0L
        var bytes = 0L
        scan(instanceKey) { entry, file ->
            val data = dataBytes(File(directory, file.nameWithoutExtension + ".1"))
            if (data > 0) {
                files++
                bytes += data + entry.bytes
            }
        }
        return ThumbnailUsage(files, bytes)
    }

    @Synchronized
    fun clear(instanceKey: String) {
        generation++
        scan(instanceKey) { entry, _ -> disk.remove(entry.key) }
    }

    @Synchronized
    fun shutdown() = disk.shutdown()

    private data class Entry(val key: String, val version: Long, val bytes: Long)

    private fun metadata(file: File): Entry? {
        try {
            val bytes = Files.size(file.toPath())
            if (bytes !in 131L..149L) return null
            val lines = file.readText(Charsets.UTF_8).split('\n', limit = 2)
            if (lines.size != 2 || !entryKeyPattern.matches(lines[0])) return null
            return Entry(lines[0], lines[1].toLongOrNull()?.takeIf { it > 0 } ?: return null, bytes)
        } catch (_: NoSuchFileException) {
            return null
        } catch (_: FileNotFoundException) {
            return null
        }
    }

    private fun dataBytes(file: File): Long = try {
        Files.size(file.toPath())
    } catch (_: NoSuchFileException) {
        0
    }

    private fun scan(instanceKey: String, visit: (Entry, File) -> Unit) {
        // Initialize Coil's journal and discard unfinished edits before observing its committed files.
        disk.size
        if (!directory.exists()) return
        val prefix = "$instanceKey:"
        Files.newDirectoryStream(directory.toPath(), "*.0").use { paths ->
            for (path in paths) {
                val file = path.toFile()
                val entry = metadata(file) ?: continue
                if (entry.key.startsWith(prefix)) visit(entry, file)
            }
        }
    }
}
