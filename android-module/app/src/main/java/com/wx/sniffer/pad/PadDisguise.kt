package com.wx.sniffer.pad

import android.app.Activity
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.content.res.Resources
import android.os.Build
import android.util.Log
import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge
import de.robv.android.xposed.XposedHelpers
import de.robv.android.xposed.callbacks.XC_LoadPackage

/**
 * PadDisguise tricks WeChat into recognizing the current device as a tablet (Pad),
 * enabling multi-device dual-login (phone + pad simultaneously) without kicking off sessions.
 * It also locks portrait orientation to ensure standard phone usability.
 */
object PadDisguise {
    private const val TAG = "WX-PadDisguise"

    /**
     * Master switch for PadDisguise.
     *
     * WHY THIS EXISTS: PadDisguise spoofs Build.MODEL/DEVICE/PRODUCT/MANUFACTURER and
     * Configuration.smallestScreenWidthDp/screenLayout to fake a tablet. This overlaps with the
     * responsibility of `com.roro.tablethook` (TabletHook), which is the module currently proven
     * to grant the u999 tablet secondary-device login. When both modules inject com.tencent.mm,
     * their spoofing Hooks may collide (last-writer-wins on static fields / before-Hook args),
     * risking the already-working u999 login. To allow coexisting safely without recompiling,
     * the disguise can be disabled at runtime via system property, leaving the sniffer (read-only
     * DB Hook) active while TabletHook owns the disguise exclusively.
     *
     * Default: ENABLED (preserves existing behaviour).
     * Disable at runtime (no recompile / no reinstall of the APK needed):
     *   su -c 'setprop persist.wx.sniffer.pad_disguise 0'
     *   (restart WeChat / reboot to re-apply; the property is read in apply()).
     */
    private const val PROP_PAD_DISGUISE_ENABLED = "persist.wx.sniffer.pad_disguise"

    /**
     * @return true if PadDisguise should run. Reads the system property
     * [PROP_PAD_DISGUISE_ENABLED]; any value other than exactly "0" is treated as enabled.
     */
    fun isEnabled(): Boolean {
        return try {
            val cls = Class.forName("android.os.SystemProperties")
            val get = cls.getDeclaredMethod("get", String::class.java, String::class.java)
            val v = get.invoke(null, PROP_PAD_DISGUISE_ENABLED, "1") as? String ?: "1"
            v != "0"
        } catch (e: Throwable) {
            true
        }
    }

    private const val FAKE_MODEL = "SM-X910" // Samsung Galaxy Tab S9 Ultra
    private const val FAKE_MANUFACTURER = "samsung"
    private const val FAKE_DEVICE = "gts9uwifi"
    private const val FAKE_PRODUCT = "gts9uwifi"

    /**
     * Apply PadDisguise hooks unless disabled via [PROP_PAD_DISGUISE_ENABLED].
     * Backward compatible: default behaviour (enabled) is unchanged when the property is absent.
     */
    fun apply(lpparam: XC_LoadPackage.LoadPackageParam) {
        if (!isEnabled()) {
            Log.i(TAG, "PadDisguise DISABLED via system property $PROP_PAD_DISGUISE_ENABLED=0; " +
                    "skipping disguise (coexist mode with com.roro.tablethook).")
            return
        }
        try {
            spoofBuildProperties()
            hookConfiguration(lpparam)
            lockPortraitOrientation(lpparam)
            Log.i(TAG, "PadDisguise hooks successfully applied for ${lpparam.packageName}")
        } catch (e: Throwable) {
            Log.e(TAG, "Error applying PadDisguise hooks: ${e.message}", e)
        }
    }

    /**
     * Spoof Build static fields in the target process.
     */
    private fun spoofBuildProperties() {
        try {
            XposedHelpers.setStaticObjectField(Build::class.java, "MODEL", FAKE_MODEL)
            XposedHelpers.setStaticObjectField(Build::class.java, "MANUFACTURER", FAKE_MANUFACTURER)
            XposedHelpers.setStaticObjectField(Build::class.java, "DEVICE", FAKE_DEVICE)
            XposedHelpers.setStaticObjectField(Build::class.java, "PRODUCT", FAKE_PRODUCT)
            Log.i(TAG, "Spoofed Build properties to Model=$FAKE_MODEL Manufacturer=$FAKE_MANUFACTURER")
        } catch (e: Throwable) {
            Log.w(TAG, "Failed to spoof Build fields directly: ${e.message}")
        }
    }

    /**
     * Hook Configuration and Resources to report a tablet screen layout (sw >= 600dp / XLARGE).
     */
    private fun hookConfiguration(lpparam: XC_LoadPackage.LoadPackageParam) {
        try {
            // Hook Resources.getConfiguration to adjust smallestScreenWidthDp and screenLayout
            XposedHelpers.findAndHookMethod(
                Resources::class.java,
                "getConfiguration",
                object : XC_MethodHook() {
                    override fun afterHookedMethod(param: MethodHookParam) {
                        val config = param.result as? Configuration ?: return
                        // WeChat checks smallestScreenWidthDp >= 600 to identify tablet devices
                        if (config.smallestScreenWidthDp < 600) {
                            config.smallestScreenWidthDp = 600
                        }
                        config.screenLayout = (config.screenLayout and Configuration.SCREENLAYOUT_SIZE_MASK.inv()) or
                                Configuration.SCREENLAYOUT_SIZE_XLARGE
                    }
                }
            )

        } catch (e: Throwable) {
            Log.w(TAG, "Failed to hook Configuration: ${e.message}")
        }
    }

    /**
     * Prevent WeChat from forcibly rotating to landscape when Pad mode is active,
     * maintaining clean portrait usage on the physical phone screen.
     */
    private fun lockPortraitOrientation(lpparam: XC_LoadPackage.LoadPackageParam) {
        try {
            XposedHelpers.findAndHookMethod(
                Activity::class.java,
                "setRequestedOrientation",
                Int::class.javaPrimitiveType,
                object : XC_MethodHook() {
                    override fun beforeHookedMethod(param: MethodHookParam) {
                        val requested = param.args[0] as Int
                        // If activity attempts landscape or sensor landscape, override to portrait
                        if (requested == ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE ||
                            requested == ActivityInfo.SCREEN_ORIENTATION_USER_LANDSCAPE ||
                            requested == ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                        ) {
                            param.args[0] = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
                        }
                    }
                }
            )
        } catch (e: Throwable) {
            Log.w(TAG, "Failed to hook setRequestedOrientation: ${e.message}")
        }
    }
}
