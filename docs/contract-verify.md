# 微信 Android 辅机 · 契约核实与代码产物盘点（CONTRACT_VERIFY）

> 生成日期：2026-10-03｜设备 `xs9ponj…(redacted)`（Redmi atom / M2004J7AC, Android 12 MIUI 13）
> 性质：**纯只读**。仅做了 `/health` 与 `/api/wechat/export` 两个 GET 探测，未 POST/写任何端点，未操作 artemis、未碰 user 0。
> 关键变更（相对第一轮）：本轮**通读了 Mac 端工程源码** `wx-pad-sniffer`，契约以**真实代码**为准而非报告转述。

---

# A. 既有工程精确契约（逐字引用真实源码）

## A1. Hook 靶点：具体类/方法、dex/进程

**源码位置**：`/Users/<USER>/Projects/wx-pad-sniffer/android-module/app/src/main/java/com/wx/sniffer/`

入口 `XposedInit.kt`（范围白名单）：
```kotlin
private const val TARGET_PACKAGE = "com.tencent.mm"
...
if (processName == TARGET_PACKAGE || processName.endsWith(":push") || processName.endsWith(":tools")) {
    DexKitSniffer.init(lpparam)
}
```
→ **作用域**：`com.tencent.mm` 主进程 + `:push` + `:tools`，与报告"仅主进程"略有差异（源码实际含 push/tools）。

核心 `DexKitSniffer.kt` 注释与 init：
```kotlin
/**
 * DexKitSniffer performs non-blocking, zero-side-effect, read-only message sniffing
 * by intercepting WeChat WCDB database insertion calls and dynamic DexKit method searches.
 */
object DexKitSniffer {
    private const val TABLE_MESSAGE = "message"
    fun init(lpparam: XC_LoadPackage.LoadPackageParam) {
        RingBufferSender.start()
        hookWcdbDatabase(lpparam.classLoader)              // 主靶点
        hookAndroidSqliteDatabase(lpparam.classLoader)    // 兜底
        initDexKitAnalysis(lpparam)                        // DexKit 动态反混淆
    }
```
- **主靶点**：`com.tencent.wcdb.database.SQLiteDatabase` 的插入调用（WCDB 层，对应报告"WCDB 写入层"）。
- **兜底**：Android 框架 `SQLiteDatabase` 插入。
- **动态定位**：DexKit 运行时搜索混淆后的存储方法（类 `MsgInfoStorage`/消息插入，与报告 3.1 候选 3 一致）。
- 提取字段来自 `ContentValues`（插入行），即 `handleDatabaseInsert(table, values)`。
- **非 NetScene 网络层 Hook**（报告 §3.1 候选1 已"淘汰"，源码未实现该层，吻合）。

> 注意：报告 `route1_core_hook.md` §3.2 给的是 `com.wx.sniffer.hook` 包名 + `MessageStorageHookEngine` 旧示例 Kotlin；**真实仓库改成了 `com.wx.sniffer` + `DexKitSniffer`，且用 WCDB 插入 Hook 而非 `MsgInfoStorage.insert` 硬编码**。契约以真实代码为准。

---

## A2. UDS 契约（socket 名 / 命名空间 / 消息格式 / 谁连谁）

**源码 `RingBufferSender.kt` 逐字**：
```kotlin
private const val SOCKET_NAME = "wechat_stream_ipc"
private const val QUEUE_CAPACITY = 8192
...
// runLoop():
Log.i(TAG, "Connecting to abstract UDS @$SOCKET_NAME...")
socket = LocalSocket()
val address = LocalSocketAddress(SOCKET_NAME, LocalSocketAddress.Namespace.ABSTRACT)
socket.connect(address)
...
writer.write(message)
writer.newLine()
writer.flush()
```

- **socket 名**：`wechat_stream_ipc`，以 **`@` 前缀 = Linux Abstract Namespace**（首字节 `\0`），**非 filesystem namespace**（与报告 §2.3 一致）。→ 完整地址即 `@wechat_stream_ipc`。
- **方向**：**微信进程（Hook 侧）是 client（connect）**，**Go 守护进程是 server（listen）**。
- **消息格式**：**逐行 JSON（Line-delimited JSON，非 Protobuf！）**。每条消息一行 `JSONObject.toString()`，写后 `newLine()` + `flush()`。
  - 报告 §2.5 写"首选 Protobuf + 4字节长度头"，**真实代码是纯文本 JSON 行**。契约以代码为准。
- **每条消息字段**（JSON key，来自 `sendHookPayload(...).put(...)`）：
  ```
  msg_id, msg_svr_id, talker, display_name, type, sub_type, is_send, content, create_time, self_wxid
  ```
- **背压**：`enqueue()` 队列满（8192）不阻塞调用方（DropPolicy，与报告 §2.4 一致）。

