# Fixed-strategy measurement artifact only. The benchmark rejects WRAPPERS for this APK.
# This lets R8 remove unreachable legacy implementations to measure rollback's size surcharge.
-assumevalues class io.jankhunter.runtime.RuntimeTelemetryAccess {
    boolean getUsesTaskWrappers() return false;
    boolean getUsesCoroutineWrappers() return false;
}
