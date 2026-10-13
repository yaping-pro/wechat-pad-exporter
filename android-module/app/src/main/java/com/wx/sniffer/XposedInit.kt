package com.wx.sniffer

import android.util.Log
import com.wx.sniffer.hook.DexKitSniffer
import com.wx.sniffer.pad.PadDisguise
import de.robv.android.xposed.IXposedHookLoadPackage
import de.robv.android.xposed.callbacks.XC_LoadPackage

/**
 * XposedInit serves as the main entry point for the LSPosed module.
 * It intercepts the WeChat application lifecycle and initializes PadDisguise and DexKitSniffer.
 */
class XposedInit : IXposedHookLoadPackage {

    companion object {
        private const val TAG = "WX-Sniffer-Init"
        private const val TARGET_PACKAGE = "com.tencent.mm"
    }

    override fun handleLoadPackage(lpparam: XC_LoadPackage.LoadPackageParam) {
        if (lpparam.packageName != TARGET_PACKAGE) {
            return
        }

        val processName = lpparam.processName
        Log.i(TAG, "Initializing WX-Pad-Sniffer in process: $processName (package: ${lpparam.packageName})")

        try {
            // 1. Apply Pad disguise and portrait lock.
            // PadDisguise is gated by the system property persist.wx.sniffer.pad_disguise
            // (default enabled; set to "0" to disable without recompiling). Disabling is
            // recommended when coexisting with com.roro.tablethook so TabletHook alone owns
            // the tablet disguise and the proven u999 login is preserved.
            PadDisguise.apply(lpparam)

            // 2. Initialize non-blocking message sniffer
            // Sniffer is active on main and push processes
            if (processName == TARGET_PACKAGE || processName.endsWith(":push") || processName.endsWith(":tools")) {
                DexKitSniffer.init(lpparam)
            }

            Log.i(TAG, "WX-Pad-Sniffer modules initialized successfully for $processName")
        } catch (e: Throwable) {
            Log.e(TAG, "Fatal error initializing WX-Pad-Sniffer: ${e.message}", e)
        }
    }
}
