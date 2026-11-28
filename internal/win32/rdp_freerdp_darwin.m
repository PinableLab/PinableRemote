//go:build darwin && cgo && freerdp

#import <Cocoa/Cocoa.h>
#include "rdp_freerdp_darwin.h"
#include <freerdp/freerdp.h>
#include <freerdp/gdi/gdi.h>
#include <freerdp/input.h>
#include <freerdp/settings.h>
#include <freerdp/update.h>
#include <winpr/synch.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <unistd.h>

// FreeRDP 和输入队列仅由协议线程驱动；帧与证书询问通过锁交给主线程。
typedef struct { UINT16 flags, x, y; int kind; } PinInput;
typedef struct {
    pthread_mutex_t mu;
    atomic_int refs, stop, connected;
    freerdp* instance;
    BYTE* pixels;
    int width, height, stride;
    BOOL dirty;
    PinInput events[1024];
    unsigned count;
    int certPending, certAnswer, certChanged;
    char cert[4096], error[512];
    // known 是已信任证书的 SHA256；accepted 是用户本次选择长期信任的证书。
    char known[128], offered[128], accepted[128];
} PinState;
typedef struct { rdpContext context; PinState* state; } PinContext;

static void pin_unref(PinState* s) {
    if (!s) return;
    if (atomic_fetch_sub(&s->refs, 1) == 1) {
        free(s->pixels);
        pthread_mutex_destroy(&s->mu);
        free(s);
    }
}
static PinState* pin_state(rdpContext* c) { return ((PinContext*)c)->state; }
static BOOL pin_paint(rdpContext* c) {
    PinState* s = pin_state(c);
    rdpGdi* g = c->gdi;
    if (!g || g->width <= 0 || g->height <= 0 || g->width > 8192 || g->height > 8192 ||
        g->stride < (UINT32)g->width * 4 || g->stride > 8192 * 4) return FALSE;
    size_t size = (size_t)g->stride * g->height;
    pthread_mutex_lock(&s->mu);
    BYTE* p = realloc(s->pixels, size);
    if (p) {
        s->pixels = p;
        memcpy(p, g->primary_buffer, size);
        s->width = g->width; s->height = g->height; s->stride = g->stride;
        s->dirty = YES;
    }
    pthread_mutex_unlock(&s->mu);
    return p != NULL;
}
static BOOL pin_post_connect(freerdp* f) {
    // 桌面帧不含透明度，第四个字节是填充而非 alpha。
    if (!gdi_init(f, PIXEL_FORMAT_BGRX32)) return FALSE;
    f->context->update->EndPaint = pin_paint;
    return TRUE;
}
static BOOL pin_resize(rdpContext* c) {
    if (!gdi_resize(c->gdi, freerdp_settings_get_uint32(c->settings, FreeRDP_DesktopWidth),
                    freerdp_settings_get_uint32(c->settings, FreeRDP_DesktopHeight))) return FALSE;
    return pin_paint(c);
}
// 信任记录由 Go 侧 known_hosts.json 保存，不写入 FreeRDP 全局信任库。
// 与已信任指纹一致时直接连接；首次或指纹变化时询问用户。
static DWORD pin_cert(freerdp* f, const char* host, UINT16 port, const char* cn,
                      const char* subject, const char* issuer, const char* fingerprint, DWORD flags) {
    PinState* s = pin_state(f->context);
    pthread_mutex_lock(&s->mu);
    const char* fp = fingerprint ? fingerprint : "";
    if (s->known[0] && fp[0] && strcasecmp(s->known, fp) == 0) {
        pthread_mutex_unlock(&s->mu);
        return 2;
    }
    snprintf(s->cert, sizeof(s->cert), "%s:%u\n\nSubject: %s\nIssuer: %s\nSHA256: %s",
             host, port, subject ? subject : "", issuer ? issuer : "", fp);
    snprintf(s->offered, sizeof(s->offered), "%s", fp);
    s->certChanged = s->known[0] != 0;
    s->certAnswer = 0; s->certPending = 1;
    pthread_mutex_unlock(&s->mu);
    // 用户不确认时超时拒绝；关闭窗口也不能遗留认证线程。
    for (int n = 0; n < 1200 && !atomic_load(&s->stop); n++) {
        pthread_mutex_lock(&s->mu);
        int answer = s->certAnswer;
        pthread_mutex_unlock(&s->mu);
        if (answer) return answer == 1 ? 2 : 0;
        usleep(100000);
    }
    return 0;
}
static DWORD pin_changed_cert(freerdp* f, const char* host, UINT16 port, const char* cn,
                              const char* subject, const char* issuer, const char* fingerprint,
                              const char* oldSubject, const char* oldIssuer,
                              const char* oldFingerprint, DWORD flags) {
    // 已知证书变化不降级为普通首次确认。
    PinState* s = pin_state(f->context);
    pthread_mutex_lock(&s->mu);
    snprintf(s->error, sizeof(s->error), "RDP server certificate changed; connection refused");
    pthread_mutex_unlock(&s->mu);
    return 0;
}
static void* pin_worker(void* arg) {
    PinState* s = arg;
    freerdp* f = s->instance;
    BOOL ok = freerdp_connect(f);
    if (ok) {
        f->context->update->DesktopResize = pin_resize;
        atomic_store(&s->connected, 1);
        while (!atomic_load(&s->stop) && !freerdp_shall_disconnect_context(f->context)) {
            PinInput events[1024];
            pthread_mutex_lock(&s->mu);
            unsigned count = s->count;
            memcpy(events, s->events, count * sizeof(PinInput)); s->count = 0;
            pthread_mutex_unlock(&s->mu);
            for (unsigned i = 0; i < count; i++) {
                PinInput e = events[i];
                BOOL sent = e.kind == 0
                    ? freerdp_input_send_mouse_event(f->context->input, e.flags, e.x, e.y)
                    : freerdp_input_send_keyboard_event(f->context->input, e.flags, (UINT8)e.x);
                if (!sent) atomic_store(&s->stop, 1);
            }
            HANDLE handles[64];
            DWORD n = freerdp_get_event_handles(f->context, handles, 64);
            if (!n || WaitForMultipleObjects(n, handles, FALSE, 16) == WAIT_FAILED ||
                !freerdp_check_event_handles(f->context)) break;
        }
    }
    if (!atomic_load(&s->stop)) {
        UINT32 code = freerdp_get_last_error(f->context);
        pthread_mutex_lock(&s->mu);
        if (!s->error[0]) snprintf(s->error, sizeof(s->error), "RDP: %s (0x%08x)",
                                  freerdp_get_last_error_string(code), code);
        pthread_mutex_unlock(&s->mu);
    }
    atomic_store(&s->connected, 0);
    BOOL disconnected = freerdp_disconnect(f);
    (void)disconnected;
    gdi_free(f);
    // Close 的 abort 操作与释放实例互斥，避免悬空 context。
    pthread_mutex_lock(&s->mu);
    s->instance = NULL;
    pthread_mutex_unlock(&s->mu);
    freerdp_context_free(f); freerdp_free(f);
    pin_unref(s);
    return NULL;
}

