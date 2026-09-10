import org.gradle.api.tasks.bundling.Compression
import org.gradle.api.tasks.bundling.Tar

plugins {
    id("io.jankhunter.cli-package")
}

val cliMakefile = rootProject.layout.projectDirectory.file("../cli/Makefile")
val defaultCliVersion = providers.fileContents(cliMakefile).asText.map { contents ->
    Regex("""(?m)^VERSION\s*\?=\s*(\S+)\s*$""")
        .find(contents)
        ?.groupValues
        ?.get(1)
        ?: error("VERSION is not set in ${cliMakefile.asFile}")
}
val cliVersion = providers.gradleProperty("jankHunterCliVersion").orElse(defaultCliVersion)
version = cliVersion.get()

val cliSourceDirectory = rootProject.layout.projectDirectory.dir("../cli")
val cliBinary = layout.buildDirectory.file("cli/darwin-arm64/jankhunter")

val buildDarwinArm64Cli by tasks.registering(Exec::class) {
    group = "build"
    description = "Builds the Jank Hunter CLI for macOS Apple Silicon."
    workingDir(cliSourceDirectory)
    commandLine(
        "make",
        "build",
        "BUILD_OS=darwin",
        "BUILD_ARCH=arm64",
        "VERSION=${cliVersion.get()}",
        "BIN_DIR=${cliBinary.get().asFile.parentFile.absolutePath}",
        "OUT=${cliBinary.get().asFile.absolutePath}",
    )
    inputs.files(
        cliSourceDirectory.file("go.mod"),
        cliMakefile,
        cliSourceDirectory.asFileTree.matching {
            include("**/*.go", "**/*.css", "**/*.js", "**/*.tmpl")
        },
    )
    inputs.property("cliVersion", cliVersion)
    outputs.file(cliBinary)
}

val packageDarwinArm64Cli by tasks.registering(Tar::class) {
    group = "distribution"
    description = "Packages the macOS Apple Silicon CLI binary."
    dependsOn(buildDarwinArm64Cli)
    archiveBaseName.set("jankhunter-cli")
    archiveVersion.set(cliVersion)
    archiveClassifier.set("darwin-arm64")
    archiveExtension.set("tar.gz")
    compression = Compression.GZIP
    destinationDirectory.set(layout.buildDirectory.dir("distributions"))
    isPreserveFileTimestamps = false
    isReproducibleFileOrder = true
    from(cliBinary) {
        rename { "jankhunter" }
        filePermissions {
            unix("755")
        }
    }
}

publishing {
    publications {
        create<MavenPublication>("cli") {
            artifactId = "jankhunter-cli"
            artifact(packageDarwinArm64Cli)
        }
    }
}
