# Private Windows task viewer protocol

Build this internal Windows executable with the same FreeRDP SDK and
`pinable_freerdp` tag as the native viewer tests. This is not the regular
PinableRemote executable, and is not installed or enabled by this change.

Desktop starts it only after local confirmation for the selected task and
device. Desktop is responsible for installing a trusted helper and DLL bundle
and resolving the registered endpoint, trust anchor, and credentials locally.
Never pass these values through model tools, command-line arguments or logs.

Standard input and output must be inherited pipes. The first input line is
a bounded JSON object containing `host`, `port`, `domain`, `user`, and
`ca_pem` (JSON base64 of the PEM trust anchor). The bootstrap contains no
password. After that line, the existing `managedrdp` broker protocol starts;
its single `open` request must match the bootstrap and have input disabled.
The child has no listener. Closing the corresponding Desktop task must
revoke the pipe and terminate the helper.

This only defines the private transport. Desktop must not advertise a task
RDP target until its installer, enrollment, approval and task lifecycle are
wired. The helper does not perform keyboard or pointer operations.