**守护进程侧 `main.go` 接收**：
```go
udsAddr  = flag.String("uds", "@wechat_stream_ipc", "Unix Domain Socket address (use @ for Linux abstract socket)")
...
func listenUDS(addr string) (net.Listener, error) {
    if runtime.GOOS == "linux" { /* Abstract: trim leading @, bind net.Listener "unix" */ }
}
// handleUdsConnection: 逐行读 bufio.Scanner -> RawHookMessage -> msgChan
```
→ 守护进程监听 `@wechat_stream_ipc`，逐行解析 JSON。

---

## A3. daemon HTTP API 面

**源码 `api.go` 逐字路由与结构体**：
```go
// GET /health
mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet { ... }
    resp := HealthResponse{...}
// GET /api/wechat/export?since=<epoch>&to=<epoch>
mux.HandleFunc("/api/wechat/export", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet { ... }
    query := r.URL.Query()
    since := int64(0)
    if sinceStr := query.Get("since"); sinceStr != "" { since, _ = strconv.ParseInt(...) }
    to := now
    if toStr := query.Get("to"); toStr != "" { to, _ = strconv.ParseInt(...) }
    msgs, _ := state.Store.QueryMessagesByRange(since, to)
    watermark := since
    for _, msg := range msgs { if msg.Ts > watermark { watermark = msg.Ts } }
    ...
```

**端点清单**：
| 方法 | 路径 | 参数 | 返回 |
|---|---|---|---|
| GET | `/health` | 无 | `HealthResponse` |
| GET | `/api/wechat/export` | `?since=<epoch秒>` `&to=<epoch秒>`（缺省 since=0, to=now） | `ExportResponse` |

**`HealthResponse` 字段**：`status, uptime_seconds, uds_connected, active_uds_connections, total_messages, total_received_stream, memory_bytes, version`。

**`ExportResponse` 字段（与 wechat-vault 兼容契约）**——逐字 struct tag：
```go
type ExportResponse struct {
    ExportedAt int64            `json:"exported_at"`
    Range      ExportRange      `json:"range"`        // {from,to}
    Watermark  int64            `json:"watermark"`
    Chats      []ExportChatItem `json:"chats"`
    Manifest   ExportManifest   `json:"manifest"`     // {sha256, message_count}
}
type ExportChatItem struct {
    ChatId      string              `json:"chat_id"`
    DisplayName string              `json:"display_name"`
    IsGroup     bool                `json:"is_group"`
    Messages    []ExportMessageItem `json:"messages"`
}
type ExportMessageItem struct {
    Ts     int64  `json:"ts"`
    Sender string `json:"sender"`
    Kind   string `json:"kind"`
    Text   string `json:"text"`
    Note   string `json:"note"`
}
```
→ 与报告 §3.3 给的示例 JSON 字段级一致（`chats[].name` 在报告示例中，代码实际是 `chat_id`+`display_name`+`is_group`；下游消费端需注意此差异）。

**启动参数（`main.go` flags）**：
```
-http  127.0.0.1:8765   (默认, 仅环回; 旧默认 :8765 全网卡监听, 已整改)
-db    /data/adb/wx_stream/vault.db   (Android 环境默认, root-only 目录 0700 / 文件 0600; 旧默认 /data/local/tmp/wx_vault.db)
-uds   @wechat_stream_ipc            (默认)
```

---

## A4. 部署形态原文（Magisk module / Watchdog / Tailscale）

**源码现状**：`/Users/<USER>/Projects/wx-pad-sniffer/` 下有 `daemon/`（Go，含 `build/` 已编译 ARM64 二进制）、`android-module/`（LSPosed 模块源码）、`deploy/`（报告 daemon_smoke_test.md 提及 `service.sh`/`watchdog.sh`/`setup_phone.sh`）。

- **报告 `route1_infra_pipeline.md` §3.1 原文**：
  > "架构选定：**选项 B**。将 Go 编译为静态二进制文件 `wx-daemon-aarch64`，直接打包为 Magisk / KernelSU / APatch 模块，在 `/data/adb/modules/wx_stream_daemon/service.sh` 中以纯后台守护进程启动。"
- **报告 §4.5 原文**："Tailscale 内网节点: 分配固定 IP (如 100.115.88.20:8765)"；§4.3："Watchdog: 30s 周期探测 mm 进程, 崩溃后 am start 自动唤醒"；§4.4："Doze & PPK 豁免: Android 12+ Phantom Process Killer 彻底解除"。
- **实测偏差（关键）**：`/data/adb/modules/` 当前**仅有** `zygisk_lsposed` / `zygisk_shamiko` / `zygisk_vector` / `mute_bootaudio` / `playintegrityfix` / `universal-gms-doze` —— **没有 `wx_stream_daemon` 这个 Magisk module**。即报告规划的"Magisk 自启 + Watchdog 自愈"**尚未部署**。当前 `wx_daemon` 是手动 `&` 拉起（见 `/data/local/tmp/daemon.log` 时间戳 17:18，仅一次，无自启）。

