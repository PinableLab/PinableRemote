#include <stdint.h>
// 原生对象仅在主线程调用；协议线程不直接访问窗口。
void* pin_rdp_new(uintptr_t parent);
int pin_rdp_connect(void*, const char*, int, const char*, const char*, const char*, int, int);
void pin_rdp_fit(void*, int, int, int, int);
void pin_rdp_show(void*, int);
void pin_rdp_focus(void*);
void pin_rdp_release_keys(void*);
void pin_rdp_close(void*);
int pin_rdp_connected(void*);
void pin_rdp_size(void*, int*, int*);
void pin_rdp_client_rect(uintptr_t, int*, int*);
void pin_rdp_trust(void*, const char*);
int pin_rdp_accepted(void*, char*, int);
int pin_rdp_error(void*, char*, int);
