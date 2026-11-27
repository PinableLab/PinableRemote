# Desktop managed RDP: private broker and driver acceptance

This work adds an internal broker and synthetic driver tests only. It **does not
enable managed RDP**, advertise a CLI capability, or change the user-operated
RDP viewer or raw MCP server. Do not route agent traffic directly into the
broker. Desktop remains responsible for per-task authorization, approved-device
lookup, credentials, allowlisted actions, frame provenance in its own scope,
and killing the child on revocation.

## Private broker contract, version 1

A future verified driver host should use inherited stdin/stdout only. There is
no listener. Send one JSON object per newline, waiting for the response before
sending the next. Keep the request pipe open until the final response; EOF
revokes the session even if the driver is still in the handshake. IDs must
strictly increase, and the first line is the only request that can contain
endpoint, credential, or input scope:

```json
{"id":1,"op":"open","binding":{"host":"authorized.example.test","port":3389,"user":"tester","password":"<test value>","ttl_seconds":900,"allow_input":false}}
{"id":2,"op":"status"}
{"id":3,"op":"screenshot"}
{"id":4,"op":"disconnect"}
```

An `open` acknowledgement only means the driver started; readiness is
reported by `status`. Responses have `id`, `ok`, an optional stable `error`
code, and either `status` or `screenshot`. Raw backend errors, host/account
and credentials are not echoed. Real credentials must not travel in process
arguments, agent environment, agent-visible logs, audit records, or files.

- `status` reports `connected`, `authenticated`, `screenshot` and `input`;
  only an authenticated live session gets frame or input capabilities.
- `screenshot` returns `{id, data, mime, width, height}` under the response
  `screenshot` field. `data` is Base64 for bounded JPEG/PNG from the authenticated
  RDP framebuffer. `id` is the request ID within this child. A header dimension
  mismatch or unsupported format is rejected.
- With separately approved input, an `input` request uses the most recent
  screenshot ID, for example:
  `{"id":5,"op":"input","input":{"frame_id":4,"kind":"click","x":16,"y":12,"button":"left"}}`.
  The other supported shape is `{"frame_id":4,"kind":"key","key":"Return"}`
  or `{"frame_id":4,"kind":"text","text":"fixture"}` as its input object.
  The backend must target the same session, never the machine desktop.
  A frame grant expires after 20 seconds and is consumed on an attempted
  driver input. Another action requires another screenshot. Coordinates are
  in image pixels, not local screen coordinates.
- Rebinding, non-monotonic IDs, parent EOF, explicit disconnect, cancellation,
  or lease expiry close the child session. No clipboard, local/remote file,
  raw event, OS shell, display-management, or VNC fallback is exposed.

## Production driver gate

The existing Microsoft ActiveX viewer embeds a Windows HWND inside MyGo. Its
current connection state is not proof of authenticated logon or a clean
in-session frame. Microsoft documents
[SendKeys](https://learn.microsoft.com/en-us/windows/win32/termserv/imsrdpclientnonscriptable-sendkeys)
for scancodes but it does not provide a complete session-bound mouse and
frame interface. The documented [RemoteDesktopClient snapshot interface](https://learn.microsoft.com/en-us/previous-versions/hh448581(v=vs.85))
belongs to a different control. Do not substitute global screenshot, window
message injection, or global keyboard/mouse APIs.

A candidate is a dedicated FreeRDP driver. Upstream's
[client sample](https://github.com/FreeRDP/FreeRDP/blob/master/client/Sample/tf_freerdp.c)
shows the software GDI and `EndPaint` hooks. The per-session
[framebuffer](https://github.com/FreeRDP/FreeRDP/blob/master/include/freerdp/gdi/gdi.h)
provides width, height, stride and pixels; [input](https://github.com/FreeRDP/FreeRDP/blob/master/include/freerdp/input.h)
uses the associated `rdpInput` context. This is an API feasibility finding, not
an integration or runtime dependency. Use a pinned reviewed build, prove
certificate/host identity and NLA (no trust-all, anonymous, or legacy fallback),
and force off auto-reconnect, clipboard, drive, device, smart card, and
other redirection channels. Disable broker capability until a native driver
validates that snapshots follow authenticated logon, resize invalidates
prior coordinates, and all input stays in that session. Care is required
when adapting the sample: it sets a parser-only decoding flag that would
prevent its intended use as a screenshot driver.

Before publishing: test with owned synthetic Windows endpoints, including
wrong certificates, credential failure, channel negotiation, two simultaneous
sessions with visible test markers, context cancellation during dial and
frame work, and revocation with a queued input. No real endpoint was used
for the broker tests.
