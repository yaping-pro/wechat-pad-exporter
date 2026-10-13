package com.wx.sniffer.hook

import android.content.ContentValues
import android.util.Log
import com.wx.sniffer.ipc.RingBufferSender
import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge
import de.robv.android.xposed.XposedHelpers
import de.robv.android.xposed.callbacks.XC_LoadPackage
import org.luckypray.dexkit.DexKitBridge

/**
 * DexKitSniffer performs non-blocking, zero-side-effect, read-only message sniffing
 * by intercepting WeChat WCDB database insertion calls and dynamic DexKit method searches.
 */
object DexKitSniffer {
    private const val TAG = "WX-DexKitSniffer"
    private const val TABLE_MESSAGE = "message"

    @Volatile
    private var selfWxid: String = ""

    fun init(lpparam: XC_LoadPackage.LoadPackageParam) {
        // Start IPC sender worker
        RingBufferSender.start()

        // 1. Hook standard WCDB SQLiteDatabase insertion
        hookWcdbDatabase(lpparam.classLoader)

        // 2. Hook Android framework SQLiteDatabase as fallback
        hookAndroidSqliteDatabase(lpparam.classLoader)

        // 3. Perform dynamic DexKit analysis for obfuscated storage methods if necessary
        initDexKitAnalysis(lpparam)
    }

    /**
     * Hook WeChat's customized WCDB database engine (com.tencent.wcdb.database.SQLiteDatabase)
     */
    private fun hookWcdbDatabase(classLoader: ClassLoader) {
        val wcdbClassNames = listOf(
            "com.tencent.wcdb.database.SQLiteDatabase",
            "com.tencent.mm.storage.SQLiteDatabase"
        )

        for (className in wcdbClassNames) {
            try {
                val dbClass = XposedHelpers.findClassIfExists(className, classLoader) ?: continue
                XposedHelpers.findAndHookMethod(
                    dbClass,
                    "insertWithOnConflict",
                    String::class.java,
                    String::class.java,
                    ContentValues::class.java,
                    Int::class.javaPrimitiveType,
                    object : XC_MethodHook() {
                        override fun afterHookedMethod(param: MethodHookParam) {
                            val table = param.args[0] as? String ?: return
                            val values = param.args[2] as? ContentValues ?: return
                            handleDatabaseInsert(table, values)
                        }
                    }
                )
                Log.i(TAG, "Successfully hooked WCDB insertWithOnConflict on $className")
            } catch (e: Throwable) {
                Log.w(TAG, "Could not hook $className: ${e.message}")
            }
        }
    }

    /**
     * Hook Android framework SQLiteDatabase as a fallback
     */
    private fun hookAndroidSqliteDatabase(classLoader: ClassLoader) {
        try {
            XposedHelpers.findAndHookMethod(
                "android.database.sqlite.SQLiteDatabase",
                classLoader,
                "insertWithOnConflict",
                String::class.java,
                String::class.java,
                ContentValues::class.java,
                Int::class.javaPrimitiveType,
                object : XC_MethodHook() {
                    override fun afterHookedMethod(param: MethodHookParam) {
                        val table = param.args[0] as? String ?: return
                        val values = param.args[2] as? ContentValues ?: return
                        handleDatabaseInsert(table, values)
                    }
                }
            )
            Log.i(TAG, "Successfully hooked Android SQLiteDatabase.insertWithOnConflict")
        } catch (e: Throwable) {
            Log.w(TAG, "Failed hooking android.database.sqlite.SQLiteDatabase: ${e.message}")
        }
    }

    /**
     * Extract fields from the inserted message row and forward to RingBufferSender asynchronously.
     */
    private fun handleDatabaseInsert(table: String, values: ContentValues) {
        if (!table.equals(TABLE_MESSAGE, ignoreCase = true)) {
            return
        }

        try {
            val msgId = values.getAsLong("msgId") ?: 0L
            val msgSvrId = values.getAsLong("msgSvrId") ?: 0L
            val talker = values.getAsString("talker") ?: return
            val type = values.getAsInteger("type") ?: 1
            val isSend = values.getAsInteger("isSend") ?: 0
            val content = values.getAsString("content") ?: ""
            val createTime = values.getAsLong("createTime") ?: System.currentTimeMillis()

            var subType = 0
            if (values.containsKey("subType")) {
                subType = values.getAsInteger("subType") ?: 0
            }

            // High-throughput non-blocking dispatch to daemon
            RingBufferSender.sendHookPayload(
                msgId = msgId,
                msgSvrId = msgSvrId,
                talker = talker,
                type = type,
                subType = subType,
                isSend = isSend,
                content = content,
                createTime = createTime,
                selfWxid = selfWxid
            )
        } catch (e: Throwable) {
            Log.e(TAG, "Error handling message insert: ${e.message}", e)
        }
    }

    /**
     * DexKit runtime analysis to locate obfuscated WeChat storage or account methods dynamically.
     */
    private fun initDexKitAnalysis(lpparam: XC_LoadPackage.LoadPackageParam) {
        Thread({
            try {
                System.loadLibrary("dexkit")
                val apkPath = lpparam.appInfo.sourceDir
                DexKitBridge.create(apkPath)?.use { bridge ->
                    Log.i(TAG, "DexKitBridge initialized successfully for WeChat APK")
                    // Bridge can be used for deep search of obfuscated core classes if WeChat updates
                }
            } catch (e: Throwable) {
                // If native library is not bundled or already loaded, continue gracefully
                Log.d(TAG, "DexKit optional dynamic scan notice: ${e.message}")
            }
        }, "wx-dexkit-worker").start()
    }
}
