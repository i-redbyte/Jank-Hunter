# These narrow entry points are called from the separate instrumentation APK.
# Scenario bodies and private helpers remain eligible for optimization and obfuscation.
-keep class io.jankhunter.sample.concurrency.ConcurrencyDiagnosticScenario {
    <init>(android.content.Context);
    java.lang.Object run(kotlin.coroutines.Continuation);
    java.util.UUID enqueueRetryWorker();
}
-keep class io.jankhunter.sample.concurrency.ConcurrencyBoundaryBenchmark {
    <init>(...);
    java.io.File run(int);
}
-keep class io.jankhunter.sample.concurrency.ExecutionObservationProbe { *; }
-keep class io.jankhunter.sample.concurrency.ConcurrencyRegistryFootprint {
    <init>(android.content.Context);
    java.io.File capture();
}
# The release test APK calls these shared dependencies through their original ABI. AGP
# excludes them from that APK because the target contains the same artifacts; keep only
# shared API families that the test DEX actually invokes, so target R8 may still shrink
# Jank Hunter's implementation classes and the scenario bodies.
-keep class androidx.tracing.Trace { *; }
-keep class androidx.concurrent.futures.CallbackToFutureAdapter { *; }
-keep class androidx.concurrent.futures.CallbackToFutureAdapter$Completer { *; }
-keep class androidx.concurrent.futures.ResolvableFuture { *; }
-keep class androidx.work.Data { *; }
-keep class androidx.work.WorkInfo { *; }
-keep class androidx.work.WorkInfo$State { *; }
-keep class androidx.work.WorkManager { *; }
-keep class androidx.work.WorkManager$Companion { *; }
-keep class io.jankhunter.runtime.JankHunter { *; }
-keep class io.jankhunter.runtime.JankHunterConfig { *; }
-keep class io.jankhunter.runtime.JankHunterConfig$Builder { *; }
# Implementations of this interface are supplied by the separate test APK.
-keep interface io.jankhunter.runtime.JankHunterConfigUpdater { *; }
-keep class io.jankhunter.runtime.JankHunterStoragePolicy {
    public <init>(...);
}
-keep class io.jankhunter.runtime.JankHunterManifestConfig { *; }
-keep class io.jankhunter.runtime.JankHunterManifestConfig$Companion { *; }
-keep class io.jankhunter.runtime.JankHunterRuntimeFeature { *; }
# The separate test APK reads these getters after target R8 has optimized its own calls.
-keepclassmembers,allowobfuscation class io.jankhunter.runtime.JankHunterExecutionObservationModes {
    public io.jankhunter.runtime.JankHunterExecutionObservationMode getTasks();
    public io.jankhunter.runtime.JankHunterExecutionObservationMode getCoroutines();
}
-keep class io.jankhunter.runtime.JankHunterTelemetry { *; }
-keep class io.jankhunter.sample.JankHunterLogExporter { *; }
-keep class io.jankhunter.sample.R$string { *; }
-keep class io.jankhunter.sample.automatic.ScenarioProgressUseCase { *; }
-keep class io.jankhunter.sample.automatic.ScenarioStep { *; }
-keep class io.jankhunter.sample.manual.ManualAction { *; }
-keep class io.jankhunter.sample.manual.ManualControlsUiFactory { *; }
-keep class io.jankhunter.sample.ui.SampleAction { *; }
-keep class io.jankhunter.sample.ui.SampleSection { *; }
-keep class kotlin.Result { *; }
-keep class kotlin.LazyKt { *; }
-keep class kotlin.ResultKt { *; }
-keep class kotlin.collections.ArraysKt* { *; }
-keep class kotlin.collections.CollectionsKt* { *; }
-keep class kotlin.collections.SetsKt { *; }
-keep class kotlin.comparisons.ComparisonsKt { *; }
-keep class kotlin.coroutines.ContinuationKt { *; }
-keep class kotlin.coroutines.intrinsics.IntrinsicsKt { *; }
-keep class kotlin.coroutines.jvm.internal.Boxing { *; }
-keep class kotlin.coroutines.jvm.internal.DebugProbesKt { *; }
-keep class kotlin.coroutines.jvm.internal.SpillingKt { *; }
-keep class kotlin.coroutines.jvm.internal.SuspendLambda { *; }
-keep class kotlin.io.FilesKt* { *; }
-keep class kotlin.io.ByteStreamsKt { *; }
-keep class kotlin.io.CloseableKt { *; }
-keep class kotlin.jvm.internal.StringCompanionObject { *; }
-keep class kotlin.jvm.internal.FunctionReference { *; }
-keep class kotlin.jvm.internal.FunctionReferenceImpl { *; }
-keep class kotlin.jvm.internal.AdaptedFunctionReference { *; }
-keep class kotlin.jvm.internal.Ref$BooleanRef { *; }
-keep class kotlin.jvm.internal.Ref$ObjectRef { *; }
-keep class kotlin.ranges.RangesKt { *; }
-keep class kotlin.sequences.SequencesKt* { *; }
-keep class kotlin.text.StringsKt* { *; }
-keep class kotlin.time.Duration { *; }
-keep class kotlin.time.DurationKt { *; }
-keep class kotlinx.coroutines.CancellableContinuation { *; }
-keep class kotlinx.coroutines.CancellableContinuationImpl { *; }
-keep class kotlinx.coroutines.BuildersKt* { *; }
-keep class kotlinx.coroutines.Deferred { *; }
-keep class kotlinx.coroutines.Dispatchers { *; }
-keep class kotlinx.coroutines.ExecutorsKt* { *; }
-keep class kotlinx.coroutines.Job$DefaultImpls { *; }
-keep class kotlinx.coroutines.TimeoutKt* { *; }
-keep class kotlinx.coroutines.flow.FlowKt* { *; }
