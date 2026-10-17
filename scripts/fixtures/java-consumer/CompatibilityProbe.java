package com.fixture.compat;

import io.jankhunter.runtime.JankHunterConfig;

public final class CompatibilityProbe {
    public static String verify() throws Exception {
        if (JankHunterConfig.builder().build() == null) throw new AssertionError("SDK builder");
        if (compute(9) != 10) throw new AssertionError("return value changed");
        IllegalStateException expected = new IllegalStateException("fixture");
        try { fail(expected); throw new AssertionError("exception lost"); }
        catch (IllegalStateException observed) { if (observed != expected) throw new AssertionError("throwable changed"); }
        if (!CompatibilityProbe.class.getDeclaredMethod("compute", int.class).invoke(null, 4).equals(5)) {
            throw new AssertionError("reflection changed");
        }
        return "compatibility-ok";
    }
    private static int compute(int value) { return value + 1; }
    private static void fail(IllegalStateException expected) { throw expected; }
}
