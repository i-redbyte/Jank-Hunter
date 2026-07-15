package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class GenerateJankHunterOwnerMapTaskTest {
    @Test
    fun writesMetadataJsonlWithoutFakeOwnersObject() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register(
            "generateOwnerMap",
            GenerateJankHunterOwnerMapTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/owner-map.json"))
        task.entriesDirectory.set(project.layout.buildDirectory.dir("jankhunter/owner-map-entries"))
        task.variantName.set("debug")
        task.methodCounters.set(true)
        task.okhttp.set(true)
        task.webSockets.set(true)
        task.handlers.set(true)
        task.executors.set(true)
        task.coroutines.set(false)
        task.flowInteractions.set(true)
        task.logSpam.set(true)
        task.classGraph.set(true)
        task.runtimeCallGraph.set(false)
        task.generatedOwners.set(true)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")
        task.androidNamespace.set("com.app")
        task.includePackages.set(setOf("com.app"))
        task.excludePackages.set(setOf("com.app.generated"))

        task.write()

        val text = task.outputFile.get().asFile.readText()
        assertTrue(text.contains("\"format\":4"))
        assertTrue(text.contains("\"kind\":\"metadata\""))
        assertTrue(text.contains("\"generatedOwners\":true"))
        assertTrue(text.contains("\"symbolNamespace\":\"0123456789abcdef0123456789abcdef\""))
        assertTrue(text.contains("\"includePackages\":[\"com.app\"]"))
        assertFalse(text.contains("\"owners\":{}"))
    }

    @Test
    fun mergesGeneratedOwnerEntryShards() {
        val project = ProjectBuilder.builder().build()
        val entriesDirectory = Files.createTempDirectory("jankhunter-owner-map-entries").toFile()
        OwnerMapWriter.writeEntries(
            entriesDirectory.absolutePath,
            "com/example/Foo",
            listOf(
                OwnerMapEntry(
                    id = OwnerIds.methodId("com/example/Foo", "load", "()V"),
                    owner = "com.example.Foo.load",
                    className = "com.example.Foo",
                    methodName = "load",
                    descriptor = "()V",
                ),
            ),
        )
        val task = project.tasks.register(
            "generateOwnerMapWithEntries",
            GenerateJankHunterOwnerMapTask::class.java,
        ).get()
        task.outputFile.set(project.layout.buildDirectory.file("jankhunter/owner-map-with-entries.json"))
        task.entriesDirectory.set(entriesDirectory)
        task.variantName.set("debug")
        task.methodCounters.set(true)
        task.okhttp.set(false)
        task.webSockets.set(false)
        task.handlers.set(false)
        task.executors.set(false)
        task.coroutines.set(false)
        task.flowInteractions.set(false)
        task.logSpam.set(false)
        task.classGraph.set(false)
        task.runtimeCallGraph.set(false)
        task.generatedOwners.set(true)
        task.symbolNamespace.set("0123456789abcdef0123456789abcdef")
        task.androidNamespace.set("com.app")
        task.includePackages.set(setOf("com.app"))
        task.excludePackages.set(emptySet())

        task.write()

        val text = task.outputFile.get().asFile.readText()
        assertTrue(text.contains("\"kind\":\"metadata\""))
        assertTrue(text.contains("\"kind\":\"entry\""))
        assertTrue(text.contains("\"id\":\"stable:0x"))
        assertTrue(text.contains("\"owner\":\"com.example.Foo.load\""))
    }

    @Test
    fun rejectsAmbiguousStableIdCollisions() {
        val collisionId = 0L
        val first = OwnerMapWriter.entryRecord(
            OwnerMapEntry(collisionId, "com.example.First.run", "com.example.First", "run", "()V"),
        )
        val second = OwnerMapWriter.entryRecord(
            OwnerMapEntry(collisionId, "com.example.Second.run", "com.example.Second", "run", "()V"),
        )

        assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(first, second))
        }
        assertTrue(first.contains("\"id\":\"stable:0x0000000000000000\""))
    }

    @Test
    fun parsesWhitespaceAndEscapedSymbolValuesBeforeComparingCollisions() {
        val canonical = """
            {
              "format" : 4,
              "kind" : "entry",
              "id" : "stable:0x0000000000000001",
              "owner" : "com.example.Foo.say\"Hi",
              "class" : "com.example.Foo",
              "method" : "say\"Hi",
              "descriptor" : "()V"
            }
        """.trimIndent()
        val equivalentEscaping = """
            {
              "format":4,
              "kind":"entry",
              "id":"stable:0x0000000000000001",
              "owner":"com.example.\u0046oo.say\u0022Hi",
              "class":"com.example.\u0046oo",
              "method":"say\u0022Hi",
              "descriptor":"()V"
            }
        """.trimIndent()

        OwnerMapWriter.validateNoCollisions(listOf(canonical, equivalentEscaping))
    }

    @Test
    fun detectsCollisionInWhitespaceFormattedRecords() {
        val first = ownerRecord(id = 2, className = "com.example.First")
        val second = ownerRecord(id = 2, className = "com.example.Second")

        assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(first, second))
        }
    }

    @Test
    fun detectsConflictingOwnerForTheSameMethodSignature() {
        val first = ownerRecord(id = 5, className = "com.example.Foo")
        val conflictingOwner = first.replace("com.example.Foo.run", "wrong.Owner.run")

        assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(first, conflictingOwner))
        }
    }

    @Test
    fun rejectsMalformedJsonInsteadOfSilentlySkippingCollisionValidation() {
        val malformed =
            """{"kind":"entry","id":"stable:0x0000000000000003","class":"com.example.Foo""""

        val error = assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(malformed))
        }

        assertTrue(error.message.orEmpty().contains("malformed JSON"))
    }

    @Test
    fun rejectsRecordsThatDoNotMatchTheOwnerMapEntrySchema() {
        val missingOwner = """
            {
              "format": 4,
              "kind": "entry",
              "id": "stable:0x0000000000000004",
              "class": "com.example.Foo",
              "method": "run",
              "descriptor": "()V"
            }
        """.trimIndent()
        val metadata = """{"format":4,"kind":"metadata"}"""

        val missingOwnerError = assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(missingOwner))
        }
        val metadataError = assertThrows(GradleException::class.java) {
            OwnerMapWriter.validateNoCollisions(listOf(metadata))
        }

        assertTrue(missingOwnerError.message.orEmpty().contains("field 'owner'"))
        assertTrue(metadataError.message.orEmpty().contains("unsupported kind 'metadata'"))
    }

    private fun ownerRecord(id: Int, className: String): String = """
        {
          "format" : 4,
          "kind" : "entry",
          "id" : "stable:0x${id.toString(16).padStart(16, '0')}",
          "owner" : "$className.run",
          "class" : "$className",
          "method" : "run",
          "descriptor" : "()V"
        }
    """.trimIndent()
}
