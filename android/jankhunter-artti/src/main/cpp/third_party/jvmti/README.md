# Android JVMTI header provenance

`jvmti.h` is copied without modification from the Android Open Source Project,
tag `android-9.0.0_r1`, path `art/openjdkjvmti/include/jvmti.h`.

Source: <https://android.googlesource.com/platform/art/+/android-9.0.0_r1/openjdkjvmti/include/jvmti.h>

The header is vendored because Android NDK 30 ships JNI headers but does not
ship the public JVMTI declarations required to compile an ART TI agent. Its
license notice remains in the header. Runtime compatibility is determined by
`GetEnv`, capability negotiation, and individual JVMTI error results rather
than by assumptions derived from the header version.
