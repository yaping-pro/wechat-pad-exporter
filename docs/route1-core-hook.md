# 微信实时抓取小助理（路线一：Root + WeChatPad + 进内存被动抓取）技术架构与实现方案

---

## 1. 硬件选型与 Root 隐蔽底座 (Stealth Base)

在 7x24 小时高可用常驻的抓取体系中，底座的稳定性、温度控制及抗对抗检测能力决定了整体工程的生死。微信的安全风控机制包含设备指纹校验、环境完整性检测（TEE / Play Integrity / 腾讯自研安全沙盒安全扫描）以及动态行为特征分析。

### 1.1 推荐机型与系统版本选型

| 机型系列 | 推荐 SoC | 推荐系统版本 | 内核版本要求 | 选型分析与 7x24h 表现 |
| :--- | :--- | :--- | :--- | :--- |
| **Google Pixel 5 / 6 / 7** | Snapdragon 765G / Google Tensor G1/G2 | Android 13 / 14 (AOSP / LineageOS) | GKI 2.0 (Generic Kernel Image) / Linux 5.10+ | **首选方案**。官方 AOSP 支持最完善，GKI 内核对 KernelSU 原生支持极佳；驱动层无国产 ROM 预装常驻安全守护进程，环境纯净。需配合散热背夹或温控脚本。 |
| **小米 / 红米 865 / 870** (如 Redmi K40 / 小米 10) | Snapdragon 865 / 870 (sm8250) | LineageOS 20 (Android 13) / 官方 MIUI 降级精简版 | Linux 4.19+ (需移植 KSU 补丁或编译内核) | **高性价比方案**。高通 865/870 发热量低、能效比极佳，非常适合 7x24 挂机；但需刷入纯净 AOSP/LineageOS 移除 MIUI 内部的安全组件。 |

---

### 1.2 Root 方案对比与裁决

#### 技术裁决：选用 **KernelSU (KSU)**

