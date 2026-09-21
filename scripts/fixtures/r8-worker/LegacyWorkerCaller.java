package com.example.jhsmoke;

/** Models the public JVM entrypoint emitted into consumers by earlier plugin versions. */
public final class LegacyWorkerCaller {
    private LegacyWorkerCaller() { }

    public static int classify(Object value) {
        return io.jankhunter.runtime.JankHunterHooks.classifyWorkerOutcome(value);
    }
}
