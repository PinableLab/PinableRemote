# 隐私屏幕

Remote 打开 **设置 → 连接 → 要求隐私屏幕**，用于新连接。默认关闭；该选项不会修改远端系统设置，不会改变已有连接，也不传给智能体通道。

| 连接方式 | 行为 |
| --- | --- |
| Mac（Apple Remote Desktop） | 使用 Mac 的系统幕帘。只有服务端报告支持而且当前会话已离开实体控制台，Remote 才进入桌面；不支持、未确认或保护丢失，都不会退回普通 VNC。这个模式暂时关闭高性能串流和动态分辨率。 |
| Windows RDP | 沿用 Windows 原生远程会话，不切换到实体控制台，也不从客户端发送锁屏/解锁按键。Remote 无法读取 Windows 实体屏幕的锁定状态，不能将 RDP 连接状态当作验收结果。 |
| 其他 VNC（含 Windows VNC） | 通用 RFB 不提供可验证的实体屏幕遮蔽。要求隐私屏幕时拒绝连接。 |

Mac 请在 **系统设置 → 通用 → 共享 → 远程管理** 中只给指定账户控制权限。不要把关闭显示器、客户端黑屏或没有画面当成隐私屏幕。Remote 在断开时不会主动解除幕帘；最终实体屏幕状态由 macOS 负责。

## 真机验收（测试账号和无敏感内容的窗口）

1. Mac：用外部相机拍实体屏幕。启用选项后连接，确认本机只显示系统锁定画面，客户端仍可操作。测试普通屏幕共享、未给控制权限及保护被本机解除；不能继续操作且不能静默回退。
2. Mac：分别关闭标签、退出 Remote、断网。相机逐帧检查本机不会短暂显示已登录桌面。记录 macOS 版本、Apple Silicon/Intel、内外接显示器及初始锁定状态。
3. Windows：使用标准 Windows RDP（不使用共享控制台），确认连接中与断开后实体屏幕不显示远程操作的桌面。测试普通 Windows VNC 在开启选项时被拒绝。

参考：[Apple Remote Desktop 幕帘说明](https://support.apple.com/guide/remote-desktop/apd2450a787/mac) · [Microsoft 的 RDP 与实体控制台区别](https://learn.microsoft.com/windows/win32/termserv/consoles-vs-terminals) · [DisplayInfo2 会话标志的公开协议实现](https://github.com/kitknox/rootshell-vnc/blob/93d900fdebafef20239df732f0a72cb071b29796/Sources/RFBTransport/Session/TransportSession.swift)。
