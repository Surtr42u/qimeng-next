package media.qimeng.next.data

import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import media.qimeng.next.model.*
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.util.UUID

/** Private device intake manifest. Only Go owns media, library routing, file naming and protection decisions. */
internal class ArchiveStore(context: Context) : SQLiteOpenHelper(context, "archive-intake.db", null, 1) {
    override fun onConfigure(db: SQLiteDatabase) { db.setForeignKeyConstraintsEnabled(true) }
    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL("CREATE TABLE plans (id TEXT PRIMARY KEY, tree TEXT NOT NULL, base TEXT NOT NULL, instance TEXT NOT NULL, label TEXT NOT NULL, state TEXT NOT NULL, local INTEGER NOT NULL, message TEXT, manifest TEXT)")
        db.execSQL("CREATE TABLE folders (plan TEXT NOT NULL REFERENCES plans ON DELETE CASCADE, uri TEXT NOT NULL, library TEXT NOT NULL, directory TEXT NOT NULL, scanned INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(plan,uri))")
        db.execSQL("CREATE INDEX pending_folders ON folders(plan,scanned)")
        db.execSQL("CREATE TABLE excluded (plan TEXT NOT NULL REFERENCES plans ON DELETE CASCADE, name TEXT NOT NULL, reason TEXT NOT NULL)")
        db.execSQL("CREATE TABLE batches (id TEXT PRIMARY KEY, plan TEXT NOT NULL REFERENCES plans ON DELETE CASCADE, library TEXT NOT NULL, directory TEXT NOT NULL, count INTEGER NOT NULL DEFAULT 0, queued INTEGER NOT NULL DEFAULT 0, protection TEXT)")
        db.execSQL("CREATE INDEX pending_batches ON batches(plan,queued)")
        db.execSQL("CREATE TABLE inputs (id TEXT PRIMARY KEY, batch TEXT NOT NULL REFERENCES batches ON DELETE CASCADE, uri TEXT NOT NULL, name TEXT NOT NULL, size INTEGER, modified INTEGER, original_media TEXT, original_generation INTEGER, file_identity TEXT, stage TEXT NOT NULL DEFAULT 'pending', permit TEXT, media TEXT, generation INTEGER, outcome TEXT, code TEXT, receipt TEXT, UNIQUE(batch,uri))")
        db.execSQL("CREATE INDEX batch_inputs ON inputs(batch)")
    }
    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = error("归档清单版本不受支持")
    fun start(target: Session, tree: String, local: Boolean): String {
        val id = UUID.randomUUID().toString()
        writableDatabase.execSQL("INSERT INTO plans(id,tree,base,instance,label,state,local) VALUES(?,?,?,?,?,'scanning',?)", arrayOf(id, tree, target.baseUrl, target.instanceId, target.label, if (local) 1 else 0))
        return id
    }
    fun exclude(plan: String, name: String, reason: String) = writableDatabase.execSQL("INSERT INTO excluded VALUES(?,?,?)", arrayOf(plan, name, reason))
    fun folder(plan: String, uri: String, library: String, directory: String) {
        writableDatabase.execSQL("INSERT OR IGNORE INTO folders(plan,uri,library,directory) VALUES(?,?,?,?)", arrayOf(plan, uri, library, directory))
    }
    data class Folder(val uri: String, val library: String, val directory: String)
    fun nextFolder(plan: String): Folder? = readableDatabase.rawQuery("SELECT uri,library,directory FROM folders WHERE plan=? AND scanned=0 ORDER BY rowid LIMIT 1", arrayOf(plan))
        .use { if (it.moveToFirst()) Folder(it.getString(0), it.getString(1), it.getString(2)) else null }
    fun scannedFolder(plan: String, uri: String) = writableDatabase.execSQL("UPDATE folders SET scanned=1 WHERE plan=? AND uri=?", arrayOf(plan,uri))
    fun inputs(plan: String, folder: Folder, files: List<ArchiveInput>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            val batch = "archive-${UUID.randomUUID()}"
            db.execSQL("INSERT INTO batches(id,plan,library,directory,count) VALUES(?,?,?,?,?)", arrayOf(batch, plan, folder.library, folder.directory, files.size))
            files.forEach { file -> db.execSQL("INSERT INTO inputs(id,batch,uri,name,size,modified,original_media,original_generation,file_identity) VALUES(?,?,?,?,?,?,?,?,?)",
                arrayOf(file.id, batch, file.uri, file.name, file.size, file.modified, file.mediaUri, file.mediaGeneration, file.fileIdentity)) }
            db.setTransactionSuccessful()
        } finally { db.endTransaction() }
    }
    fun state(id: String, state: String, message: String? = null) = writableDatabase.execSQL("UPDATE plans SET state=?,message=? WHERE id=?", arrayOf(state, message, id))
    fun sealPreview(id: String): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val length = ByteBuffer.allocate(Int.SIZE_BYTES)
        readableDatabase.rawQuery("""SELECT b.library,b.directory,i.uri,i.name,i.size,i.modified,i.original_media,i.original_generation,i.file_identity
            FROM batches b JOIN inputs i ON i.batch=b.id WHERE b.plan=? ORDER BY b.library,b.directory,i.uri""", arrayOf(id)).use { rows ->
            while (rows.moveToNext()) for (column in 0 until rows.columnCount) {
                val value = rows.getString(column)?.toByteArray(Charsets.UTF_8)
                length.clear()
                digest.update(length.putInt(value?.size ?: -1).array())
                value?.let { digest.update(it) }
            }
        }
        val hash = digest.digest().joinToString("") { "%02x".format(it) }
        writableDatabase.execSQL("UPDATE plans SET state='preview',manifest=? WHERE id=?", arrayOf(hash, id))
        return hash
    }
    fun sameManifest(id: String, manifest: String): Boolean =
        readableDatabase.rawQuery("SELECT 1 FROM plans WHERE id=? AND manifest=?", arrayOf(id, manifest)).use { it.moveToFirst() }
    fun discardPreview(plan: String) = writableDatabase.execSQL("DELETE FROM plans WHERE id=? AND state IN ('scanning','preview','failed')", arrayOf(plan))
    fun latest(target: Session): String? = readableDatabase.rawQuery("SELECT id FROM plans WHERE base=? AND instance=? ORDER BY rowid DESC LIMIT 1", arrayOf(target.baseUrl,target.instanceId))
        .use { if (it.moveToFirst()) it.getString(0) else null }
    fun overview(id: String): ArchiveOverview? {
        val db = readableDatabase
        return db.rawQuery("""SELECT p.id,p.base,p.instance,p.label,p.tree,p.state,p.message,
            (SELECT coalesce(sum(count),0) FROM batches WHERE plan=p.id),
            (SELECT coalesce(sum(count),0) FROM batches WHERE plan=p.id AND queued=1),
            (SELECT count(*) FROM excluded WHERE plan=p.id) FROM plans p WHERE p.id=?""", arrayOf(id)).use { c ->
            if (!c.moveToFirst()) null else ArchiveOverview(c.getString(0), Session(c.getString(1), c.getString(2), c.getString(3)), c.getString(4),
                c.getString(5), c.getLong(7), c.getLong(8), c.getLong(9), db.rawQuery("SELECT name FROM excluded WHERE plan=? ORDER BY rowid LIMIT 20", arrayOf(id))
                    .use { names -> buildList { while (names.moveToNext()) add(names.getString(0)) } }, c.getString(6))
        }
    }
    fun nextGroup(plan: String): ArchiveGroup? = readableDatabase.rawQuery("SELECT id,library,directory FROM batches WHERE plan=? AND queued=0 ORDER BY rowid LIMIT 1",arrayOf(plan)).use { c ->
        if (!c.moveToFirst()) null else {
            val id = c.getString(0)
            val inputs = readableDatabase.rawQuery("SELECT id,uri,name,size,modified FROM inputs WHERE batch=? ORDER BY rowid", arrayOf(id)).use { rows ->
                buildList { while (rows.moveToNext()) add(ArchiveInput(rows.getString(0), rows.getString(1), rows.getString(2),
                    rows.getLong(3).takeUnless { rows.isNull(3) }, rows.getLong(4).takeUnless { rows.isNull(4) })) }
            }
            ArchiveGroup(id, plan, c.getString(1), c.getString(2), inputs)
        }
    }
    fun queued(batch: String) = writableDatabase.execSQL("UPDATE batches SET queued=1 WHERE id=?", arrayOf(batch))

    data class Cleanup(val uri: String, val local: Boolean, val stage: String, val protection: String?, val permit: String?,
        val media: String?, val generation: Long?, val outcome: String?, val code: String?, val receipt: String?, val original: MediaBinding?)
    fun cleanup(id: String): Cleanup? = readableDatabase.rawQuery("""SELECT i.uri,p.local,i.stage,b.protection,i.permit,i.media,i.generation,i.outcome,i.code,i.receipt,i.original_media,i.original_generation,i.file_identity
        FROM inputs i JOIN batches b ON b.id=i.batch JOIN plans p ON p.id=b.plan WHERE i.id=?""", arrayOf(id)).use { c ->
        if (!c.moveToFirst()) null else Cleanup(c.getString(0), c.getInt(1) != 0, c.getString(2), c.getString(3), c.getString(4),
            c.getString(5), c.getLong(6).takeUnless { c.isNull(6) }, c.getString(7), c.getString(8), c.getString(9),
            if (c.isNull(10) || c.isNull(11) || c.isNull(12)) null else MediaBinding(android.net.Uri.parse(c.getString(10)), c.getLong(11), c.getString(12)))
    }
    fun protection(batch: String, protection: String) = writableDatabase.execSQL("UPDATE batches SET protection=? WHERE id=?", arrayOf(protection, batch))
    fun permit(id: String, permit: String) = writableDatabase.execSQL("UPDATE inputs SET permit=? WHERE id=?", arrayOf(permit, id))
    fun arm(id: String, media: String, generation: Long) = writableDatabase.execSQL(
        "UPDATE inputs SET stage='armed',media=?,generation=? WHERE id=? AND stage='pending'", arrayOf(media, generation, id))
    fun observe(id: String, outcome: String, code: String, receipt: String?) = writableDatabase.execSQL(
        "UPDATE inputs SET stage='observed',outcome=?,code=?,receipt=? WHERE id=?", arrayOf(outcome, code, receipt, id))
    fun finish(id: String, code: String) = writableDatabase.execSQL("UPDATE inputs SET stage='done',code=? WHERE id=?", arrayOf(code, id))
    fun block(id: String, code: String) = writableDatabase.execSQL("UPDATE inputs SET stage='blocked',code=? WHERE id=? AND stage='pending'", arrayOf(code, id))
    fun retry(id: String) = writableDatabase.execSQL("UPDATE inputs SET stage='pending',code=NULL WHERE id=? AND stage='blocked'", arrayOf(id))
    fun needsGrant(tree: String): Boolean = readableDatabase.rawQuery("""SELECT EXISTS(
        SELECT 1 FROM plans p JOIN batches b ON b.plan=p.id JOIN inputs i ON i.batch=b.id
        WHERE p.tree=? AND p.state IN ('confirmed','preparing','queued') AND i.stage NOT IN ('done','blocked'))""", arrayOf(tree))
        .use { it.moveToFirst(); it.getInt(0) != 0 }
    fun prune(ids: Set<String>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            ids.forEach { id -> db.execSQL("DELETE FROM inputs WHERE id=? AND stage IN ('done','blocked') AND batch IN (SELECT id FROM batches WHERE queued=1)", arrayOf(id)) }
            db.execSQL("""DELETE FROM plans WHERE state='queued' AND rowid NOT IN (SELECT max(rowid) FROM plans GROUP BY base,instance)
                AND NOT EXISTS (SELECT 1 FROM batches b JOIN inputs i ON i.batch=b.id WHERE b.plan=plans.id)""")
            db.setTransactionSuccessful()
        } finally { db.endTransaction() }
    }
}
