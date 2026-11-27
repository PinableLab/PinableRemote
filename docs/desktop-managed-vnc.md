# Desktop-managed Mac Screen Sharing: restricted VNC adapter

This change supplies an internal Go adapter and a private macOS child entry.
It is not a listener, MCP tool, or enabled Desktop product capability. The ordinary interactive VNC/ARD
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

Desktop may start the verified macOS build with the fixed internal argument
`--internal-desktop-vnc-v1` only after the user approves this task's request.
The child requires anonymous stdin/stdout and a separate inherited anonymous
pipe (descriptor 3). The latter carries one bounded JSON object with
`host`, `port`, `server_name`, `spki_sha256` (64 hex characters) and
`require_screen_privacy`. Desktop closes this descriptor after writing.
A missing or invalid bootstrap, a request for privacy that cannot be
established, or an unsupported operating system does not start the viewer
or dial the target. The existing system screen-privacy preference must
be included in the caller's decision and cannot be relaxed for a task.

Stdin/stdout reuse the [version 1 private wire](desktop-managed-rdp.md):
exactly one `open` binding is accepted, and its host and port must match the
enrolled identity. The VNC password is confined to this binding, never the
command line, environment, bootstrap, audit, or model context. Status
becomes authenticated only after TLS identity and VNC authentication.
Input additionally requires a separate, explicit grant and a fresh frame.
This is not a real Mac acceptance claim; do not expose the product entry
until the approved Mac, trusted relay and installed binary pass testing.

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
