# FreeRDP 3.33.1 compile-only feasibility

Experimental Linux fixture for the proposed managedrdp.Driver native seam, not a production driver or broker daemon. No new published dependency and no real RDP target. The private Go broker remains independently tested and guarded; this fixture does not wire its adapter.

1. Prerequisites: Linux with seccomp, GCC, OpenSSL 3 development headers, CMake, Ninja, pkg-config, curl, tar and sha256sum. Copy this directory's source files into the probe/ directory of a writable empty folder.
2. Run bash probe/build-sdk.sh. It downloads the official immutable release, verifies the 3.33.1 release asset SHA-256, and builds under the folder.
3. Run bash probe/test.sh. The fixture compiles with warnings as errors except upstream deprecation annotations; its C code installs Linux seccomp blocking socket/connect before creating the library contexts. Expect a deliberate negotiation-error log from the overridden transport followed by PASS.

Verified: CA chain and exact SAN; rejection of untrusted CA, mismatched hostname/port, and forwarded/redirected identity; a named account fixture before first paint; padded surface stride copy without leaking another context; blocked transport callback receives approved endpoint even with HTTPS_PROXY; and context-owned input API linkage. Tested with Linux amd64 GCC, OpenSSL 3.5.9, workspace-only Debian-archive-verified CMake 3.31.6/Ninja 1.12.1.

Not verified: actual NLA negotiation, event ordering and account/domain mapping on Windows; complete actual decoded frame/cursor and delivery of remote pointer/keys; redirect handling in a live server handshake; Windows packaging or trust path. Initialize real GDI only in PostConnect, after negotiated cache creation. The fixture supplies known in-memory synthetic pixels rather than faking a complete RDP connection.

Implementation notes observed in 3.33.1:
- ProxyType=0 (PROXY_TYPE_NONE) still inherits environment proxies. Require PROXY_TYPE_IGNORE and cleanse the child environment. Gate ConnectLayer and other applicable lower-level callbacks. TCPConnect alone does not gate default setup. Block every second connection/redirection and review fallback logs. Verify the approved endpoint and negotiated protocol before readiness.
- Set ExternalCertificateManagement, leave IgnoreCertificate off and the accepted-fingerprint list empty. Enforce validated leaf and full chain with no certificate writeback, and no host exceptions. Do not enable device, clipboard, disk, remote app, gateway, network proxy, or automatically reconnect.
- Require approved account AND domain/UPN family in production acceptance. The fixture tests exact account string and a synthetic domain. Plain-notification-only events remain unready. Transport connect never proves a logged-on session.
- Serialize event polling, GDI/copy/resize and input in one worker. Invalidate on resize, disconnect, revocation, reauthentication, or backend error. Encode a bounded PNG/JPEG from the freshest post-logon buffer only.

Upstream public release and source:
[FreeRDP 3.33.1](https://github.com/FreeRDP/FreeRDP/releases/tag/3.33.1) ·
[Transport API](https://github.com/FreeRDP/FreeRDP/blob/3.33.1/include/freerdp/transport_io.h) ·
[GDI API](https://github.com/FreeRDP/FreeRDP/blob/3.33.1/include/freerdp/gdi/gdi.h) ·
[Input API](https://github.com/FreeRDP/FreeRDP/blob/3.33.1/include/freerdp/input.h)
