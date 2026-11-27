# Desktop-managed Mac Screen Sharing: restricted VNC adapter

This change supplies an internal Go adapter, not a command, listener, MCP
tool, or enabled desktop capability. The ordinary interactive VNC/ARD
viewer retains its behavior. Desktop must authorize the target and
per-task lease locally before opening a private, dedicated child and
close the child on revocation. An agent must not receive this pipe,
network credentials, relay configuration, or a raw connect operation.

## Supported deployment

Only a **private, authenticated TLS relay** that is preconfigured to
forward to one approved Mac Screen Sharing service is supported.
On the approved Mac, enable Screen Sharing's opt-in VNC password mode;
use a unique random 8-character printable ASCII password (no spaces).
This is the VNC password, not a macOS account login. The Mac's direct
VNC/RFB listener must not be reachable outside the approved path. Run the
relay on the same Mac and bind its upstream to the local Screen Sharing
service; restrict relay ingress to authorized Desktop hosts. Do not expose either
service to a public network. RFB's classic VNC authentication is not
sufficient to protect an unauthenticated network.

The relay must present a currently valid certificate for the enrolled TLS
server name from the workstation's trusted CA store. Desktop must verify
and enroll its leaf public key independently. The adapter requires both
normal TLS chain and hostname validation **and** the pre-enrolled SHA-256
of the certificate's DER SubjectPublicKeyInfo. A certificate mismatch
fails closed. No trust-on-first-use, trust-all, plaintext, generic proxy,
or Apple ARD account fallback. DNS may resolve to RFC 1918 private IPv4 or
IPv6 ULA only; public, loopback and link-local addresses are refused.

The RFB client selects only classic VncAuth inside the authenticated tunnel.
If the server advertises the Apple protocol version, we request standard
RFB 3.8; Apple-specific extensions, Apple account login and ARD-only
servers are not supported. Applicability to a real Mac and any required
Mac settings are still subject to native acceptance.

## Private integration

The future fixed VNC child entry point (not agent-selected) calls:

```go
managedvnc.Serve(ctx, inheritedIn, inheritedOut, managedvnc.Identity{
    Host: enrolledRelayAddress,
    Port: enrolledRelayPort,
    ServerName: enrolledTLSName,
    SPKISHA256: enrolledLeafSPKIHash, // [32]byte, decoded from trusted records
})
```

The parent reads those fields from approved device configuration and passes
them privately to the fixed child (for example through a separate inherited
bootstrap pipe); none belong in shell arguments,
agent-visible settings, logs or files. Reuses the [version 1 newline-JSON
private wire](desktop-managed-rdp.md): exactly one `open` may provide a
binding. Its `host` and `port` must exactly match the approved relay;
`user` is the literal `vnc` and `password` is the enrolled VNC password
(inherited pipe only). Status becomes authenticated only after both TLS
identity and VNC authentication succeed. Capture and input require a
live session; input further requires explicit authorization and a fresh
nonreusable frame ID.

A screenshot asks for a full raw RFB framebuffer from that authenticated
session. Partial frames, unsolicited updates and resize are rejected; the
session does not use global display APIs. Maximum framebuffer dimension is
4096 on each axis and 8 Mi pixels total. JPEG results are capped at 4 MiB.
Actions are bounded click, a named key or printable character, and
printable text as individual key strokes. No key chords, clipboard,
files, high-performance ARD streams, shell or audio.

## Validation and release gate

Tests use an in-memory TLS/RFB fixture with an ephemeral test CA and
exercise authentication, pin/name/CA mismatches, refusal of None and
Apple-only auth, exact pipe screenshots/input, and rejection of stale
frames, unsafe endpoints, overlapping partial frames, clipboard protocol
traffic, and revoked operations.
No real Mac was connected. Before exposing the option in the Desktop,
verify trusted relay enrollment, native Mac VNC compatibility (including
the protocol version negotiation and current display size), visible
session markers bound to two different approved endpoints, read-only
leases, status after disconnect, and revocation in the Desktop task flow.
