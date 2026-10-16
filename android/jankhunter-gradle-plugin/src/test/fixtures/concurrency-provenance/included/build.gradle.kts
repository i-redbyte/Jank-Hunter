plugins {
    `java-library`
}

group = "com.fixture"
version = "1.0"

java {
    toolchain.languageVersion.set(JavaLanguageVersion.of(17))
}
