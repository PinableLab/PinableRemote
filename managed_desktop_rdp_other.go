//go:build !darwin || !cgo || !freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

package main

// Builds without the macOS FreeRDP backend offer no task desktop over RDP;
// Windows Desktop uses its own pinable-rdp-view helper.
func runManagedDesktopRDPList(args []string) int { return 2 }
func runManagedDesktopRDP(args []string) int     { return 2 }
