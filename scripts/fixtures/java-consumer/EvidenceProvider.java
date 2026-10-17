package com.fixture.compat;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.net.Uri;
import android.os.Binder;
import android.os.ParcelFileDescriptor;
import android.os.Process;
import java.io.File;
import java.io.FileNotFoundException;
import java.util.Arrays;

/** Shell can read/delete only these generated test outputs. */
public final class EvidenceProvider extends ContentProvider {
    @Override public boolean onCreate() { return true; }
    private File evidence(Uri uri) {
        int uid = Binder.getCallingUid();
        if (uid != 2000 && uid != Process.myUid()) throw new SecurityException("fixture caller");
        String name = uri.getLastPathSegment();
        if (!Arrays.asList("compat-hot-path.zip", "hot-path-counts.json", "stability-benchmark.json",
            "stability-benchmark.zip", "causal-registry.hprof").contains(name)) throw new SecurityException("fixture path");
        return new File(getContext().getExternalFilesDir(null), name);
    }
    @Override public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        if (!"r".equals(mode)) throw new SecurityException("read only");
        return ParcelFileDescriptor.open(evidence(uri), ParcelFileDescriptor.MODE_READ_ONLY);
    }
    @Override public int delete(Uri uri, String selection, String[] args) { return evidence(uri).delete() ? 1 : 0; }
    @Override public Cursor query(Uri uri, String[] columns, String selection, String[] args, String sort) { return null; }
    @Override public String getType(Uri uri) { return "application/octet-stream"; }
    @Override public Uri insert(Uri uri, ContentValues values) { throw new UnsupportedOperationException(); }
    @Override public int update(Uri uri, ContentValues values, String selection, String[] args) { throw new UnsupportedOperationException(); }
}
