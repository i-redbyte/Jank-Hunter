package com.fixture.compat;

import com.fixture.callback.CallbackPipeline;

/** Direct paired measurement calls are excluded; the callback pipeline is FULL-instrumented. */
public final class HookFixture {
    private HookFixture() {}
    @io.jankhunter.annotations.JankHunterIgnore
    public static long enter(long id, String name) {
        return io.jankhunter.runtime.JankHunterHooks.enterMethod(id, name);
    }
    @io.jankhunter.annotations.JankHunterIgnore
    public static void exit(long token, long id) {
        io.jankhunter.runtime.JankHunterHooks.exitMethod(token, id);
    }
    public static CallbackPipeline create(Runnable handled, Runnable dropped, Runnable failure) {
        return new CallbackPipeline(handled, dropped, failure);
    }
}
