package media.qimeng.next.test;

import android.content.Intent;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.Bundle;
import android.os.CancellationSignal;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.DocumentsContract.Document;
import android.provider.DocumentsProvider;
import android.webkit.MimeTypeMap;
import java.io.File;
import java.io.FileNotFoundException;
import java.io.IOException;
import java.util.Locale;
import java.util.Objects;
import java.util.UUID;

/** Writable synthetic SAF tree in the instrumentation APK. Directory deletion is intentionally unsupported. */
public final class MaintenanceDocumentsProvider extends DocumentsProvider {
    private static final String AUTHORITY = "media.qimeng.next.test.maintenance";
    private static final String[] COLUMNS = {Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME, Document.COLUMN_MIME_TYPE,
        Document.COLUMN_SIZE, Document.COLUMN_LAST_MODIFIED, Document.COLUMN_FLAGS};
    private File base() { return new File(Objects.requireNonNull(getContext()).getFilesDir(), "maintenance-fixtures"); }
    @Override public boolean onCreate() { return true; }

    @Override public Bundle call(String method, String arg, Bundle extras) {
        if (!"newMaintenanceFixture".equals(method)) return super.call(method,arg,extras);
        String id = "synthetic-" + UUID.randomUUID();
        if (!new File(base(), id).mkdirs()) throw new IllegalStateException("Fixture creation failed");
        Uri tree = DocumentsContract.buildTreeDocumentUri(AUTHORITY,id);
        Objects.requireNonNull(getContext()).grantUriPermission("media.qimeng.next",tree, Intent.FLAG_GRANT_READ_URI_PERMISSION |
            Intent.FLAG_GRANT_WRITE_URI_PERMISSION | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION);
        Bundle result = new Bundle(); result.putString("tree",tree.toString()); return result;
    }
    private File file(String id) throws FileNotFoundException {
        if (id == null || !id.startsWith("synthetic-") || id.indexOf('\0') >= 0 || id.contains("\\")) throw new FileNotFoundException();
        try {
            File base = base().getCanonicalFile(), item = new File(base,id).getCanonicalFile();
            if (!item.toPath().startsWith(base.toPath()) || item.equals(base) || !item.exists()) throw new FileNotFoundException();
            return item;
        } catch (IOException problem) { throw new FileNotFoundException(); }
    }
    private String id(File item) {
        try { return base().getCanonicalFile().toPath().relativize(item.getCanonicalFile().toPath()).toString(); }
        catch (IOException problem) { throw new IllegalStateException("Invalid synthetic document ID", problem); }
    }
    @Override public Cursor queryRoots(String[] columns) { return new MatrixCursor(columns == null ? new String[]{DocumentsContract.Root.COLUMN_ROOT_ID} : columns); }
    @Override public Cursor queryDocument(String id, String[] columns) throws FileNotFoundException {
        MatrixCursor rows = new MatrixCursor(columns == null ? COLUMNS : columns); row(rows,file(id)); return rows;
    }
    @Override public Cursor queryChildDocuments(String parent, String[] columns, String order) throws FileNotFoundException {
        MatrixCursor rows = new MatrixCursor(columns == null ? COLUMNS : columns);
        File[] children = file(parent).listFiles(); if (children == null) throw new FileNotFoundException();
        for (File child : children) row(rows,child); return rows;
    }
    private void row(MatrixCursor cursor, File item) {
        MatrixCursor.RowBuilder row = cursor.newRow();
        String name = item.getName();
        String mime = MimeTypeMap.getSingleton().getMimeTypeFromExtension(name.substring(name.lastIndexOf('.') + 1).toLowerCase(Locale.ROOT));
        for (String col : cursor.getColumnNames()) switch (col) {
            case Document.COLUMN_DOCUMENT_ID: row.add(id(item)); break;
            case Document.COLUMN_DISPLAY_NAME: row.add(name); break;
            case Document.COLUMN_MIME_TYPE: row.add(item.isDirectory() ? Document.MIME_TYPE_DIR : mime == null ? "application/octet-stream" : mime); break;
            case Document.COLUMN_LAST_MODIFIED: row.add(item.lastModified()); break;
            case Document.COLUMN_SIZE: row.add(item.length()); break;
            case Document.COLUMN_FLAGS: row.add(item.isDirectory() ? Document.FLAG_SUPPORTS_RENAME | Document.FLAG_DIR_SUPPORTS_CREATE :
                Document.FLAG_SUPPORTS_WRITE | Document.FLAG_SUPPORTS_RENAME | Document.FLAG_SUPPORTS_DELETE); break;
            default: row.add(null);
        }
    }
    private void safeName(String name) throws FileNotFoundException {
        if (name.isEmpty() || name.equals(".") || name.equals("..") || name.contains("/") || name.contains("\\")) throw new FileNotFoundException();
    }
    @Override public String createDocument(String parent, String type, String name) throws FileNotFoundException {
        safeName(name); File out = new File(file(parent),name);
        try { if (!(Document.MIME_TYPE_DIR.equals(type) ? out.mkdir() : out.createNewFile())) throw new FileNotFoundException(); }
        catch (IOException failure) { throw new FileNotFoundException(); }
        return id(out);
    }
    @Override public String renameDocument(String sourceId, String name) throws FileNotFoundException {
        safeName(name); File source = file(sourceId), out = new File(source.getParentFile(),name);
        if (out.exists() || !source.renameTo(out)) throw new FileNotFoundException(); return id(out);
    }
    @Override public void deleteDocument(String docId) throws FileNotFoundException {
        File source = file(docId); if (source.isDirectory() || !source.delete()) throw new FileNotFoundException();
    }
    @Override public boolean isChildDocument(String parent, String child) { return child.startsWith(parent + "/"); }
    @Override public ParcelFileDescriptor openDocument(String id, String mode, CancellationSignal signal) throws FileNotFoundException {
        return ParcelFileDescriptor.open(file(id),ParcelFileDescriptor.parseMode(mode));
    }
}
