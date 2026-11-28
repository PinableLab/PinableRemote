# macOS FreeRDP 后端（开发中）

## 当前边界

此后端在 PinableRemote 窗口内创建 Cocoa 子视图，使用 FreeRDP 协议线程接收 BGRA 帧并发送鼠标、扫描码键盘输入，不启动外部客户端。

独立构建需要启用 `freerdp` 构建标签和 cgo；默认纯 Go 构建仍使用不支持 RDP 的旧占位实现。Desktop 构建管线发现本地源码包含新后端时，会自动启用 FreeRDP，并要求显式提供静态 SDK，缺少依赖即失败，不静默降级。固定远端旧提交仍没有此后端，需要使用 `--local-repo`。**真实 Windows 连接及干净 macOS 分发仍待验收。**

已实现：固定远程尺寸、本地缩放、基础鼠标/滚轮/键盘、异步错误回传、关闭回收。尚未实现：动态远程尺寸、剪贴板、音频、磁盘重定向、IME 文本输入及完整系统快捷键捕获。当前禁用图形管线，使用 FreeRDP 软件 GDI 基础显示能力。

## 依赖

- FreeRDP **3.33.1**，Apache-2.0。
- 上游源码：`https://github.com/FreeRDP/FreeRDP/archive/refs/tags/3.33.1.tar.gz`
- SHA256：`6b6da1e7217f924b033da6896e70eb4c4a447c3900d03c0d10179062f0a7d495`
- OpenSSL 3、系统 zlib、Apple Cocoa 及 FreeRDP 所需系统框架。

不允许随意从 PATH 加载不明辅助程序，也不允许下载后执行安装脚本。

开发环境可在独立目录通过 CMake 构建 FreeRDP，关闭 client/server、channels、sample、FFmpeg、X11、Wayland 等不需要的功能。依赖不应安装到系统路径。使用 `BUILD_SHARED_LIBS=OFF`、`OPENSSL_USE_STATIC_LIBS=TRUE`，关闭 `WITH_JSON_DISABLED=ON` 对应的 JSON 支持及 `WITH_AAD=OFF`，防止意外引入主机 JSON 库。SDK 目录须为无空白的绝对路径，包含：

- `include/freerdp3`、`include/winpr3`
- `lib/libfreerdp3.a`、`lib/libwinpr3.a`、`lib/libssl.a`、`lib/libcrypto.a`
- `share/pinable-freerdp-version`，内容为 `3.33.1`

SDK 里的所有归档须与目标架构匹配。Desktop 构建会审计最终 Mach-O，只允许 `/usr/lib/`、`/System/Library/` 动态依赖。SDK 由开发者显式准备，当前不会自动下载或安装。

独立测试示例（将 `/absolute/sdk` 替换为实际路径）：

```bash
CGO_ENABLED=1 \
CGO_CFLAGS='-I/absolute/sdk/include/freerdp3 -I/absolute/sdk/include/winpr3' \
CGO_LDFLAGS='-L/absolute/sdk/lib -lssl -lcrypto -lz -framework Security -framework SystemConfiguration -framework Carbon -framework VideoToolbox -framework CoreMedia -framework CoreVideo' \
go test -tags freerdp ./...
```

构建应用使用相同环境以及 `go build -tags freerdp -o /absolute/output/pinable-remote .`。开发 SDK 路径不得写死进产品源码或发布产物。

Desktop 从仓库根启动时，在原有命令前增加 `PINABLE_REMOTE_FREERDP_SDK=/absolute/sdk`，保留 `--local-repo` 和自己的 `--pinable-home` 参数。此变量只用于本次构建，不修改系统环境。构建会更新 Desktop 的生成 wrapper 资产，不应提交这些二进制中间产物。

## 证书与密码

禁用裸 RDP 安全层，启用 TLS/NLA，明确要求证书验证。未通过 FreeRDP 信任校验时，在主线程显示原生确认框，默认取消；用户须通过独立可信渠道核对服务器 SHA256 指纹后才能允许本次连接。当前不持久化新指纹；若 FreeRDP 已知记录发生变化，直接拒绝，不降级为普通首次确认。

密码仅由现有凭据流程提供，复制进 FreeRDP 设置；不进入命令行、不写明文配置。协议线程不能直接访问 Cocoa，关闭后先标记取消并中止连接，再异步释放协议上下文。输入队列溢出时断开，不静默丢弃按键释放。

## 尚需验收

1. 使用 Windows 10/11 实机验证 NLA、错误密码和账户域。
2. 验证未知证书取消、确认、证书变化及连接中关闭窗口。
3. 验证画面颜色/方向、Retina 坐标、滚轮、修饰键、标签切换和断网。
4. 固定 SDK 的可复现构建和完整依赖来源校验，完成签名和许可证随包分发；当前已有显式静态 SDK 接入和最终 Mach-O 动态库审计，但没有自动构建 SDK。
5. 在不安装 FreeRDP/OpenSSL 的干净 macOS 上验收；Windows 原后端执行回归。

Go 自动测试仅验证现有逻辑和最小空视图生命周期，不证明真实 Windows 连接、原生交互和干净机器分发可用。
