package io.jankhunter.gradle;

import java.util.LinkedHashSet;
import java.util.Set;

public final class JankHunterExtension {
    private final Set<String> enabledBuildTypes = new LinkedHashSet<String>();
    private final Instrumentation instrumentation = new Instrumentation();
    private boolean autoInit = true;

    public JankHunterExtension() {
        enabledBuildTypes.add("debug");
    }

    public Set<String> getEnabledBuildTypes() {
        return enabledBuildTypes;
    }

    public boolean isAutoInit() {
        return autoInit;
    }

    public void setAutoInit(boolean autoInit) {
        this.autoInit = autoInit;
    }

    public Instrumentation getInstrument() {
        return instrumentation;
    }

    public void instrument(org.gradle.api.Action<Instrumentation> action) {
        action.execute(instrumentation);
    }

    public static final class Instrumentation {
        private boolean activities = true;
        private boolean fragments = true;
        private boolean okhttp = true;
        private boolean webSockets = true;
        private boolean handlers = true;
        private boolean executors = true;
        private boolean rxJava = true;
        private boolean coroutines = false;
        private final Set<String> includePackages = new LinkedHashSet<String>();
        private final Set<String> excludePackages = new LinkedHashSet<String>();

        public boolean isActivities() {
            return activities;
        }

        public void setActivities(boolean activities) {
            this.activities = activities;
        }

        public boolean isFragments() {
            return fragments;
        }

        public void setFragments(boolean fragments) {
            this.fragments = fragments;
        }

        public boolean isOkhttp() {
            return okhttp;
        }

        public void setOkhttp(boolean okhttp) {
            this.okhttp = okhttp;
        }

        public boolean isWebSockets() {
            return webSockets;
        }

        public void setWebSockets(boolean webSockets) {
            this.webSockets = webSockets;
        }

        public boolean isHandlers() {
            return handlers;
        }

        public void setHandlers(boolean handlers) {
            this.handlers = handlers;
        }

        public boolean isExecutors() {
            return executors;
        }

        public void setExecutors(boolean executors) {
            this.executors = executors;
        }

        public boolean isRxJava() {
            return rxJava;
        }

        public void setRxJava(boolean rxJava) {
            this.rxJava = rxJava;
        }

        public boolean isCoroutines() {
            return coroutines;
        }

        public void setCoroutines(boolean coroutines) {
            this.coroutines = coroutines;
        }

        public Set<String> getIncludePackages() {
            return includePackages;
        }

        public Set<String> getExcludePackages() {
            return excludePackages;
        }
    }
}
