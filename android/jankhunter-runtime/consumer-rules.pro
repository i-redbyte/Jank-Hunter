# Legacy object-based hooks resolve these optional framework types by name.
# Preserve only the reflection contract; missing integrations remain optional.
-keep,allowoptimization class androidx.work.ListenableWorker$Result$Success
-keep,allowoptimization class androidx.work.ListenableWorker$Result$Failure
-keep,allowoptimization class androidx.work.ListenableWorker$Result$Retry

-keep,allowoptimization class androidx.fragment.app.Fragment {
    public android.view.View getView();
}
-keep,allowoptimization class android.support.v4.app.Fragment {
    public android.view.View getView();
}
-keep,allowoptimization class androidx.lifecycle.ViewModel
-keep,allowoptimization class android.arch.lifecycle.ViewModel
-keep,allowoptimization interface androidx.viewbinding.ViewBinding {
    public android.view.View getRoot();
}

# Application fields/getters are intentionally not kept for the legacy fallback.
# Full lifecycle/binding coverage requires the current plugin's typed accessors.
