//go:build darwin && cgo && freerdp

package win32

import "testing"

func TestFreeRDPClosedView(t *testing.T) {
	r := &RDP{}
	if r.Connected() {
		t.Fatal("关闭视图不应连接")
	}
	if err := r.Connect("localhost", 3389, "user", "secret", ""); err == nil {
		t.Fatal("关闭视图必须拒绝连接")
	}
	r.Close()
	r.Close()
	r.Show(true)
	r.Focus()
	r.ReleaseKeys()
	r.Fit(0, 0, 100, 100)
	if w, h := r.RemoteSize(); w != 0 || h != 0 {
		t.Fatalf("尺寸=%dx%d", w, h)
	}
	if r.Error() != nil {
		t.Fatal("未启动不能产生网络错误")
	}
}
func TestFreeRDPNoParent(t *testing.T) {
	if r, err := NewRDP(0); err == nil || r != nil {
		t.Fatal("必须拒绝缺失原生窗口")
	}
}