```
┌─────────────────────────────────────────────────────────────┐
│                    User Space (用户空间)                    │
│                                                             │
│  [ WeChat App (com.tencent.mm) ]                            │
│    ├── 扫描 /proc/self/mounts   ──────> (完全干净，无挂载特征) │
│    ├── 扫描 /system/xbin/su     ──────> (不存在 su 文件)      │
│    ├── 扫描 /proc/self/maps     ──────> (无 Magisk 内存痕迹)  │
│    └── 执行 stat / execve("su") ──────> (直接返回 ENOENT)    │
│                                                             │
└──────────────────────────────┬──────────────────────────────┘
                               │ Syscall (系统调用层)
┌──────────────────────────────▼──────────────────────────────┐
│                    Kernel Space (内核空间)                  │
│                                                             │
│  KernelSU Hook (sys_execve / sys_prctl)                     │
│    ├── 检查 Caller UID                                      │
│    │    ├── 若不在授权白名单 ──> 透传，完全表现为无 Root 状态 │
│    │    └── 若在授权白名单   ──> 内核级直接赋予 root 权限 (UID 0)│
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

#### 对比分析：
1. **Magisk 的天然劣势**：
   - Magisk 依赖修改 `init.rc` 并在用户空间启动 `magiskd` 守护进程，通过挂载 overlay 隐藏自身。
   - 微信安全 SDK（如基于 `turing` 体系）通过直接系统调用读取 `/proc/self/mounts`、遍历 `/proc/self/maps`、检测内存重映射区、检查文件系统 inode 差异以及执行 `stat` 检测 su 路径，极易捕捉到用户态挂载残留痕迹。
2. **KernelSU 的底层优势**：
   - **内核空间控制**：KernelSU 的 hook 完全运行在内核层（通过 `sys_execve` 或 `kprobes`），用户空间不存在任何全局可见的 `su` 守护进程或二进制文件。
   - **App 白名单隔离**：未被显式授权的 App 处于标准沙盒内，调用 `execve("su")` 仅会收到内核返回的标准 `ENOENT` 错误；任何读取 `/proc/mounts` 或文件系统的操作均完全无挂载痕迹。

#### Root 隐蔽防护配置栈：
- **Kernel 底座**：KernelSU (或 APatch)。
- **Zygote 注入器**：**ZygiskNext** (提供独立的 Zygisk API 支持，隔离度高于传统嵌入实现)。
- **黑白名单隐藏**：**Shamiko**（在 Zygote 阶段为非授权 App 脱钩注入痕迹并隐藏文件句柄）。
- **密钥与环境认证伪造**：**TrickyStore**（软解 Keybox 证书，使设备通过 Play Integrity / 硬件级安全环境检测）。

---

### 1.3 Hook 框架选型与作用域隔离

1. **LSPosed 维护分支选型**：
   - 官方 LSPosed 停更后，推荐采用社区持续维护且适配 Android 13/14 现代 ART 运行时的分支（如 **JingMatrix/LSPosed** 或 **mywalkb/LSPosed_mod**）。
2. **作用域严格白名单化**：
   - 模块必须在 `xposed_init` 中声明仅关注 `com.tencent.mm`。
   - 在 LSPosed Manager 中**仅勾选微信主进程**，严禁勾选系统框架（`android`）、输入法、SystemUI 或其它守护进程，防止跨进程注入造成系统抖动或内存泄漏。

---

## 2. WeChatPad（平板模式伪装）底层原理与工程实现

微信的登录体系分为移动端（手机，单点互斥登录）与多端协同（Pad/PC/Mac 协同登录）。通过被动修改微信内部对当前运行环境的设备判决，可使同一账号在手机微信在线的同时，通过扫码在 Android 挂机端并存登录，避免挤号。

```
                    ┌────────────────────────────┐
                    │     微信客户端启动生命周期   │
                    └─────────────┬──────────────┘
                                  │
                  ┌───────────────┴───────────────┐
                  │                               │
        [ 系统层特征判断 ]                [ 微信内部判决类 ]
        ├── SystemProperties.get         ├── Util.isTabletDevice()
        ├── Build.MODEL                  └── Sub-classes / Cache
        └── Configuration.screenLayout
                  │                               │
                  └───────────────┬───────────────┘
                                  │
                     (Hook: 拦截并改写判决逻辑)
                                  │
                                  ▼
                    ┌────────────────────────────┐
                    │  强制返回 TABLET / FOLD 设备 │
                    └─────────────┬──────────────┘
                                  │
                                  ▼
                    ┌────────────────────────────┐
                    │  触发协同登录：展示 Pad 登录码 │
                    │  (与移动端手机微信共存不互斥) │
                    └────────────────────────────┘
```

### 2.1 微信平板设备判定原理

微信判断当前设备形态并非依赖单一标志位，而是综合评估系统属性、屏幕物理尺寸以及内部工具函数：

1. **系统属性层**：
   - 查询系统属性 `ro.build.characteristics` 是否包含 `tablet`。
   - 读取 `android.os.Build.MODEL`。
2. **屏幕布局与资源配置层**：
   - 读取 `Configuration.screenLayout & Configuration.SCREENLAYOUT_SIZE_MASK`，检查是否处于 `SCREENLAYOUT_SIZE_LARGE` (0x3) 或 `SCREENLAYOUT_SIZE_XLARGE` (0x4)。
   - 检查 `smallestScreenWidthDp` (sw) 是否大于等于 600dp。
3. **微信内部判定逻辑**：
   - 早期及现代微信中常驻工具类（如 `com.tencent.mm.sdk.platformtools.Util` 或 `com.tencent.mm.sdk.platformtools.MMApplicationContext`）中包含 `isTablet()`、`getDeviceType()` 方法，其内部缓存了设备状态。

---

### 2.2 Xposed Hook 伪装工程实现 (Kotlin 示范)

```kotlin
package com.wx.sniffer.hook

import android.content.res.Configuration
import de.robv.android.xposed.IXposedHookLoadPackage
import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge
import de.robv.android.xposed.XposedHelpers
import de.robv.android.xposed.callbacks.XC_LoadPackage

