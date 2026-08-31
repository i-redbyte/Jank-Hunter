package io.jankhunter.gradle

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.Opcodes

class AndroidComponentCatalogTest {
    @Test
    fun serviceCatalogSeparatesDiscoveredAndInstrumentedCallbacks() {
        val builder = AndroidComponentCatalogClassBuilder(
            "example/SyncService",
            setOf("example/SyncService", "android/app/Service"),
        )
        builder.recordClass(Opcodes.ACC_PUBLIC)
        builder.recordMethod(Opcodes.ACC_PUBLIC, "onCreate", "()V")
        builder.recordMethod(
            Opcodes.ACC_PUBLIC,
            "onStartCommand",
            "(Landroid/content/Intent;II)I",
        )
        builder.recordInstrumented("onCreate", "()V")

        val record = requireNotNull(builder.finish())
        assertEquals(AndroidComponentCatalogKind.SERVICE, record.kind)
        assertEquals(2, record.entryPoints.size)
        assertEquals(AndroidComponentCoverage.PARTIAL, record.coverage)
        assertEquals(listOf("onStartCommand(Landroid/content/Intent;II)I"), record.uncoveredEntryPoints)
    }

    @Test
    fun aidlStubCatalogRetainsOnlyApprovedMetadata() {
        val builder = AndroidComponentCatalogClassBuilder(
            "example/ISyncService\$Stub",
            setOf(
                "example/ISyncService\$Stub",
                "android/os/Binder",
                "example/ISyncService",
                "android/os/IInterface",
            ),
        )
        builder.recordClass(Opcodes.ACC_PUBLIC)
        builder.recordField(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC or Opcodes.ACC_FINAL,
            "DESCRIPTOR",
            "Ljava/lang/String;",
            "example.ISyncService",
        )
        builder.recordField(
            Opcodes.ACC_STATIC or Opcodes.ACC_FINAL,
            "TRANSACTION_syncNow",
            "I",
            7,
        )
        builder.recordMethod(
            Opcodes.ACC_PUBLIC,
            "onTransact",
            "(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z",
        )

        val record = requireNotNull(builder.finish())
        assertEquals(AndroidComponentCatalogKind.AIDL_STUB, record.kind)
        assertEquals("example.ISyncService", record.aidlDescriptor)
        assertEquals(mapOf(7L to "syncNow"), record.transactions)
        assertTrue(record.fieldsExcludeSensitiveRuntimeData())
    }

    @Test
    fun catalogWriterProducesDeterministicVariantShard() {
        val directory = Files.createTempDirectory("jankhunter-components-catalog").toFile()
        val builder = AndroidComponentCatalogClassBuilder(
            "example/BootReceiver",
            setOf("example/BootReceiver", "android/content/BroadcastReceiver"),
        )
        builder.recordClass(Opcodes.ACC_PUBLIC)
        builder.recordMethod(
            Opcodes.ACC_PUBLIC,
            "onReceive",
            "(Landroid/content/Context;Landroid/content/Intent;)V",
        )

        AndroidComponentCatalogWriter.write(directory.absolutePath, requireNotNull(builder.finish()))

        val json = InstrumentationArtifactFiles.readJsonlLines(directory).single()
        assertTrue(json.contains("\"format\":1"))
        assertTrue(json.contains("\"kind\":\"receiver\""))
        assertTrue(json.contains("\"coverage\":\"none\""))
        assertTrue(json.contains("\"uncoveredEntryPoints\""))
    }

    private fun AndroidComponentCatalogRecord.fieldsExcludeSensitiveRuntimeData(): Boolean {
        val fieldNames = javaClass.declaredFields.mapTo(HashSet()) { it.name.lowercase() }
        return fieldNames.none { name ->
            name.contains("payload") || name.contains("extras") || name == "uid" || name == "pid"
        }
    }
}
