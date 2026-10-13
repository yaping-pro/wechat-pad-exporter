package com.wx.sniffer

import android.util.Log
import com.wx.sniffer.hook.DexKitSniffer
import de.robv.android.xposed.IXposedHookLoadPackage
import de.robv.android.xposed.callbacks.XC_LoadPackage

/**
 * XposedInit serves as the main entry point for the LSPosed module.
 * It intercepts the WeChat application lifecycle and initializes DexKitSniffer
 * for real-time, read-only data extraction.
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
        Log.i(TAG, "Initializing WX-Sniffer in process: $processName (package: ${lpparam.packageName})")

        try {
            // Initialize non-blocking message sniffer
            // Sniffer is active on main and push processes
            if (processName == TARGET_PACKAGE || processName.endsWith(":push") || processName.endsWith(":tools")) {
                DexKitSniffer.init(lpparam)
            }

            Log.i(TAG, "WX-Sniffer module initialized successfully for $processName")
        } catch (e: Throwable) {
            Log.e(TAG, "Fatal error initializing WX-Sniffer: ${e.message}", e)
        }
    }
}
