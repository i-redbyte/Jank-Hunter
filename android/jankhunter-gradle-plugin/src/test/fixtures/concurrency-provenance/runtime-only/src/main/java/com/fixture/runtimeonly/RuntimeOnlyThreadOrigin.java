package com.fixture.runtimeonly;

public final class RuntimeOnlyThreadOrigin {
    public void start() {
        new Thread(() -> { }).start();
    }
}
