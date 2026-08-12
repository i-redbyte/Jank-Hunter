# JNI entry points are resolved by their exported native names.
-keep,includedescriptorclasses class io.jankhunter.artti.internal.ArtTiNativeBridge { native <methods>; }
-keep class io.jankhunter.artti.internal.ArtTiIntegration { public <init>(); }
