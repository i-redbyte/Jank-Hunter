plugins {
    id("io.jankhunter.android-library")
}

android {
    namespace = "io.jankhunter.artti"
    ndkVersion = libs.versions.android.ndk.get()

    defaultConfig {
        externalNativeBuild {
            cmake {
                arguments += listOf("-DJH_BUILD_HOST_TESTS=OFF")
            }
        }
        ndk {
            abiFilters += setOf("arm64-v8a", "x86_64")
        }
        consumerProguardFiles("consumer-rules.pro")
    }

    externalNativeBuild {
        cmake {
            path = file("src/main/cpp/CMakeLists.txt")
            version = libs.versions.android.cmake.get()
        }
    }
}

dependencies {
    implementation(project(":jankhunter-runtime"))
    testImplementation(libs.junit)
}
