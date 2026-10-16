package com.fixture.unknown;

public final class FileJarThreadOrigin {
    public void start() {
        new Thread(() -> { }).start();
    }
}
