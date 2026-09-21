import io.jankhunter.buildlogic.JankHunterCliArchiveTask
import org.gradle.api.tasks.bundling.Compression

plugins {
    id("io.jankhunter.cli-package")
}

val cliMakefile = rootProject.layout.projectDirectory.file("../cli/Makefile")
val cliVersion = providers.gradleProperty("jankHunterCliVersion")
    .orElse(providers.gradleProperty("jankHunterVersion"))
version = cliVersion.get()

val cliSourceDirectory = rootProject.layout.projectDirectory.dir("../cli")
val cliBinary = layout.buildDirectory.file("cli/darwin-arm64/jankhunter")
val retraceBundle = layout.buildDirectory.dir("cli/darwin-arm64/jankhunter-retrace")

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
        "RETRACE_DIR=${retraceBundle.get().asFile.absolutePath}",
        "OUT=${cliBinary.get().asFile.absolutePath}",
    )
    inputs.files(
        cliSourceDirectory.file("go.mod"),
        cliMakefile,
        rootProject.layout.projectDirectory.file("../scripts/build-retrace-bundle.sh"),
        cliSourceDirectory.asFileTree.matching {
            include("**/*.go", "**/*.css", "**/*.js", "**/*.tmpl", "retrace/**/*.java", "go.sum")
        },
    )
    inputs.property("cliVersion", cliVersion)
    outputs.file(cliBinary)
    outputs.dir(retraceBundle)
}

val packageDarwinArm64Cli by tasks.registering(JankHunterCliArchiveTask::class) {
    group = "distribution"
    description = "Packages the macOS Apple Silicon CLI and its offline Retrace bundle."
    dependsOn(buildDarwinArm64Cli)
    retraceDirectory.set(retraceBundle)
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
    from(retraceDirectory) {
        into("jankhunter-retrace")
        filePermissions {
            unix("644")
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