class WeChatPadHook : IXposedHookLoadPackage {

    companion object {
        private const val TARGET_PACKAGE = "com.tencent.mm"
    }

    override fun handleLoadPackage(lpparam: XC_LoadPackage.LoadPackageParam) {
        if (lpparam.packageName != TARGET_PACKAGE || lpparam.processName != TARGET_PACKAGE) {
            return
        }

        hookSystemCharacteristics(lpparam.classLoader)
        hookConfiguration(lpparam.classLoader)
    }

    /**
     * 拦截系统属性读取与 Build 参数
     */
    private fun hookSystemCharacteristics(classLoader: ClassLoader) {
        try {
            // Hook android.os.SystemProperties.get(String, String)
            XposedHelpers.findAndHookMethod(
                "android.os.SystemProperties",
                classLoader,
                "get",
                String::class.java,
                String::class.java,
                object : XC_MethodHook() {
                    override fun afterHookedMethod(param: MethodHookParam) {
                        val key = param.args[0] as? String
                        if (key == "ro.build.characteristics") {
                            param.result = "tablet"
                        }
                    }
                }
            )
        } catch (t: Throwable) {
            XposedBridge.log("[WeChatPad] Hook SystemProperties failed: ${t.message}")
        }
    }

    /**
     * 欺骗微信 Resource 与 Configuration 检查
     */
    private fun hookConfiguration(classLoader: ClassLoader) {
        try {
            // Hook android.content.res.Resources.getConfiguration()
            XposedHelpers.findAndHookMethod(
                "android.content.res.Resources",
                classLoader,
                "getConfiguration",
                object : XC_MethodHook() {
                    override fun afterHookedMethod(param: MethodHookParam) {
                        val config = param.result as? Configuration ?: return
                        // 伪装屏幕尺寸为 LARGE
                        config.screenLayout = (config.screenLayout and Configuration.SCREENLAYOUT_SIZE_MASK.inv()) or
                                Configuration.SCREENLAYOUT_SIZE_LARGE
                        // 伪装最小宽度 >= 600dp (典型平板阈值)
                        if (config.smallestScreenWidthDp < 600) {
                            config.smallestScreenWidthDp = 600
                        }
                    }
                }
            )
        } catch (t: Throwable) {
            XposedBridge.log("[WeChatPad] Hook Configuration failed: ${t.message}")
        }
    }
}
```

---

### 2.3 布局适配与异常处理（防止分栏错乱）

- **问题**：部分手机在欺骗为平板后，微信登录页或主页面可能尝试以左右双栏（Master-Detail）模式渲染，在小屏幕手机上可能导致登录二维码被挤出可视区域或变形。
- **解决方案**：
  1. **固定竖屏**：在 `Activity.onCreate` 处，对 `com.tencent.mm.ui.LauncherUI` 与 `com.tencent.mm.plugin.login.ui.LoginUI` 强制设定 `requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT`。
  2. **避免篡改物理 DisplayMetrics**：仅在 `Resources.getConfiguration` 返回虚假配置，不随意修改 WindowManager 的物理像素分辨率，确保 Canvas 绘制坐标系正常。

---

## 3. 内存被动只读抓取靶点 (The Core Hook Engine)

---

### 3.1 Hook 靶点选型与权衡分析

| 候选层级 | 具体实现 / 涉及类 | 抓取阶段优势 | 劣势 / 风险 | 选型结论 |
| :--- | :--- | :--- | :--- | :--- |
| **1. 网络同步层 (NetSceneNewSync)** | `NetSceneNewSync` -> `onGYNetEnd` (Protobuf) | 处于消息到达最前线，直接捕获原始二进制 PB 数据包。 | 每次混淆导致 PB 字段映射剧烈变动；需在内存重构 Protobuf 序列化逻辑，维护成本极高；容易触发重放/反作弊特征。 | **淘汰** (过度复杂) |
| **2. 底层数据库层 (WCDB / SQLite)** | `SQLiteDatabase.insertWithOnConflict` / `wcdb.SQLiteStatement` | 必然写入数据库，底层特征明确，容易定位 SQL 字符串。 | 拿到的仅为已序列化的扁平 SQL 字段或 Blob，部分复杂类型（引用的群聊名片、合并转发等）需二次解析，且无法直接获得高层封装对象。 | **备选** |
| **3. 消息仓库层 (MsgInfoStorage)** | 消息插入方法（通常入参为 `MsgInfo` / `Storage.IOnStorageChange`） | **最理想平衡点**：在微信业务层完成解密与字段组装后、持久化时截获。字段完整（发送人、Talker、Content、Type、MsgId、CreateTime 等一目了然）。 | 方法名与类名在每个版本均发生混淆，必须结合 DexKit 动态定位。 | **首选方案** |

---

### 3.2 动态反混淆框架：DexKit 的工程应用

传统的硬编码类名（如 `com.tencent.mm.storage.b7`）在微信版本迭代（甚至是热补丁发布）后会立即失效。**DexKit**（基于 C++ 实现的高性能 DEX 分析库）能在微信启动、ClassLoader 载入 DEX 的毫秒级时间内，通过字节码交叉引用（Xref）、常量字符串引用、方法签名特征，动态定位目标混淆类名与方法名。

```
                      ┌────────────────────────────┐
                      │    ClassLoader 加载 DEX    │
                      └─────────────┬──────────────┘
                                    │
                                    ▼
                      ┌────────────────────────────┐
                      │     DexKitBridge 初始化     │
                      └─────────────┬──────────────┘
                                    │
                  ┌─────────────────┴─────────────────┐
                  │                                   │
      [ 规则 1: 字符串引用查找 ]              [ 规则 2: 方法签名与特征码 ]
      - 查找常量字符串:                      - 方法入参包含 MsgInfo
        "INSERT INTO message"                 - 返回值或修饰符特征
      - 定位持有该字符串的持久化类
                  │                                   │
                  └─────────────────┬─────────────────┘
                                    │
                                    ▼
                      ┌────────────────────────────┐
                      │ 动态解析出真实运行时类与方法   │
                      │ (例: "a12.k.insert(...)")  │
                      └─────────────┬──────────────┘
                                    │
                                    ▼
                      ┌────────────────────────────┐
                      │    注入 XC_MethodHook      │
                      │ (只读克隆 MsgInfo，绝对无害)  │
                      └────────────────────────────┘
