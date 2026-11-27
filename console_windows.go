// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"syscall"
)

// attachConsole makes early command-line errors visible when the GUI
// executable is run in a terminal. It preserves inherited streams and
// never creates a console window.
func attachConsole() {
	invalid := func(id int) bool {
		h, err := syscall.GetStdHandle(id)
		return err != nil || h == 0 || h == syscall.InvalidHandle
	}
	in, out, errh := invalid(syscall.STD_INPUT_HANDLE), invalid(syscall.STD_OUTPUT_HANDLE), invalid(syscall.STD_ERROR_HANDLE)
	if !in && !out && !errh {
		return
	}
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	k32 := syscall.NewLazyDLL("kernel32.dll")
	attach, setStd := k32.NewProc("AttachConsole"), k32.NewProc("SetStdHandle")
	if r, _, _ := attach.Call(attachParentProcess); r == 0 {
		return // no parent console: leave the streams as they are
	}
	reopen := func(name string, id int, f **os.File) {
		h, err := syscall.CreateFile(syscall.StringToUTF16Ptr(name), syscall.GENERIC_READ|syscall.GENERIC_WRITE,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
		if err != nil {
			return
		}
		setStd.Call(uintptr(id), uintptr(h))
		*f = os.NewFile(uintptr(h), name)
	}
	if in {
		reopen("CONIN$", syscall.STD_INPUT_HANDLE, &os.Stdin)
	}
	if out {
		reopen("CONOUT$", syscall.STD_OUTPUT_HANDLE, &os.Stdout)
	}
	if errh {
		reopen("CONOUT$", syscall.STD_ERROR_HANDLE, &os.Stderr)
	}
}
