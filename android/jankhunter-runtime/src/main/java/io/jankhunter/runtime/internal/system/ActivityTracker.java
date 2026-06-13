package io.jankhunter.runtime.internal.system;

import android.app.Activity;
import android.app.Application;
import android.os.Bundle;

import io.jankhunter.runtime.JankHunter;

public final class ActivityTracker implements Application.ActivityLifecycleCallbacks {
    @Override
    public void onActivityCreated(Activity activity, Bundle savedInstanceState) {
        JankHunter.setScreen(activity.getClass().getName());
    }

    @Override
    public void onActivityStarted(Activity activity) {
        JankHunter.setScreen(activity.getClass().getName());
    }

    @Override
    public void onActivityResumed(Activity activity) {
        JankHunter.setScreen(activity.getClass().getName());
    }

    @Override
    public void onActivityPaused(Activity activity) {
    }

    @Override
    public void onActivityStopped(Activity activity) {
    }

    @Override
    public void onActivitySaveInstanceState(Activity activity, Bundle outState) {
    }

    @Override
    public void onActivityDestroyed(Activity activity) {
    }
}
