package media.qimeng.next.data

import android.content.ContentValues
import android.content.ContentUris
import android.content.Context
import android.content.pm.ApplicationInfo
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import java.security.MessageDigest

internal data class SourceResult(val outcome: String, val code: String, val receipt: String? = null)
internal data class MediaBinding(val uri: Uri, val generation: Long, val fileIdentity: String = "")

/** Only Android's indexed, generation-addressable system trash qualifies. Generic SAF deletion is never used. */
internal class ArchiveMediaTrash(private val context: Context) {
    private data class Row(val id: Long, val volume: String, val bytes: Long, val generation: Long, val trashed: Boolean)

    fun identify(document: Uri): MediaBinding? {
        if (Build.VERSION.SDK_INT < 30) return null
        val authority = document.authority
        if (authority !in setOf("com.android.externalstorage.documents", "com.android.providers.media.documents") ||
            authority == null || context.packageManager.resolveContentProvider(authority, 0)?.applicationInfo?.flags?.and(ApplicationInfo.FLAG_SYSTEM) != ApplicationInfo.FLAG_SYSTEM)
            return null
        val media = try { MediaStore.getMediaUri(context, document) } catch (_: IllegalArgumentException) { null }
            ?: return null
        return identifyMedia(media)
    }

    internal fun identifyMedia(media: Uri): MediaBinding? {
        if (Build.VERSION.SDK_INT < 30) return null
        if (media.scheme != "content" || media.authority != MediaStore.AUTHORITY) return null
        val mapped = row(media) ?: return null
        if (mapped.volume == MediaStore.VOLUME_EXTERNAL || mapped.volume.isBlank()) return null
        val exact = ContentUris.withAppendedId(MediaStore.Files.getContentUri(mapped.volume), mapped.id)
        val before = row(exact)?.takeIf { it == mapped && !it.trashed && it.generation > 0 } ?: return null
        val identity = context.contentResolver.openFileDescriptor(exact, "r")?.use { fd -> identity(fd.fileDescriptor) } ?: return null
        return MediaBinding(exact, before.generation, identity)
    }

    suspend fun inspect(document: Uri, bytes: Long, hash: String, original: MediaBinding?): Pair<MediaBinding?, SourceResult?> {
        if (original == null) return null to SourceResult("unavailable", "provider_no_trash")
        val binding = identify(document) ?: return null to SourceResult("unavailable", "source_unavailable")
        if (binding != original || !matches(binding, bytes, hash) || identify(document) != original)
            return null to SourceResult("source_changed", "source_changed")
        return binding to null
    }
    internal suspend fun matches(binding: MediaBinding, bytes: Long, hash: String): Boolean =
        identifyMedia(binding.uri) == binding && row(binding.uri)?.bytes == bytes && digest(binding.uri) == hash && identifyMedia(binding.uri) == binding

    fun move(binding: MediaBinding, size: Long): Boolean {
        check(Build.VERSION.SDK_INT >= 30)
        val values = ContentValues().apply { put(MediaStore.MediaColumns.IS_TRASHED, 1) }
        // A rename/replacement after the digest check cannot match the generation just observed.
        return context.contentResolver.openFileDescriptor(binding.uri, "r")?.use { held ->
            if (identity(held.fileDescriptor) != binding.fileIdentity) return@use false
            context.contentResolver.update(binding.uri, values,
                "${MediaStore.MediaColumns.GENERATION_MODIFIED}=? AND ${MediaStore.MediaColumns.SIZE}=? AND ${MediaStore.MediaColumns.IS_TRASHED}=0",
                arrayOf(binding.generation.toString(), size.toString())) == 1
        } == true
    }

    suspend fun observe(binding: MediaBinding, size: Long, hash: String, permit: String, restoreChanged: Boolean = false): SourceResult {
        if (Build.VERSION.SDK_INT < 30) return SourceResult("unavailable", "provider_no_trash")
        val current = row(binding.uri) ?: return SourceResult("unavailable", "recycle_unconfirmed")
        if (!current.trashed) return SourceResult("retained", "recycle_not_observed")
        val objectKey = context.contentResolver.openFileDescriptor(binding.uri, "r")?.use { identity(it.fileDescriptor) }?.substringBeforeLast(':')?.substringBeforeLast(':')
        val originalKey = binding.fileIdentity.substringBeforeLast(':').substringBeforeLast(':')
        if (objectKey != originalKey || current.bytes != size || digest(binding.uri) != hash || row(binding.uri) != current) {
            if (restoreChanged) {
                val values = ContentValues().apply { put(MediaStore.MediaColumns.IS_TRASHED, 0) }
                val restored = context.contentResolver.update(binding.uri, values,
                    "${MediaStore.MediaColumns.GENERATION_MODIFIED}=? AND ${MediaStore.MediaColumns.IS_TRASHED}=1", arrayOf(current.generation.toString()))
                if (restored != 1 || row(binding.uri)?.trashed != false) return SourceResult("unavailable", "recycle_unconfirmed")
            }
            return SourceResult("source_changed", "source_changed")
        }
        val raw = "$permit\u0000${binding.uri}\u0000${binding.generation}\u0000${current.generation}"
        val receipt = MessageDigest.getInstance("SHA-256").digest(raw.toByteArray()).joinToString("") { "%02x".format(it) }
        return SourceResult("recycled", "android_media_trash", receipt)
    }

    private fun identity(fd: java.io.FileDescriptor): String? {
        if (Build.VERSION.SDK_INT < 27) return null
        val info = android.system.Os.fstat(fd)
        if (info.st_ino == 0L || !android.system.OsConstants.S_ISREG(info.st_mode)) return null
        return "${info.st_dev}:${info.st_ino}:${info.st_ctim.tv_sec}:${info.st_ctim.tv_nsec}"
    }

    private fun row(uri: Uri): Row? {
        check(Build.VERSION.SDK_INT >= 30)
        val options = Bundle().apply { putInt(MediaStore.QUERY_ARG_MATCH_TRASHED, MediaStore.MATCH_INCLUDE) }
        val numericId = uri.lastPathSegment?.toLongOrNull()?.takeIf { it > 0 } ?: return null
        return context.contentResolver.query(uri, arrayOf(android.provider.BaseColumns._ID, MediaStore.MediaColumns.VOLUME_NAME,
            MediaStore.MediaColumns.SIZE, MediaStore.MediaColumns.GENERATION_MODIFIED, MediaStore.MediaColumns.IS_TRASHED), options, null)?.use {
            if (it.count != 1 || !it.moveToFirst() || it.getLong(0) != numericId || it.isNull(1) || it.isNull(2) || it.isNull(3)) null
            else Row(it.getLong(0), it.getString(1), it.getLong(2), it.getLong(3), it.getInt(4) != 0)
        }
    }
    private suspend fun digest(uri: Uri): String {
        val digest = MessageDigest.getInstance("SHA-256")
        context.contentResolver.openInputStream(uri)?.use { stream ->
            val buffer = ByteArray(128 * 1024)
            while (true) {
                currentCoroutineContext().ensureActive()
                val n = stream.read(buffer)
                if (n < 0) break
                digest.update(buffer, 0, n)
            }
        } ?: throw java.io.IOException("无法重新读取来源")
        return digest.digest().joinToString("") { "%02x".format(it) }
    }
}
