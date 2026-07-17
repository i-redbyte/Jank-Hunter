-keep class io.jankhunter.okhttp3.** { *; }

-keepclassmembers class okhttp3.OkHttpClient$Builder {
    okhttp3.EventListener$Factory eventListenerFactory;
}
