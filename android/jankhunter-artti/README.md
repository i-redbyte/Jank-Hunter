# Jank Hunter ART TI native core

`jankhunter-artti` is the optional ART TI agent AAR. The C++20 core is independent of JNI/JVMTI
types and can be built and tested on the host:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-host \
  -DJH_BUILD_HOST_TESTS=ON
cmake --build /private/tmp/jh-artti-host --parallel
ctest --test-dir /private/tmp/jh-artti-host --output-on-failure
/private/tmp/jh-artti-host/jh_artti_core_bench
```

Sanitizer build:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-asan \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_SANITIZERS=ON
cmake --build /private/tmp/jh-artti-asan --parallel
ctest --test-dir /private/tmp/jh-artti-asan --output-on-failure
```

Thread-sanitizer build, where the host compiler/runtime supports it:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-tsan \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_TSAN=ON
cmake --build /private/tmp/jh-artti-tsan --parallel
ctest --test-dir /private/tmp/jh-artti-tsan --output-on-failure
```

Native decoder fuzzing, with a Clang toolchain that has a linkable libFuzzer runtime:

```bash
cmake -S android/jankhunter-artti/src/main/cpp \
  -B /private/tmp/jh-artti-fuzz \
  -DJH_BUILD_HOST_TESTS=ON \
  -DJH_ENABLE_FUZZER=ON
cmake --build /private/tmp/jh-artti-fuzz --target jh_artti_batch_decoder_fuzz --parallel
/private/tmp/jh-artti-fuzz/jh_artti_batch_decoder_fuzz -max_total_time=30
```

Configuration fails with an explicit message when the selected Clang installation cannot link
libFuzzer. The current Xcode AppleClang toolchain has that limitation; Go's equivalent canonical
payload fuzz target remains runnable with
`go test ./internal/jhlog -run '^$' -fuzz FuzzAgentPayloadNeverPanics -fuzztime 30s`.

Benchmarks print JSON lines with latency percentiles, throughput and drops. They are regression
evidence, not device release gates.

## Device and publishing smoke

With an attached debuggable emulator/device:

```bash
cd android
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiHardeningTest
./gradlew :sample-app:connectedDebugAndroidTest \
  -Pandroid.testInstrumentationRunnerArguments.class=io.jankhunter.sample.ArtTiPerformanceSmokeTest
```

The hardening scenario drives real GC, thread churn, monitor contention and triggered stacks,
injects a bounded overload, shuts the SDK down, then structurally verifies committed v9 agent
events and final loss/status evidence. The performance test writes only aggregate timing, CPU,
PSS/native-heap and frame-interval values; it does not export application payload.

`scripts/gradle-plugin-smoke.sh` publishes all artifacts into an isolated Maven Local repository,
builds an external minified consumer twice with configuration-cache reuse, and verifies that the
published ART TI AAR contributes arm64/x86_64 libraries only to the configured debug variant.

## Native batch protocol V1

- fixed little-endian config and handshake structs with `structSize` and schema/ABI versions;
- a 32-byte batch header followed by 88-byte minimum length-delimited records;
- unknown records are skipped by declared length;
- Kotlin drains into a reusable direct `ByteBuffer`;
- the decoder uses a single mutable record view for visitor delivery rather than a JVM object per
  native event;
- status codes cross JNI; native exceptions and STL ownership do not.
