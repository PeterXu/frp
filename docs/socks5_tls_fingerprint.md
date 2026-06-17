# SOCKS5 TLS Fingerprint

## 什么是 TLS Fingerprint

TLS ClientHello 包含客户端特征，服务端通过这些特征识别客户端类型：

```
TLS ClientHello 内容：
├── Version (TLS 1.2/1.3)
├── Cipher Suites (加密套件顺序)
├── Extensions (扩展列表顺序)
├── Supported Groups (曲线：X25519, P256...)
├── Signature Algorithms (签名算法)
├── ALPN (h2, http/1.1)
└── Key Share (密钥共享组)
    ↓
生成 JA3/JA4 指印（MD5 hash）
```

**示例 JA3/JA4**：

| 客户端 | JA3 Hash |
|--------|----------|
| Chrome | 随版本变化 |
| Firefox | 随版本变化 |
| Go (net/http) | 固定，易被识别 |
| Node.js 24.x | 44f88fca027f27bab4bb08d4af15f23e |

---

## SOCKS5 代理链路中的 TLS Fingerprint

```
链路分析：

Firefox (TLS握手) → frpc1 → frps → frpc2 → 目标服务器
        ↑
   Firefox 的 TLS ClientHello
   透明传输，不被修改
```

**结论**：

| 场景 | TLS 握手位置 | fingerprint |
|------|--------------|-------------|
| 无 outboundProxy | Firefox → 目标 | Firefox ✓ |
| HTTP/SOCKS5 outboundProxy | Firefox → 目标 | Firefox ✓ |
| HTTPS outboundProxy | frpc2 → 代理 | frpc2 (Go) ⚠️ |

**只有 HTTPS outboundProxy 场景需要 TLS fingerprint 模拟！**

---

## 检测维度分析

| 维度 | 透明代理情况 | 检测风险 |
|------|--------------|----------|
| TLS fingerprint | Firefox 的指印 | ✓ 不被检测 |
| 出口 IP | frpc2 服务器 IP | ⚠️ 高（数据中心标记） |
| HTTP Headers | Firefox 的配置 | ⚠️ 可能与 IP 位置不一致 |
| TCP 参数 | Go runtime 默认 | ⚠️ 可能异常 |
| RTT 延迟 | 多层转发延迟 | ⚠️ 可能偏高 |

---

## 解决方案

### 方案 1：TLS Fingerprint 模拟（已实现）

**适用场景**：使用 HTTPS outboundProxy

```toml
[[proxies]]
name = "relay"
type = "socks5_relay"
outboundProxy = "https://proxy.example.com:443"
tlsFingerprint = "chrome"
```

**支持的指纹预设**：

| 名称 | 说明 |
|------|------|
| `chrome` | Chrome 浏览器指纹 |
| `firefox` | Firefox 浏览器指纹 |
| `safari` | Safari 浏览器指纹 |
| `node` | Node.js 24.x 指印 |

**效果**：frpc2 → HTTPS proxy 使用指定浏览器 TLS 指印

---

### 方案 2：家庭 IP + 用户配置（推荐）

**适用场景**：frpc2 运行在家庭网络

```
配置：
├── frpc2: 家庭 IP 出口（解决 IP 检测）
├── Firefox: 语言/地区与家庭 IP 位置匹配（解决 Headers）
└── TLS: Firefox 指印自动使用（无需额外处理）
```

**优势**：
- 不需要修改 frp 架构
- 解决主要检测点（IP + Headers + TLS）
- 实现简单

---

### 方案 3：HTTPS 中间人模式（未实现）

**适用场景**：需要 frpc2 控制 TLS + HTTP Headers

```
架构改变：
Firefox → frpc2(中间人) → 目标
            ↓
        解密 HTTPS
        修改 Headers
        使用自定义 TLS fingerprint
        重加密发送
```

**复杂度**：高，需要：
- 证书信任问题
- 解密/重加密逻辑
- 性能影响

---

## 实现技术

### 库

使用 `github.com/refraction-networking/utls` 实现 TLS fingerprint 模拟。

参考项目：
- sing-box: `common/tls/utls_client.go`（预设浏览器指纹）
- sub2api: `backend/internal/pkg/tlsfingerprint/dialer.go`（自定义 Profile）

### 核心代码

```go
import utls "github.com/refraction-networking/utls"

// 使用预设指纹
tlsConn := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloChrome_Auto)

// 使用自定义 Profile
spec := buildClientHelloSpecFromProfile(profile)
tlsConn := utls.UClient(conn, &utls.Config{ServerName: host}, utls.HelloCustom)
tlsConn.ApplyPreset(spec)
```

### frp 实现

- `pkg/util/tlsfingerprint/dialer.go` - TLS fingerprint 核心
- `client/proxy/socks5_relay.go` - HTTPS outboundProxy 时应用

---

## 建议

| 场景 | 建议方案 |
|------|----------|
| frpc2 在家庭网络 | 方案 2（无需 TLS fingerprint 功能） |
| frpc2 使用 HTTPS outboundProxy | 方案 1（已实现） |
| 需要完全控制出口特征 | 方案 3（需讨论是否实现） |

**结论**：当前 TLS fingerprint 实现已满足 HTTPS outboundProxy 场景。对于大多数用户，方案 2（家庭 IP + 配置匹配）是最佳选择。