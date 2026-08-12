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

Benchmarks print JSON lines with latency percentiles, throughput and drops. They are regression
evidence, not device release gates.
