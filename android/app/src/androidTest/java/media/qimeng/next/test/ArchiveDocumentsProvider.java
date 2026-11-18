package media.qimeng.next.test;

import android.content.Intent;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.graphics.Bitmap;
import android.net.Uri;
import android.os.Bundle;
import android.os.CancellationSignal;
import android.os.ParcelFileDescriptor;
import android.provider.DocumentsContract;
import android.provider.DocumentsContract.Document;
import android.provider.DocumentsProvider;
import java.io.File;
import java.io.FileNotFoundException;
import java.io.FileOutputStream;
import java.io.IOException;
import java.util.Objects;

/** A real SAF tree in the cloud test APK, with one matched folder, one unmatched folder and one PNG. */
public final class ArchiveDocumentsProvider extends DocumentsProvider {
    private static final String AUTHORITY = "media.qimeng.next.test.archive";
    private static final String[] PROJECTION = {Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME,
        Document.COLUMN_MIME_TYPE, Document.COLUMN_SIZE, Document.COLUMN_LAST_MODIFIED, Document.COLUMN_FLAGS};
    @Override public boolean onCreate() { return true; }
    private File fixture() { return new File(Objects.requireNonNull(getContext()).getFilesDir(), "archive-input.png"); }
    @Override public Bundle call(String method, String arg, Bundle extras) {
        if (!"seedTestArchive".equals(method)) return super.call(method, arg, extras);
        Bitmap bitmap = Bitmap.createBitmap(72, 40, Bitmap.Config.ARGB_8888);
        try (FileOutputStream out = new FileOutputStream(fixture())) {
            bitmap.eraseColor(0xffcd3974);
            if (!bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)) throw new IOException("Cannot write test PNG");
        } catch (IOException failure) { throw new IllegalStateException("Cannot seed synthetic archive", failure); }
        finally { bitmap.recycle(); }
        Uri tree = DocumentsContract.buildTreeDocumentUri(AUTHORITY, "root");
        Objects.requireNonNull(getContext()).grantUriPermission("media.qimeng.next", tree,
            Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_PREFIX_URI_PERMISSION | Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION);
        Bundle result = new Bundle(); result.putString("tree", tree.toString()); return result;
    }
    @Override public Cursor queryRoots(String[] projection) {
        return new MatrixCursor(projection == null ? new String[]{DocumentsContract.Root.COLUMN_ROOT_ID} : projection);
    }
    @Override public Cursor queryDocument(String documentId, String[] projection) throws FileNotFoundException {
        MatrixCursor cursor = new MatrixCursor(projection == null ? PROJECTION : projection); add(cursor, documentId); return cursor;
    }
    @Override public Cursor queryChildDocuments(String parent, String[] projection, String sortOrder) throws FileNotFoundException {
        MatrixCursor cursor = new MatrixCursor(projection == null ? PROJECTION : projection);
        switch (parent) {
            case "root": add(cursor,"root/library"); add(cursor,"root/other"); break;
            case "root/library": add(cursor,"root/library/album"); break;
            case "root/library/album": add(cursor,"root/library/album/png"); break;
            case "root/other": break;
            default: throw new FileNotFoundException();
        }
        return cursor;
    }
    @Override public boolean isChildDocument(String parentId, String documentId) { return documentId.startsWith(parentId + "/"); }
    @Override public ParcelFileDescriptor openDocument(String id, String mode, CancellationSignal signal) throws FileNotFoundException {
        if (!"r".equals(mode) || !"root/library/album/png".equals(id)) throw new FileNotFoundException();
        return ParcelFileDescriptor.open(fixture(), ParcelFileDescriptor.MODE_READ_ONLY);
    }
    private void add(MatrixCursor cursor, String id) throws FileNotFoundException {
        String name;
        switch (id) {
            case "root": name = "云端归档"; break;
            case "root/library": name = "云端归档目标"; break;
            case "root/library/album": name = "合成子目录"; break;
            case "root/library/album/png": name = "归档合成.png"; break;
            case "root/other": name = "未登记合成目录"; break;
            default: throw new FileNotFoundException();
        }
        boolean file = id.endsWith("/png");
        MatrixCursor.RowBuilder row = cursor.newRow();
        for (String col : cursor.getColumnNames()) {
            switch (col) {
                case Document.COLUMN_DOCUMENT_ID: row.add(id); break;
                case Document.COLUMN_DISPLAY_NAME: row.add(name); break;
                case Document.COLUMN_MIME_TYPE: row.add(file ? "image/png" : Document.MIME_TYPE_DIR); break;
                case Document.COLUMN_SIZE: row.add(file ? fixture().length() : 0); break;
                case Document.COLUMN_LAST_MODIFIED: row.add(file ? fixture().lastModified() : null); break;
                case Document.COLUMN_FLAGS: row.add(0); break;
                default: row.add(null);
            }
        }
    }
}
