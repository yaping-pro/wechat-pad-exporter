# 网络配置与安全暴露指引 (Networking & Port Forwarding)

## 1. 核心安全铁律 (Loopback & Token Security)

为了防止个人即时通讯数据在网络中意外泄露，`wechat-pad-exporter` 恪守严格的安全设计：
- `wx_daemon` **默认且推荐**仅监听 Android 设备的本地回环地址 `127.0.0.1:8765`。
- 严禁在未经加密隔离的公共局域网中配置 `bind: "0.0.0.0"`。
- 所有 API 请求均受 Bearer 令牌保护。

---

## 2. 访问方案指南

### 方案 A：USB / 本地 ADB 端口转发（最推荐、零暴露）
如果 Android 设备通过 USB 数据线连接，或通过内网无线调试（`adb connect <DEVICE_IP>:5555`）连接：

在工作站或个人服务器执行端口映射：
```bash
adb forward tcp:8765 tcp:8765
```
随后客户端直接访问本地地址：
```bash
curl -H "Authorization: Bearer <YOUR_TOKEN>" http://127.0.0.1:8765/health
```

### 方案 B：加密点对点虚拟网络 (Tailscale / WireGuard)
如果设备位于异地，需要跨广域网访问：
1. 可以在 Android 设备上通过 VPN 工具连接私有虚拟专用网（如 Tailscale）。
2. 在 `config.json` 中将 `bind` 配置为 Android 设备的专用内网 IP（例如 `<TAILNET_IP>`）。
3. 消费端配置对应内网端点：
   ```bash
   export WX_EXPORT_ENDPOINT="http://<TAILNET_IP>:8765/api/wechat/export"
   export WX_DEVICE_TOKEN="your_token"
   ```

### 方案 C：SSH 本地端口映射 (SSH Tunneling)
若工作站与设备挂载在同一台家庭网关 / Linux 服务器后方：
```bash
ssh -L 8765:127.0.0.1:8765 user@<HOMELAB_HOST>
```

---

## 3. 双端点与家庭中继拓扑注意 (Dual-Endpoint Topologies)

在部分复杂网络架构中，用户可能会配置**主端点（手机设备端点）**与**备用中继端点（家庭服务器缓存端点）**：
- **手机设备直连端点 (Primary Device Endpoint)**：`<DEVICE_HOST>:8765`，直接读取手机上运行的 `wx_daemon`。
- **家庭回退端点 (Fallback Home Relay)**：`<HOMELAB_FALLBACK_HOST>:8765`，当手机处于弱网或断线时，消费端可从家庭服务器的本地备份或中继拉取历史归档。
- 请在客户端配置中分别设置对应的独立 Token，确保权限最小化隔离。
