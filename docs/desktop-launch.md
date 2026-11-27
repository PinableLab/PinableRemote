# PinableDesktop launch contract

PinableRemote remains a separately runnable native application. Launch the
packaged executable by its verified absolute path, without a shell and without
a host argument. Starting the viewer must not start a remote session.

- Set `PINABLE_REMOTE_DESKTOP=1` in the child environment. Managed launches show
  the PinableRemote title and do not register the upstream MyVNC updater.
  PinableDesktop owns updates of any copy it ships.
- Pass the current `PINABLE_THEME_MODE=system|light|dark`. This is applied before
  the window opens. A launch override is not saved as a preference; an explicit
  change in the viewer’s settings is saved. Standalone launches ignore the
  environment theme override.
- Ship the viewer as a distinct executable alongside Desktop, not as a Go
  import in Desktop. Retain the [upstream license](../LICENSE),
  [third-party notices](../README.third-party.md), and corresponding-source access
  for the exact distributed build.
- The managed GUI launch does not inherit Desktop credentials or authorize
  task access. The old standalone Agent (MCP) mode was removed. Existing
  external clients should remove their `--mcp` integration; this argument
  is rejected before any viewer or connection starts. The planned, separate
  Desktop task integration is not part of this GUI launch contract.

## Desktop integration checklist (not yet implemented here)

1. Build from a pinned PinableRemote revision. Record the revision and license
   in the package inventory and include the corresponding source and build
   scripts, or provide the applicable source-distribution path.
2. Install the bundled viewer as a separate managed executable. Match the
   installed bytes against the embedded asset before launch. Do not use PATH
   or download a binary in response to a status-bar click.
3. Place an icon-only PinableRemote action before PinableWeb in Desktop’s
   bottom status bar, with an accessible name and hover label. Hide it on
   unsupported targets. The initial release targets are Windows amd64 and
   macOS arm64; validate them independently.
4. On each supported platform, verify click-to-open, no automatic remote
   connection, all three initial theme modes, closing and reopening, and
   the error shown when the installed binary is missing or was altered.