```

#### DexKit 特征搜索与 Hook 注入实现 (Kotlin 完整代码)

```kotlin
package com.wx.sniffer.hook

import android.content.Context
import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge
import de.robv.android.xposed.XposedHelpers
import org.luckypray.dexkit.DexKitBridge
import org.luckypray.dexkit.query.FindMethod
import org.luckypray.dexkit.query.matchers.MethodMatcher
import java.lang.reflect.Method
import java.util.concurrent.Executors

object MessageStorageHookEngine {

    private val workerExecutor = Executors.newSingleThreadExecutor()
    private var isHooked = false

    /**
     * 在 Application.attachBaseContext 之后调用
     */
    fun initAndHook(context: Context, classLoader: ClassLoader) {
        if (isHooked) return

        try {
            System.loadLibrary("dexkit")
            val apkPath = context.applicationInfo.sourceDir

            DexKitBridge.create(apkPath)?.use { bridge ->
                findAndHookMsgInsertMethod(bridge, classLoader)
            }
            isHooked = true
        } catch (t: Throwable) {
            XposedBridge.log("[WxSniffer] DexKit initialization or hook failed: ${t.stackTraceToString()}")
        }
    }

    /**
     * 利用 DexKit 动态查找消息插入靶点
     */
    private fun findAndHookMsgInsertMethod(bridge: DexKitBridge, classLoader: ClassLoader) {
        // 规则：搜索使用了 "INSERT INTO message" 或其等价存储 SQL 特征的方法
        val searchResults = bridge.findMethod(
            FindMethod.create()
                .matcher(
                    MethodMatcher.create()
                        .usingStrings("INSERT INTO message", "INSERT INTO message_history")
                )
        )

        val targetMethodDesc = searchResults.firstOrNull()
        if (targetMethodDesc == null) {
            XposedBridge.log("[WxSniffer] [ERROR] Target insert method NOT found by DexKit!")
            return
        }

        XposedBridge.log("[WxSniffer] Successfully resolved target: ${targetMethodDesc.className}->${targetMethodDesc.name}")

        val targetMethod: Method = targetMethodDesc.getMethodInstance(classLoader)

        // 实施被动安全 Hook
        XposedBridge.hookMethod(targetMethod, object : XC_MethodHook() {
            override fun afterHookedMethod(param: MethodHookParam) {
                // 全局严格 Try-Catch，确保零崩溃
                try {
                    val msgObj = param.args.firstOrNull() ?: return
                    
                    // 提取核心字段并立即解除原对象引用（深拷贝为 DTO）
                    extractAndDispatchMessage(msgObj)
                } catch (t: Throwable) {
                    XposedBridge.log("[WxSniffer] Critical error in afterHookedMethod: ${t.message}")
                }
            }
        })
    }

