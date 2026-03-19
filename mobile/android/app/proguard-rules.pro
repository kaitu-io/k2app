# === Kaitu ProGuard Rules ===

# --- Capacitor ---
-keep public class * extends com.getcapacitor.Plugin { *; }
-keep @com.getcapacitor.annotation.CapacitorPlugin class * { *; }
-keep @com.getcapacitor.PluginMethod class * { *; }
-keepclassmembers class * {
    @com.getcapacitor.PluginMethod public <methods>;
}
-keep class com.getcapacitor.** { *; }

# --- K2Plugin (Capacitor plugin) ---
-keep class io.kaitu.k2plugin.** { *; }

# --- K2VpnService + bridge ---
-keep class io.kaitu.K2VpnService { *; }
-keep class io.kaitu.K2VpnServiceUtils { *; }
-keep interface io.kaitu.k2plugin.VpnServiceBridge { *; }

# --- gomobile AAR (appext) ---
# gomobile uses reflection + JNI — keep all exported types
-keep class appext.** { *; }
-keep class go.** { *; }

# --- Android VPN framework ---
-keep class android.net.VpnService { *; }
-keep class * extends android.net.VpnService { *; }

# --- WebView JavaScript interface ---
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}

# --- AndroidX ---
-keep class androidx.core.content.FileProvider { *; }

# --- Keep source file + line numbers for crash reports ---
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile

# --- Suppress warnings for gomobile generated code ---
-dontwarn go.**
-dontwarn appext.**
