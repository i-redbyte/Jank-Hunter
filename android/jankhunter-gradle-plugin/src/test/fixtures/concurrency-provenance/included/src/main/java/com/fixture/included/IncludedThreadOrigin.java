package com.fixture.included;

public final class IncludedThreadOrigin {
    public void start() {
        new Thread(() -> { }).start();
    }
}
