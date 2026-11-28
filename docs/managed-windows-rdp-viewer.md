# Managed Windows RDP viewer candidate

This is an opt-in, read-only FreeRDP backend for the existing managed RDP
broker. The regular Remote application and Desktop do not launch or advertise
it. No new installer or network-accessible service is introduced.

The backend requires a pre-enrolled host, port, Windows domain and account,
and a CA certificate selected by the calling application. It will not use
an unknown or system-default CA, and refuses an input-enabled lease. It
permits one NLA connection only: no server redirects, automatic reconnect,
gateway, fallback, clipboard, drives, or global desktop capture. A frame
requires a verified endpoint, a named logon for the enrolled account, and a
painted in-session GDI surface. A resize invalidates the frame until a new
paint. Keys and pointer are intentionally not implemented here.

## Windows build and tests

Use a trusted UCRT64 FreeRDP 3.33.1 SDK and its matching OpenSSL runtime.
The optional tests need GCC, the import libraries, and matching DLLs on
PATH. Do not use loose DLLs from untrusted locations in an installer.

```powershell
$sdk = 'C:/msys64/ucrt64'
$env:Path = "$sdk/bin;$env:Path"
$env:CC = "$sdk/bin/gcc.exe"
$env:CGO_ENABLED = '1'
$env:CGO_CFLAGS = "-I$sdk/include/freerdp3 -I$sdk/include/winpr3 -I$sdk/include"
$env:CGO_LDFLAGS = "-L$sdk/lib"
go test -tags pinable_freerdp ./internal/managedrdp
```

The same callback implementation is covered by
`tools/managed-rdp/native-fixture.c`. Run it in a UCRT64 shell with
a short-lived test CA and a serverAuth certificate with DNS SAN
`server-fixture.example.test` signed by that CA:

```sh
gcc -D__STDC_NO_THREADS__=1 -Wall -Wextra -Werror -Wno-deprecated-declarations \
  -Wno-unknown-pragmas tools/managed-rdp/native-fixture.c \
  $(pkg-config --cflags --libs freerdp3 openssl) -lws2_32 -o /tmp/pinable-rdp-fixture.exe
/tmp/pinable-rdp-fixture.exe root.pem leaf.pem
```

These tests do not connect to a desktop. Before making this available in
Desktop, verify a trusted Windows target, rejection of the wrong certificate
and account, revocation while idle and mid-handshake, and behavior on resize
and server disconnect. Verify real Windows logon announcements before treating
a server as compatible. Missing named-logon evidence leaves capture disabled.
