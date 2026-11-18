package media.qimeng.next.test;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.Binder;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;

import java.io.File;
import java.io.FileNotFoundException;
import java.util.Objects;

/** Android may start this test APK outside instrumentation, with no target APK class loader. */
public final class FixtureProvider extends ContentProvider {
    @Override public boolean onCreate() { return true; }

    @Override public Bundle call(String method, String arg, Bundle extras) {
        if (!"seedTestArchive".equals(method) && !"newMaintenanceFixture".equals(method)) return super.call(method, arg, extras);
        // The fixture APK owns the protected DocumentsProviders. It grants one tree to the target,
        // like a picker would, before the target makes any calls to either DocumentsProvider.
        long identity = Binder.clearCallingIdentity();
        try {
            android.content.Context owner = Objects.requireNonNull(getContext());
            return "seedTestArchive".equals(method) ? ArchiveDocumentsProvider.grantFixture(owner) : MaintenanceDocumentsProvider.grantFixture(owner);
        } finally {
            Binder.restoreCallingIdentity(identity);
        }
    }

    private File file(Uri uri) {
        String name = uri.getLastPathSegment();
        if (!"android-synthetic.png".equals(name) && !"android-synthetic.mp4".equals(name)) {
            throw new IllegalArgumentException("Unknown synthetic input");
        }
        return new File(Objects.requireNonNull(getContext()).getFilesDir(), "fixture-" + name);
    }

    @Override public String getType(Uri uri) {
        return "android-synthetic.mp4".equals(uri.getLastPathSegment()) ? "video/mp4" : null;
    }

    @Override public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        return ParcelFileDescriptor.open(file(uri), ParcelFileDescriptor.parseMode(mode));
    }

    @Override public Cursor query(Uri uri, String[] projection, String selection, String[] args, String sortOrder) {
        String[] columns = projection == null ? new String[]{OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE} : projection;
        File selected = file(uri);
        MatrixCursor result = new MatrixCursor(columns);
        MatrixCursor.RowBuilder row = result.newRow();
        for (String column : columns) {
            if (OpenableColumns.DISPLAY_NAME.equals(column)) row.add(uri.getLastPathSegment());
            else if (OpenableColumns.SIZE.equals(column)) row.add(selected.length());
            else row.add(null);
        }
        return result;
    }

    @Override public Uri insert(Uri uri, ContentValues values) { throw new UnsupportedOperationException(); }
    @Override public int delete(Uri uri, String selection, String[] args) { throw new UnsupportedOperationException(); }
    @Override public int update(Uri uri, ContentValues values, String selection, String[] args) { throw new UnsupportedOperationException(); }
}
