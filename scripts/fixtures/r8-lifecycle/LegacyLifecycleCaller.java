package com.example.jhsmoke;

/** Calls the public JVM ABI emitted by old plugins, despite its Kotlin-internal declaration. */
public final class LegacyLifecycleCaller {
    private LegacyLifecycleCaller() { }

    public static void watch(Object instance, String event, String owner) {
        io.jankhunter.runtime.JankHunterHooks.watchLifecycleObject(instance, event, owner);
    }
}
