package com.fixture.compat

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri
import android.os.Binder
import android.os.ParcelFileDescriptor
import android.os.Process
import java.io.File

/** Test-only shell read port for scoped-storage devices. Never exposes arbitrary app files. */
class EvidenceProvider : ContentProvider() {
    override fun onCreate() = true
    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        check(Binder.getCallingUid() == 2000 || Binder.getCallingUid() == Process.myUid())
        check(mode == "r")
        val name = uri.lastPathSegment
        check(name in setOf("compat-hot-path.zip", "hot-path-counts.json",
            "stability-benchmark.json", "stability-benchmark.zip", "causal-registry.hprof"))
        val file = File(checkNotNull(context!!.getExternalFilesDir(null)), checkNotNull(name))
        check(file.isFile && file.length() > 0L)
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY)
    }
    override fun query(uri: Uri, projection: Array<out String>?, selection: String?, args: Array<out String>?, order: String?): Cursor? = null
    override fun getType(uri: Uri) = "application/octet-stream"
    override fun insert(uri: Uri, values: ContentValues?): Uri? = null
    override fun update(uri: Uri, values: ContentValues?, selection: String?, args: Array<out String>?) = 0
    override fun delete(uri: Uri, selection: String?, args: Array<out String>?): Int {
        check(Binder.getCallingUid() == 2000 || Binder.getCallingUid() == Process.myUid())
        val name = uri.lastPathSegment
        check(name in setOf("compat-hot-path.zip", "hot-path-counts.json",
            "stability-benchmark.json", "stability-benchmark.zip", "causal-registry.hprof"))
        val file = File(checkNotNull(context!!.getExternalFilesDir(null)), checkNotNull(name))
        return if (!file.exists()) 0 else { check(file.delete()); 1 }
    }
}
