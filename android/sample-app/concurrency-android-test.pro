# Error Prone annotations in AndroidX Test are compile-time metadata. Android has no javax.lang.model API.
-dontwarn javax.lang.model.element.Modifier

# Device commands select these test classes by their original fully qualified names.
-keep class io.jankhunter.sample.ConcurrencyBoundaryBenchmarkTest { *; }
-keep class io.jankhunter.sample.ConcurrencyDiagnosticsLogTest { *; }

-keep class io.jankhunter.sample.ConcurrencyRegistryFootprintTest { *; }
