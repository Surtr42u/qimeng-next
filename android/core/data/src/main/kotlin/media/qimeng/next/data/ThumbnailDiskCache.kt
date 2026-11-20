package media.qimeng.next.data

import coil3.disk.DiskCache
import media.qimeng.next.model.ThumbnailUsage
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

internal fun validJpeg(bytes: ByteArray): Boolean = bytes.size >= 4 &&
    bytes[0] == 0xff.toByte() && bytes[1] == 0xd8.toByte() &&
    bytes[bytes.lastIndex - 1] == 0xff.toByte() && bytes.last() == 0xd9.toByte()

internal enum class ThumbnailDiskResult { READY, MISSING, FULL }

/** The two Coil LRU caches own only JPEG previews. The entry metadata binds each file to a content version. */
internal class ThumbnailDiskCache(private val directory: File, maxBytes: Long) {
    private val disk = DiskCache.Builder().directory(directory.toOkioPath()).maxSizeBytes(maxBytes).build()
    @Volatile var generation = 0L
        private set

    @Synchronized
    fun read(key: String, version: Long): ByteArray? {
        val snapshot = disk.openSnapshot(key) ?: return null
        val data = snapshot.use {
            val entry = metadata(it.metadata.toFile()) ?: return@use null
            if (entry.version != version || entry.key != key) return null
            try {
                if (Files.size(it.data.toFile().toPath()) !in 4L..MAX_THUMBNAIL_BYTES) return@use null
                it.data.toFile().readBytes().takeIf(::validJpeg)
            } catch (_: FileNotFoundException) {
                null
            } catch (_: NoSuchFileException) {
                null
            }
        }
        if (data == null) disk.remove(key)
        return data
    }

    @Synchronized
    fun warmState(key: String, version: Long): ThumbnailDiskResult {
        val current = disk.openSnapshot(key)?.use {
            val entry = metadata(it.metadata.toFile()) ?: return@use null
            if (entry.key != key || dataBytes(it.data.toFile()) !in 4L..MAX_THUMBNAIL_BYTES) return@use null
            entry.version
        }
        if (current == version) return ThumbnailDiskResult.READY
        if (current == null && disk.size >= disk.maxSize - 1024) return ThumbnailDiskResult.FULL
        return ThumbnailDiskResult.MISSING
    }

    @Synchronized
    fun store(key: String, version: Long, jpeg: ByteArray, capturedGeneration: Long, background: Boolean,
              authorized: () -> Boolean): ThumbnailDiskResult {
        if (generation != capturedGeneration || !authorized()) return ThumbnailDiskResult.MISSING
        val previous = disk.openSnapshot(key)?.use { metadata(it.metadata.toFile()) }
        if (previous != null && previous.version > version) return ThumbnailDiskResult.MISSING
        val text = "$key\n$version"
        if (background && previous == null && disk.size + jpeg.size + text.length > disk.maxSize) {
            return ThumbnailDiskResult.FULL
        }
        if (directory.usableSpace < MIN_FREE_BYTES + jpeg.size) return ThumbnailDiskResult.FULL
        val editor = disk.openEditor(key) ?: return ThumbnailDiskResult.MISSING
        try {
            disk.fileSystem.write(editor.metadata) { writeUtf8(text) }
            disk.fileSystem.write(editor.data) { write(jpeg) }
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
