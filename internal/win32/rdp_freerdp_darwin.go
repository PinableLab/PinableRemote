//go:build darwin && cgo && freerdp

package win32

/*
#cgo LDFLAGS: -lfreerdp3 -lwinpr3
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "rdp_freerdp_darwin.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// RDP 在主线程拥有原生视图；网络和解码在 FreeRDP 独立线程执行。
type RDP struct {
	view          unsafe.Pointer
	width, height int32
}

func NewRDP(parent uintptr) (*RDP, error) {
	p := C.pin_rdp_new(C.uintptr_t(parent))
	if p == nil {
		return nil, errors.New("创建 FreeRDP 原生视图失败")
	}
	return &RDP{view: p}, nil
}
func (r *RDP) Connect(host string, port int, user, password, domain string) error {
	if r.view == nil {
		return errors.New("RDP 视图已关闭")
	}
	if port < 1 || port > 65535 {
		return errors.New("无效 RDP 端口")
	}
	h, u, p, d := C.CString(host), C.CString(user), C.CString(password), C.CString(domain)
	defer C.free(unsafe.Pointer(h))
	defer C.free(unsafe.Pointer(u))
	defer C.free(unsafe.Pointer(p))
	defer C.free(unsafe.Pointer(d))
	w, height := r.width, r.height
	if w <= 0 || height <= 0 {
		w, height = 1280, 800
	}
	if w > 8192 || height > 8192 {
		return errors.New("RDP 桌面尺寸超过限制")
	}
	if C.pin_rdp_connect(r.view, h, C.int(port), u, p, d, C.int(w), C.int(height)) == 0 {
		return errors.New("启动 FreeRDP 会话失败")
	}
	return nil
}
func (r *RDP) Fit(x, y, w, h int32) {
	if r.view != nil && w > 0 && h > 0 {
		C.pin_rdp_fit(r.view, C.int(x), C.int(y), C.int(w), C.int(h))
	}
}
func (r *RDP) Show(show bool) {
	if r.view != nil {
		var v C.int
		if show {
			v = 1
		}
		C.pin_rdp_show(r.view, v)
	}
}
func (r *RDP) Close() {
	if r.view != nil {
		C.pin_rdp_close(r.view)
		r.view = nil
	}
}
func (r *RDP) Raise() {} // 原生子视图始终位于渲染层之上。
func (r *RDP) Focus() {
	if r.view != nil {
		C.pin_rdp_focus(r.view)
	}
}
func (r *RDP) ReleaseKeys() {
	if r.view != nil {
		C.pin_rdp_release_keys(r.view)
	}
}
func (r *RDP) Connected() bool { return r.view != nil && C.pin_rdp_connected(r.view) != 0 }
func (r *RDP) RemoteSize() (int32, int32) {
	var w, h C.int
	if r.view != nil {
		C.pin_rdp_size(r.view, &w, &h)
	}
	return int32(w), int32(h)
}

// 当前基线使用固定远程尺寸，本地视图缩放；不假报动态分辨率已生效。
func (r *RDP) ResizeRemoteNow(w, h, scale int32) (bool, error) { return false, nil }
func (r *RDP) SetDesktop(w, h int32)                           { r.width, r.height = w, h }
func (r *RDP) DPIPercent() int32                               { return 100 }
func (r *RDP) Scale() int32                                    { return 100 }
func (r *RDP) SetZoom(percent int32) error                     { return nil }
func (r *RDP) SetSmartSizing(on bool) error                    { return nil }
func (r *RDP) Error() error {
	if r.view == nil {
		return nil
	}
	var buf [512]C.char
	if C.pin_rdp_error(r.view, &buf[0], 512) != 0 {
		return errors.New(C.GoString(&buf[0]))
	}
	return nil
}
func ClientRect(parent uintptr) (int32, int32) {
	var w, h C.int
	if parent != 0 {
		C.pin_rdp_client_rect(C.uintptr_t(parent), &w, &h)
	}
	return int32(w), int32(h)
}
