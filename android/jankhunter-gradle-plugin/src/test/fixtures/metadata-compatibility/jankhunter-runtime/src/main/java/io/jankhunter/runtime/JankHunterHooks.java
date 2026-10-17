package io.jankhunter.runtime;

/** Linkage-only fixture; behavioral SDK release tests use the exact published artifact. */
public final class JankHunterHooks {
    private JankHunterHooks() {}
    public static long enterMethod(long id, String owner) { return 0L; }
    public static void exitMethod(long started, long id) {}
}