    /**
     * 被动数据克隆与无泄漏派发
     */
    private fun extractAndDispatchMessage(msgObj: Any) {
        // 利用反射提取关键属性
        val talker = XposedHelpers.getObjectField(msgObj, "field_talker") as? String ?: ""
        val content = XposedHelpers.getObjectField(msgObj, "field_content") as? String ?: ""
        val msgId = XposedHelpers.getLongField(msgObj, "field_msgId")
        val type = XposedHelpers.getIntField(msgObj, "field_type")
        val createTime = XposedHelpers.getLongField(msgObj, "field_createTime")
        val isSend = XposedHelpers.getIntField(msgObj, "field_isSend")

        // 构造与微信内部对象解耦的 Plain Kotlin Data Class
        val clonedDto = CapturedMessage(
            msgId = msgId,
            talker = talker,
            content = content,
            type = type,
            createTime = createTime,
            isSend = (isSend == 1)
        )

        // 异步派发，立即释放当前栈帧与 Dalvik/ART 引用，避免主线程 GC 堆积
        workerExecutor.execute {
            try {
                // 派发至本地 LocalSocket / IPC / 环形缓冲区
                DataDispatcher.dispatch(clonedDto)
            } catch (e: Exception) {
                XposedBridge.log("[WxSniffer] Dispatch failed: ${e.message}")
            }
        }
    }
}

data class CapturedMessage(
    val msgId: Long,
    val talker: String,
    val content: String,
    val type: Int,
    val createTime: Long,
    val isSend: Boolean
)

object DataDispatcher {
    fun dispatch(msg: CapturedMessage) {
        // 接入下游采集消费者
    }
}
```

---

### 3.3 零干扰被动安全与 GC 防护规范

1. **绝对只读契约 (Read-Only Invariant)**：
   - **严禁改写参数与返回值**：不得在 `beforeHookedMethod` / `afterHookedMethod` 中执行 `param.result = ...` 或修改 `param.args`。
   - **严禁阻断调用链**：严禁拦截异常抛出或阻止原方法进入底层数据库持久化。
   - **严禁主动调用内部私有方法**：严禁反射调用任何发信、拉取联系人列表等主动 JNI 逻辑，杜绝产生异常的主动网络行为。
2. **内存防泄漏与 GC 卫生**：
   - 截获的 `MsgInfo` 对象必须在同步回调中完成基本类型/字符串读取，**严禁将微信内部实例缓存到全局 List/Map**。
   - 数据抽取后立即封装为轻量级 DTO，移交单线程 Worker 线程池消费，避免占用微信主线程或数据库读写线程。

---

## 4. 架构交付：模块生命周期与工程骨架

### 4.1 模块初始化时序图

```
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐       ┌──────────────────┐
│  Android OS /   │       │  LSPosed Frame  │       │  WeChat Process │       │ DexKit & Message │
│  KernelSU Zygote│       │ (Hook Provider) │       │ (com.tencent.mm)│       │  Storage Hook    │
└────────┬────────┘       └────────┬────────┘       └────────┬────────┘       └────────┬─────────┘
         │                         │                         │                         │
         │ 1. Fork 微信主进程       │                         │                         │
         ├────────────────────────>│                         │                         │
         │                         │ 2. 加载模块入口类       │                         │
         │                         ├────────────────────────>│                         │
         │                         │                         │ 3. Application 启动     │
         │                         │                         │    attachBaseContext    │
         │                         │                         ├───────────────┐         │
         │                         │                         │               │         │
         │                         │                         │<──────────────┘         │
         │                         │                         │ 4. 触发 WeChatPadHook   │
         │                         │                         ├────────────────────────>│
         │                         │                         │    (注入平板配置特征)    │
         │                         │                         │                         │
         │                         │                         │ 5. MultiDex 加载完成    │
         │                         │                         ├────────────────────────>│
         │                         │                         │    6. DexKit 扫描特征   │
         │                         │                         │    7. 挂载持久化只读 Hook │
         │                         │                         │                         │
         │                         │                         │ 8. 微信收到新消息       │
         │                         │                         │    MsgInfo 入库         │
         │                         │                         ├────────────────────────>│
         │                         │                         │    9. 内存深拷贝 DTO     │
         │                         │                         │    10. 异步排队派发     │
         │                         │                         │                         │