---

# B. 代码产物清单（Mac 路径 / 设备路径 / 是否存在）

## B1. `wx_daemon` 源码在哪？还是只有二进制？
- **源码在 Mac 上**：`/Users/<USER>/Projects/wx-pad-sniffer/daemon/`
  - `main.go`（flags/UDS/HTTP 启动）、`api.go`（路由+结构体）、`normalizer.go`（字段清洗，映射 type→kind）、`normalizer_test.go`、`go.mod`/`go.sum`
  - `build/wx_daemon` = **编译好的 ARM64 二进制**（与设备侧 `/data/local/tmp/wx_daemon` 同源）
- **设备侧产物**：`/data/local/tmp/wx_daemon`（9.7MB，ELF 64-bit LSB arm64, static, BuildID=d8fcd4be…）+ `/data/local/tmp/wx_vault.db`（28KB，当前空；注：此为新加固前旧二进制创建的库，新源码默认已改为 `/data/adb/wx_stream/vault.db`，设备侧尚未重部署，新默认值待部署后生效）。
- **结论**：**既有完整 Go 源码 + 已编译 ARM64 二进制**，不是"只有二进制"。

## B2. Hook 模块（喂 UDS 的那个 LSPosed 模块）是否已存在产物？
- **源码在 Mac 上（完整 Kotlin）**：`/Users/<USER>/Projects/wx-pad-sniffer/android-module/app/src/main/java/com/wx/sniffer/`
  - `XposedInit.kt`、`hook/DexKitSniffer.kt`、`ipc/RingBufferSender.kt`
  - `assets/xposed_init` = `com.wx.sniffer.XposedInit`
  - `build.gradle.kts` / `AndroidManifest.xml` 齐全
- **设备侧 APK 产物**：`/data/local/tmp/` 下有 `WeChatPad.apk` / `WeChatTablet.apk` / `LSPosed.zip`，但 **未找到 `wx-pad-sniffer` 模块自己的编译 APK**（grep/ls 无 `sniffer*.apk`）。即 **Hook 模块源码存在、尚未编译安装到这台设备**。
- **与已装模块关系**：当前 user 999 登录走的是 `com.roro.tablethook`（TabletHook，见 SUCCESS_SUMMARY.md），**不是** 这个 `wx-pad-sniffer` 模块。两者职责不同：tablethook=平板伪装登录，wx-pad-sniffer=消息抓取+UDS 推送。

## B3. daemon HTTP 端点现在能否实测到？（原文回显）
```bash
adb shell "curl -s http://127.0.0.1:8765/health; echo; curl -s http://127.0.0.1:8765/api/wechat/export | head -c 400; echo"
```
**实测返回（原文）**：
```
{"status":"ok","uptime_seconds":3987,"uds_connected":false,"active_uds_connections":0,"total_messages":0,"total_received_stream":0,"memory_bytes":498808,"version":"1.0.0-pad"}
{"exported_at":1790965509,"range":{"from":0,"to":1790965509},"watermark":0,"chats":[],"manifest":{"sha256":"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945","message_count":0}}
```
→ **端点完全可用**（HTTP 服务 live，版本 `1.0.0-pad`）。但 `uds_connected:false`、`total_messages:0`、`chats:[]`：**UDS 上游无任何生产者连入，库为空**。

---

# C. 三项"一票否决"级技术论断核实

## C1. 微信 8.0.78 的 DB 密钥派生：`MD5(IMEI+UIN)[0:7]` 是否仍有效？
**设备探测**：
```bash
# shared_prefs 列表（user 999）：无 system_config_prefs.xml；仅 Matrix_*/TP_DRM/WLOGIN/com.tencent.mm_preferences.xml 等
# grep -aoE "[0-9a-f]{7}" system_config_prefs.xml → 无输出（该文件在此版本不存在）
```
→ **旧版 7 位 MD5 密钥缓存文件 `system_config_prefs.xml` 在 8.0.78 / user 999 下不存在**，无明文密钥痕迹。

**报告论断逐字引用**：
- `hook_routes.md` §二.(2) 旧版："`Key = MD5(IMEI + UIN)[0:7].toLowerCase()`（7 位字符串）。在 Android 10+ 无法读取真实 IMEI 时，微信改用 `getAndroidId()` 或固定常量作为 Salt。"
- `hook_routes.md` §二.(2) 新版（WCDB 2.0）："全面转向微信自研开源的 **WCDB 2.0+**，采用多重密钥派生与动态 Page Header 加密，放弃了简单的 7 位 MD5 规则，改为从安全硬件/KeyStore 生成随机 Salt，并在内存中动态派生 32 字节原始密钥。……**在 Root 环境下，目前唯一稳定提取新版密钥的方法是从运行中的 `com.tencent.mm` 进程内存中读取 `sqlite3_key` 句柄指针**。"
- `technical_eval_3routes.md` §2.2：Android 新版"引入了页级动态压缩（ZSTD Dict compression）、私有加密头以及拆库分片"。

