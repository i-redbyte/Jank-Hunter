# AGP provenance integration fixture

Run `./verify.sh` from this directory with Android SDK 35 and JDK 17 or newer. It builds an app with AGP 9.0.1 and Jank Hunter instrumentation, repeats with configuration cache, and assembles a minified release. The catalog must attribute app, local module, runtime-only local module, and included build to PROJECT; a Maven AAR with the app package to DEPENDENCY; and a file JAR to UNKNOWN. The script also checks a single `Thread.start()` hook in two app-owned classes an unminified source method literal, and WHOLE_APPLICATION selection of a file dependency outside the app namespace. This is a build/provenance fixture; it does not execute Android runtime hooks. Scoped-transform JVM tests separately execute generated classes to check frame validity and exception semantics.

Generated AAR/JAR and Gradle outputs stay under ignored fixture paths.