@interface PinRDPView : NSView {
@public
    PinState* state;
    NSTimer* timer;
    NSImage* image;
    NSMutableSet<NSNumber*>* keys;
    int desktopW, desktopH;
    BOOL started;
    NSTrackingArea* tracking;
}
@end
@implementation PinRDPView
- (BOOL)isFlipped { return YES; }
- (BOOL)acceptsFirstResponder { return YES; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if (tracking) { [self removeTrackingArea:tracking]; [tracking release]; }
    tracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect options:
        NSTrackingMouseMoved | NSTrackingActiveInKeyWindow | NSTrackingInVisibleRect
        owner:self userInfo:nil];
    [self addTrackingArea:tracking];
}
- (void)drawRect:(NSRect)rect {
    [[NSColor blackColor] setFill]; NSRectFill(self.bounds);
    if (image) [image drawInRect:self.bounds fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
}
- (void)tick:(NSTimer*)unused {
    pthread_mutex_lock(&state->mu);
    if (state->dirty && state->pixels) {
        // Core Graphics 的小端 XRGB 对应内存中的 B、G、R、X。
        // CFData 持有独立副本，协议线程可以继续覆盖下一帧。
        CFDataRef data = CFDataCreate(NULL, state->pixels, (CFIndex)state->stride * state->height);
        CGDataProviderRef provider = data ? CGDataProviderCreateWithCFData(data) : NULL;
        CGColorSpaceRef colorSpace = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
        CGImageRef frame = provider && colorSpace ? CGImageCreate(state->width, state->height,
            8, 32, state->stride, colorSpace, kCGBitmapByteOrder32Little | kCGImageAlphaNoneSkipFirst,
            provider, NULL, true, kCGRenderingIntentDefault) : NULL;
        if (frame) {
            [image release];
            image = [[NSImage alloc] initWithCGImage:frame size:NSMakeSize(state->width, state->height)];
            CGImageRelease(frame);
        }
        if (colorSpace) CGColorSpaceRelease(colorSpace);
        if (provider) CGDataProviderRelease(provider);
        if (data) CFRelease(data);
        state->dirty = NO;
        [self setNeedsDisplay:YES];
    }
    BOOL cert = state->certPending == 1, changed = state->certChanged != 0;
    NSString* details = cert ? [[NSString alloc] initWithUTF8String:state->cert] : nil;
    if (cert) state->certPending = 2;
    pthread_mutex_unlock(&state->mu);
    if (cert && !atomic_load(&state->stop)) {
        [self retain]; // 证书确认期间关闭主窗口不能释放当前回调的状态。
        BOOL zh = [[[NSLocale preferredLanguages] firstObject] hasPrefix:@"zh"];
        NSAlert* alert = [[NSAlert alloc] init];
        if (changed) {
            alert.alertStyle = NSAlertStyleCritical;
            alert.messageText = zh ? @"RDP 服务器证书已变化" : @"RDP server certificate has changed";
            alert.informativeText = [NSString stringWithFormat:@"%@\n\n%@", details,
                zh ? @"与之前信任的证书不同，可能是服务器重装或证书更新，也可能是攻击。确认无误后才信任新证书。" : @"It differs from the certificate trusted before. The server may have been reinstalled or renewed its certificate, or this may be an attack. Trust the new certificate only if you expected this."];
        } else {
            alert.messageText = zh ? @"首次连接此 RDP 服务器" : @"First connection to this RDP server";
            alert.informativeText = [NSString stringWithFormat:@"%@\n\n%@", details,
                zh ? @"此证书不受系统信任，常见于 Windows 自签名证书。信任后会记住此设备，证书不变时不再询问。" : @"The system does not trust this certificate, which is common for self-signed Windows certificates. Once trusted, this device is remembered and you will not be asked again while the certificate stays the same."];
        }
        [alert addButtonWithTitle:zh ? @"取消" : @"Cancel"];
        [alert addButtonWithTitle:zh ? @"信任此设备" : @"Trust this device"];
        NSModalResponse response = [alert runModal];
        pthread_mutex_lock(&state->mu);
        state->certAnswer = response == NSAlertSecondButtonReturn ? 1 : -1;
        if (state->certAnswer == 1) snprintf(state->accepted, sizeof(state->accepted), "%s", state->offered);
        pthread_mutex_unlock(&state->mu);
        [alert release];
        [self release];
    }
    [details release];
}
- (void)enqueue:(PinInput)e {
    if (!atomic_load(&state->connected)) return;
    pthread_mutex_lock(&state->mu);
    if (state->count < 1024) state->events[state->count++] = e;
    else atomic_store(&state->stop, 1); // 队列满时断开，避免丢失按键释放。
    pthread_mutex_unlock(&state->mu);
}
- (void)mouse:(NSEvent*)e flags:(UINT16)flags {
    NSPoint p = [self convertPoint:e.locationInWindow fromView:nil];
    pthread_mutex_lock(&state->mu);
    int w = state->width, h = state->height;
    pthread_mutex_unlock(&state->mu);
    if (!w || !h || self.bounds.size.width <= 0 || self.bounds.size.height <= 0) return;
    int x = MAX(0, MIN(w-1, p.x * w / self.bounds.size.width));
    int y = MAX(0, MIN(h-1, p.y * h / self.bounds.size.height));
    [self enqueue:(PinInput){flags, x, y, 0}];
}
- (void)mouseDown:(NSEvent*)e { [self.window makeFirstResponder:self]; [self mouse:e flags:PTR_FLAGS_BUTTON1 | PTR_FLAGS_DOWN]; }
- (void)mouseUp:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_BUTTON1]; }
- (void)rightMouseDown:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_BUTTON2 | PTR_FLAGS_DOWN]; }
- (void)rightMouseUp:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_BUTTON2]; }
- (void)otherMouseDown:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_BUTTON3 | PTR_FLAGS_DOWN]; }
- (void)otherMouseUp:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_BUTTON3]; }
- (void)mouseMoved:(NSEvent*)e { [self mouse:e flags:PTR_FLAGS_MOVE]; }
- (void)mouseDragged:(NSEvent*)e { [self mouseMoved:e]; }
- (void)rightMouseDragged:(NSEvent*)e { [self mouseMoved:e]; }
- (void)scrollWheel:(NSEvent*)e {
    int delta = MIN(255, MAX(-255, (int)(e.scrollingDeltaY * 12)));
    if (delta) [self mouse:e flags:PTR_FLAGS_WHEEL | (delta < 0 ? PTR_FLAGS_WHEEL_NEGATIVE : 0) | (delta & 0xff)];
}
// macOS 虚拟键码到 PC set-1 扫描码；扩展位使用 0x100 表示。
static const UINT16 keycodes[128] = {
    [0]=0x1e,[1]=0x1f,[2]=0x20,[3]=0x21,[4]=0x23,[5]=0x22,[6]=0x2c,[7]=0x2d,
    [8]=0x2e,[9]=0x2f,[11]=0x30,[12]=0x10,[13]=0x11,[14]=0x12,[15]=0x13,[16]=0x15,
    [17]=0x14,[18]=0x02,[19]=0x03,[20]=0x04,[21]=0x05,[22]=0x07,[23]=0x06,[24]=0x0d,
    [25]=0x0a,[26]=0x08,[27]=0x0c,[28]=0x09,[29]=0x0b,[30]=0x1b,[31]=0x18,[32]=0x16,
    [33]=0x1a,[34]=0x17,[35]=0x19,[36]=0x1c,[37]=0x26,[38]=0x24,[39]=0x28,[40]=0x25,
    [41]=0x27,[42]=0x2b,[43]=0x33,[44]=0x35,[45]=0x31,[46]=0x32,[47]=0x34,[48]=0x0f,
    [49]=0x39,[50]=0x29,[51]=0x0e,[53]=0x01,[54]=0x15c,[55]=0x15b,[56]=0x2a,[57]=0x3a,
    [58]=0x38,[59]=0x1d,[60]=0x36,[61]=0x138,[62]=0x11d,[65]=0x53,[67]=0x37,[69]=0x4e,
    [75]=0x135,[76]=0x11c,[78]=0x4a,[82]=0x52,[83]=0x4f,[84]=0x50,[85]=0x51,[86]=0x4b,
    [87]=0x4c,[88]=0x4d,[89]=0x47,[91]=0x48,[92]=0x49,[96]=0x3f,[97]=0x40,[98]=0x41,
    [99]=0x3d,[100]=0x42,[101]=0x43,[103]=0x57,[109]=0x44,[111]=0x58,[115]=0x147,
    [116]=0x149,[117]=0x153,[118]=0x3e,[119]=0x14f,[120]=0x3c,[121]=0x151,[122]=0x3b,
    [123]=0x14b,[124]=0x14d,[125]=0x150,[126]=0x148
};
- (void)key:(unsigned)code down:(BOOL)down {
    if (code >= 128 || !keycodes[code]) return;
    UINT16 scan = keycodes[code];
    [self enqueue:(PinInput){(down ? KBD_FLAGS_DOWN : KBD_FLAGS_RELEASE) | (scan & 0x100 ? KBD_FLAGS_EXTENDED : 0), scan & 0xff, 0, 1}];
    if (down) [keys addObject:@(code)]; else [keys removeObject:@(code)];
}
- (void)keyDown:(NSEvent*)e { [self key:e.keyCode down:YES]; }
- (void)keyUp:(NSEvent*)e { [self key:e.keyCode down:NO]; }
- (void)flagsChanged:(NSEvent*)e {
    NSEventModifierFlags flag = 0;
    switch(e.keyCode) {
        case 54: case 55: flag = NSEventModifierFlagCommand; break;
        case 56: case 60: flag = NSEventModifierFlagShift; break;
        case 58: case 61: flag = NSEventModifierFlagOption; break;
        case 59: case 62: flag = NSEventModifierFlagControl; break;
        case 57: [self key:57 down:YES]; [self key:57 down:NO]; return;
    }
    if (flag) [self key:e.keyCode down:(e.modifierFlags & flag) != 0 && ![keys containsObject:@(e.keyCode)]];
}
- (BOOL)resignFirstResponder { pin_rdp_release_keys(self); return YES; }
- (void)dealloc { [tracking release]; [image release]; [keys release]; pin_unref(state); [super dealloc]; }
@end