**结论**：`MD5(IMEI+UIN)[0:7]` **对已装 8.0.78 失效**；纯离线读库（不经内存取密钥）**已不可行**，必须运行时从 `com.tencent.mm` 进程内存取密钥（或干脆走 Hook 进内存路线，本工程正是后者——根本不解密 DB，Hook WCDB 插入拿明文）。

## C2. 既有 frida-server 是否在跑、版本、能否 attach 微信？
**实测**：
```bash
adb shell "su -c 'ps -A | grep -i frida'"  →  无输出（进程未运行）
```
- 设备 `/data/local/tmp/` 有 `frida-server` 二进制（第一轮 B3 确认），但**当前未在运行**。
- Mac 侧未探测 `frida --version`（不在本次只读范围；且本工程不依赖 frida——取密钥/Hook 用的是 LSPosed 模块而非 frida）。
- **结论**：frida-server 仅作备用取证工具，**不在本工程主链路**；当前未运行不影响 `wx-pad-sniffer` 方案。

## C3. `com.artemis.helper` 在 127.0.0.1:18888 是什么？
**实测（仅查询，未操作）**：
```bash
pm list packages | grep artemis  →  package:com.artemis.helper
dumpsys package com.artemis.helper | grep -iE 'versionName|firstInstall'
  versionName=1.2.0
  firstInstallTime=2026-09-26 15:28:49
pm path com.artemis.helper
  package:/data/app/~~o-h8KkbPqo8XO2ts5nDeSA==/com.artemis.helper-PIIAsNbh64XiA3mzpcr0Aw==/base.apk
```
- 监听 `127.0.0.1:18888` 的 `com.artemis.helper`（PID 4779），version 1.2.0，安装时间 2026-09-26，**与微信抓取/本工程无关**（名字 `artemis` 与 `/Users/<USER>/Projects/artemis` 目录同源，疑似另一套安卓多模态/设备管理工具）。
- **结论**：**非本项目组件**，不相关；按铁律未触碰。

---

# 最后一句话：接线点 & "接线即可"还是"需新写 Hook 模块"

**接线点（精确坐标）**：
- **API 面**：`GET http://127.0.0.1:8765/health` 与 `GET http://127.0.0.1:8765/api/wechat/export?since=&to=` —— 源码已默认仅绑 `127.0.0.1`（环回），需局域网/远端访问须显式 `-http 0.0.0.0:8765` 并自担风险；已在设备实测 `/health` 可用，字段契约见 A3，下游 Muse agent 可直接消费。
- **IPC 面（断点所在）**：抽象 UDS `@wechat_stream_ipc`，**Go 守护进程（server）已 listen 并等待 JSON 行**；缺的是 **client 侧**——即微信进程内的 LSPosed 模块把消息 `put(...)` 成 JSON 行 `write+newLine+flush` 进来。
- **Hook 触点**：`com.tencent.wcdb.database.SQLiteDatabase` 插入（主）+ Android `SQLiteDatabase`（兜底），范围 `com.tencent.mm` 主/:push/:tools。

**`wx_daemon` 是"接线即可"还是"需新写 Hook 模块"？**
- **`wx_daemon`（Go 守护进程）：已基本"接线即可"** —— 源码与 ARM64 二进制齐备，已在手机 8765 端口运行，HTTP/存储/UDS 监听全通，仅差上游连入。
- **但采集端（Hook 模块）目前是"源码就绪、未编译未安装、未接线"**：`wx-pad-sniffer/android-module` 的 Kotlin 源码完整，但本机**没有它的编译 APK**，且当前 user 999 微信挂的是 `com.roro.tablethook`（只做平板登录，不抓消息、不连 UDS）。

→ **结论：不是"从零新写"，而是"编译并安装既有 Hook 模块 + 把它接入已运行的 daemon"**。工作量=① `gradle assemble` 出 `wx-pad-sniffer` APK；② 作为 LSPosed 模块装到 user 999 并加作用域；③ 在 Vector/LSPosed 中启用；④ 起 Tailscale/穿透让云端 agent 访问 8765。守护进程侧零新代码。

**纯离线读库已不可行**（C1）：8.0.78 用 WCDB 2.0 动态密钥，`MD5(IMEI+UIN)[0:7]` 失效，无内存取密钥无法离线解密——这也正是本工程走"Hook 进内存拿明文、经 UDS 推给 daemon"路线的根本理由。