```

---

### 4.2 工业级异常防护网与工程目录结构

```
app/src/main/
├── AndroidManifest.xml
├── assets
│   └── xposed_init                   # 声明 Xposed 入口类
└── java/com/wx/sniffer/
    ├── MainHookEntry.kt              # 主注入入口 (实现 IXposedHookLoadPackage)
    ├── pad/
    │   └── WeChatPadHook.kt          # 平板模式伪装注入器
    ├── engine/
    │   ├── MessageStorageHookEngine.kt # DexKit 动态扫描与持久化 Hook
    │   └── SafeMethodHook.kt         # 封装防崩溃安全基类
    └── model/
        └── CapturedMessage.kt        # 纯净 DTO 定义
```

#### 安全 Hook 基类封装 (`SafeMethodHook.kt`)：

```kotlin
package com.wx.sniffer.engine

import de.robv.android.xposed.XC_MethodHook
import de.robv.android.xposed.XposedBridge

/**
 * 封装了全局异常捕获的 Hook 基类，确保任何错误都不会向上传播导致微信崩溃
 */
abstract class SafeMethodHook : XC_MethodHook() {

    final override fun beforeHookedMethod(param: MethodHookParam) {
        try {
            onSafeBefore(param)
        } catch (t: Throwable) {
            XposedBridge.log("[WxSniffer] [SUPPRESSED CRASH] Exception in beforeHookedMethod: ${t.stackTraceToString()}")
        }
    }

    final override fun afterHookedMethod(param: MethodHookParam) {
        try {
            onSafeAfter(param)
        } catch (t: Throwable) {
            XposedBridge.log("[WxSniffer] [SUPPRESSED CRASH] Exception in afterHookedMethod: ${t.stackTraceToString()}")
        }
    }

    open fun onSafeBefore(param: MethodHookParam) {}
    open fun onSafeAfter(param: MethodHookParam) {}
}
```

---

## 5. 方案总结与运维红线

1. **底座选择**：强制采用 **KernelSU + ZygiskNext + Shamiko + TrickyStore** 组合，彻底在内核层隐藏 Root 痕迹，杜绝用户态挂载特征泄漏。
2. **多端共存**：通过精准 Hook `SystemProperties` 与 `Configuration.screenLayout` 伪装 Tablet，获得稳定的多设备扫码登录形态。
3. **版本自适应**：舍弃脆弱的硬编码类名，全面采用 **DexKit** 进行启动期字节码与字符串特征定位，使抓取引擎具备跨微信小版本无缝自愈能力。
4. **被动安全**：坚决执行**只读、不阻断、不调用非公开接口、即刻深拷贝**四大原则，确保 7x24 小时挂机零 OOM、零崩溃、无异常风控信令。