void* pin_rdp_new(uintptr_t parent) {
    NSWindow* window = (NSWindow*)parent;
    if (!window || ![window isKindOfClass:[NSWindow class]]) return NULL;
    PinRDPView* v = [[PinRDPView alloc] initWithFrame:NSMakeRect(0,0,640,480)];
    v->state = calloc(1, sizeof(PinState));
    if (!v->state) { [v release]; return NULL; }
    pthread_mutex_init(&v->state->mu, NULL); atomic_init(&v->state->refs, 1);
    v->keys = [[NSMutableSet alloc] init];
    [v setHidden:YES]; [window.contentView addSubview:v];
    v->timer = [NSTimer scheduledTimerWithTimeInterval:1.0/30 target:v selector:@selector(tick:) userInfo:nil repeats:YES];
    [window setAcceptsMouseMovedEvents:YES];
    return v;
}
int pin_rdp_connect(void* p, const char* host, int port, const char* user, const char* password,
                    const char* domain, int width, int height) {
    PinRDPView* v = p; PinState* s = v->state;
    if (v->started) return 0;
    freerdp* f = freerdp_new();
    if (!f) return 0;
    f->ContextSize = sizeof(PinContext);
    if (!freerdp_context_new(f)) { freerdp_free(f); return 0; }
    ((PinContext*)f->context)->state = s;
    f->PostConnect = pin_post_connect;
    f->VerifyCertificateEx = pin_cert; f->VerifyChangedCertificateEx = pin_changed_cert;
    rdpSettings* settings = f->context->settings;
    BOOL ok = freerdp_settings_set_string(settings, FreeRDP_ServerHostname, host) &&
        freerdp_settings_set_uint32(settings, FreeRDP_ServerPort, port) &&
        freerdp_settings_set_string(settings, FreeRDP_Username, user) &&
        freerdp_settings_set_string(settings, FreeRDP_Password, password) &&
        freerdp_settings_set_string(settings, FreeRDP_Domain, domain) &&
        freerdp_settings_set_uint32(settings, FreeRDP_DesktopWidth, width) &&
        freerdp_settings_set_uint32(settings, FreeRDP_DesktopHeight, height) &&
        freerdp_settings_set_uint32(settings, FreeRDP_ColorDepth, 32) &&
        freerdp_settings_set_bool(settings, FreeRDP_SoftwareGdi, TRUE) &&
        freerdp_settings_set_bool(settings, FreeRDP_SupportGraphicsPipeline, FALSE) &&
        freerdp_settings_set_bool(settings, FreeRDP_RdpSecurity, FALSE) &&
        freerdp_settings_set_bool(settings, FreeRDP_TlsSecurity, TRUE) &&
        freerdp_settings_set_bool(settings, FreeRDP_NlaSecurity, TRUE) &&
        freerdp_settings_set_bool(settings, FreeRDP_IgnoreCertificate, FALSE) &&
        freerdp_settings_set_bool(settings, FreeRDP_AutoAcceptCertificate, FALSE) &&
        freerdp_settings_set_uint32(settings, FreeRDP_AuthenticationLevel, 2);
    if (!ok) { freerdp_context_free(f); freerdp_free(f); return 0; }
    s->instance = f; atomic_fetch_add(&s->refs, 1);
    pthread_t thread;
    if (pthread_create(&thread, NULL, pin_worker, s)) {
        s->instance = NULL; pin_unref(s); freerdp_context_free(f); freerdp_free(f); return 0;
    }
    pthread_detach(thread); v->started = YES;
    [v.window makeFirstResponder:v];
    return 1;
}
void pin_rdp_fit(void* p, int x, int y, int w, int h) {
    PinRDPView* v = p;
    NSView* parent = v.superview;
    CGFloat top = parent.isFlipped ? y : parent.bounds.size.height-y-h;
    [v setFrame:NSMakeRect(x, top, w, h)];
}
void pin_rdp_release_keys(void* p) {
    PinRDPView* v = p;
    NSArray* pressed = [v->keys allObjects];
    for (NSNumber* code in pressed) [v key:code.unsignedIntValue down:NO];
}
void pin_rdp_show(void* p, int show) { PinRDPView* v = p; if (!show) pin_rdp_release_keys(p); [v setHidden:!show]; }
void pin_rdp_focus(void* p) { PinRDPView* v = p; [v.window makeFirstResponder:v]; }
void pin_rdp_close(void* p) {
    PinRDPView* v = p; PinState* s = v->state;
    atomic_store(&s->stop, 1);
    pthread_mutex_lock(&s->mu);
    if (s->instance) { BOOL aborted = freerdp_abort_connect_context(s->instance->context); (void)aborted; }
    pthread_mutex_unlock(&s->mu);
    [v->timer invalidate]; [v removeFromSuperview]; [v release];
}
int pin_rdp_connected(void* p) { return atomic_load(&((PinRDPView*)p)->state->connected); }
void pin_rdp_size(void* p, int* w, int* h) {
    PinState* s = ((PinRDPView*)p)->state;
    pthread_mutex_lock(&s->mu); *w = s->width; *h = s->height; pthread_mutex_unlock(&s->mu);
}
void pin_rdp_client_rect(uintptr_t parent, int* w, int* h) {
    NSWindow* window = (NSWindow*)parent;
    *w = window.contentView.bounds.size.width; *h = window.contentView.bounds.size.height;
}
void pin_rdp_trust(void* p, const char* fingerprint) {
    PinState* s = ((PinRDPView*)p)->state;
    pthread_mutex_lock(&s->mu); snprintf(s->known, sizeof(s->known), "%s", fingerprint); pthread_mutex_unlock(&s->mu);
}
int pin_rdp_accepted(void* p, char* buf, int size) {
    PinState* s = ((PinRDPView*)p)->state;
    pthread_mutex_lock(&s->mu); snprintf(buf, size, "%s", s->accepted); s->accepted[0] = 0; pthread_mutex_unlock(&s->mu);
    return buf[0] != 0;
}
int pin_rdp_error(void* p, char* buf, int size) {
    PinState* s = ((PinRDPView*)p)->state;
    pthread_mutex_lock(&s->mu); snprintf(buf, size, "%s", s->error); pthread_mutex_unlock(&s->mu);
    return buf[0] != 0;
}
