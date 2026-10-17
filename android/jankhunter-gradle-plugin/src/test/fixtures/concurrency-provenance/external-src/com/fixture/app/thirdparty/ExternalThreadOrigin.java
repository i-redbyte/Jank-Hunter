package com.fixture.app.thirdparty;

public final class ExternalThreadOrigin {
    public void start() {
        new Thread(() -> { }).start();
    }
}
