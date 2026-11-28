//go:build (windows && cgo && pinable_freerdp) || (darwin && cgo && freerdp)

// SPDX-License-Identifier: GPL-2.0-or-later

#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <strings.h>
#define _stricmp strcasecmp
#endif
#include "native_rdp.h"
#include <winpr/synch.h>
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#ifdef _WIN32
/* 显式 CA 校验只在 Windows SDK 中提供 OpenSSL 头文件。 */
#include <openssl/pem.h>
#include <openssl/x509v3.h>
#else
typedef struct x509_store_st X509_STORE;
#define X509_STORE_free(store) ((void)(store))
#endif
#include <freerdp/crypto/certificate.h>
#include <freerdp/freerdp.h>
#include <freerdp/gdi/gdi.h>
#include <freerdp/input.h>
#include <freerdp/settings.h>
#include <freerdp/session.h>
#include <freerdp/transport_io.h>

typedef struct pinable_rdp {
    rdpContext context;
    X509_STORE *trust;
    unsigned char pin[32];
    int pinned;
    char *host;
    char *domain;
    char *user;
    uint16_t port;
    int identity;
    int logged_on;
    int generation;
    int dialed;
    int connected;
    rdpTransportLayer *(*connect_layer)(rdpTransport *, const char *, int, DWORD);
} Session;
/* 用户已在 PinableRemote 中信任此证书；只比较叶证书 SHA256，不查系统 CA。 */
static int verify_pin(Session *s, const unsigned char *pem, size_t length,
                      const char *host, uint16_t port, uint32_t flags)
{
	if (!s || !s->pinned || !host || !pem || strcmp(host, s->host) != 0 || port != s->port
	    || (flags & ~VERIFY_CERT_FLAG_FP_IS_PEM) != 0 || length == 0 || length > (256 * 1024))
		return 0;
	char *text = malloc(length + 1);
	if (!text) return 0;
	memcpy(text, pem, length);
	text[length] = 0;
	rdpCertificate *leaf = freerdp_certificate_new_from_pem(text);
	free(text);
	size_t size = 0;
	char *digest = leaf ? freerdp_certificate_get_hash(leaf, "sha256", &size) : NULL;
	int ok = digest && size == sizeof(s->pin);
	unsigned char diff = 0;
	for (size_t i = 0; ok && i < sizeof(s->pin); i++)
		diff |= (unsigned char)digest[i] ^ s->pin[i];
	free(digest);
	freerdp_certificate_free(leaf);
	return ok && diff == 0;
}

#ifdef _WIN32
static int verify_chain(Session *s, const unsigned char *pem, size_t length,
                        const char *host, uint16_t port, uint32_t flags)
{
	if (!s || !s->trust || !host || !pem || strcmp(host, s->host) != 0 || port != s->port
	    || (flags & ~VERIFY_CERT_FLAG_FP_IS_PEM) != 0 || length == 0 || length > (256 * 1024))
		return 0;
	BIO *bio = BIO_new_mem_buf(pem, (int)length);
	if (!bio) return 0;
	X509 *leaf = PEM_read_bio_X509(bio, NULL, NULL, NULL);
	STACK_OF(X509) *chain = sk_X509_new_null();
	X509_STORE_CTX *ctx = X509_STORE_CTX_new();
	int ok = 0;
	if (!leaf || !chain || !ctx) goto done;
	for (X509 *other = PEM_read_bio_X509(bio, NULL, NULL, NULL);
	     other; other = PEM_read_bio_X509(bio, NULL, NULL, NULL)) {
		if (sk_X509_num(chain) >= 8 || !sk_X509_push(chain, other)) {
			X509_free(other);
			goto done;
		}
	}
	if (X509_STORE_CTX_init(ctx, s->trust, leaf, chain) != 1 ||
	    X509_STORE_CTX_set_purpose(ctx, X509_PURPOSE_SSL_SERVER) != 1)
		goto done;
	X509_VERIFY_PARAM *param = X509_STORE_CTX_get0_param(ctx);
	if (!param) goto done;
	struct in_addr ipv4;
	struct in6_addr ipv6;
	if (inet_pton(AF_INET, host, &ipv4) == 1 || inet_pton(AF_INET6, host, &ipv6) == 1) {
		if (X509_VERIFY_PARAM_set1_ip_asc(param, host) != 1) goto done;
	} else {
		X509_VERIFY_PARAM_set_hostflags(param, X509_CHECK_FLAG_NEVER_CHECK_SUBJECT |
		                                     X509_CHECK_FLAG_NO_WILDCARDS);
		if (X509_VERIFY_PARAM_set1_host(param, host, 0) != 1) goto done;
	}
	ok = X509_verify_cert(ctx) == 1;
done:
	X509_STORE_CTX_free(ctx);
	sk_X509_pop_free(chain, X509_free);
	X509_free(leaf);
	BIO_free(bio);
	return ok;
}
#else
static int verify_chain(Session *s, const unsigned char *pem, size_t length,
                        const char *host, uint16_t port, uint32_t flags)
{
	(void)s; (void)pem; (void)length; (void)host; (void)port; (void)flags;
	return 0;
}
#endif

static int verify_x509(freerdp *instance, const BYTE *data, size_t length,
                       const char *hostname, UINT16 port, DWORD flags)
{
	Session *s = (Session *)instance->context;
	s->identity = s->pinned ? verify_pin(s, data, length, hostname, port, flags)
	                        : verify_chain(s, data, length, hostname, port, flags);
	s->logged_on = 0;
	s->generation = 0;
	return s->identity ? 2 : 0; /* session-only, never trust unknown certs */
}

static BOOL deny_redirect(freerdp *instance)
{
	Session *s = (Session *)instance->context;
	s->identity = s->logged_on = 0;
	s->generation = 0;
	return FALSE;
}

static BOOL session_info(rdpContext *context, UINT32 type, const void *data)
{
	Session *s = (Session *)context;
	if (type == INFO_TYPE_LOGON || type == INFO_TYPE_LOGON_LONG) {
		const logon_info *info = data;
		const char *requested = s->user;
		const char *domain = s->domain;
		/* 本地账户不填域时，只核对用户名；域由远端报告为计算机名。 */
		s->logged_on = s->identity && info && domain &&
		               (!domain[0] || (info->domain && _stricmp(info->domain, domain) == 0)) &&
		               info->username && requested &&
		               _stricmp(info->username, requested) == 0;
		s->generation = 0;
		return s->logged_on;
	}
	if (type == INFO_TYPE_LOGON_EXTENDED_INF && data &&
	    ((const logon_info_ex *)data)->haveErrorInfo) {
		s->logged_on = 0;
		s->generation = 0;
	}
	/* A plain notification has no account identity; do not promote readiness. */
	return TRUE;
}

static BOOL valid_surface(const rdpGdi *gdi)
{
	if (!gdi || !gdi->primary_buffer || gdi->width <= 0 || gdi->width > 4096 ||
	    gdi->height <= 0 || gdi->height > 4096 || gdi->dstFormat != PIXEL_FORMAT_XRGB32)
		return FALSE;
	return gdi->stride >= (UINT32)gdi->width * 4 &&
	       gdi->stride <= 32768 &&
	       (size_t)gdi->height * gdi->stride <= (64U << 20);
}

static BOOL end_paint(rdpContext *context)
{
	Session *s = (Session *)context;
	if (s->identity && s->logged_on && valid_surface(context->gdi))
		s->generation = 1;
	return TRUE;
}

static BOOL desktop_resize(rdpContext *context)
{
	Session *s = (Session *)context;
	s->generation = 0;
	const UINT32 width = freerdp_settings_get_uint32(context->settings, FreeRDP_DesktopWidth);
	const UINT32 height = freerdp_settings_get_uint32(context->settings, FreeRDP_DesktopHeight);
	return width > 0 && width <= 4096 && height > 0 && height <= 4096 &&
	       gdi_resize(context->gdi, width, height);
}

static BOOL post_connect(freerdp *instance)
{
	const rdpSettings *settings = instance->context->settings;
	const UINT32 width = freerdp_settings_get_uint32(settings, FreeRDP_DesktopWidth);
	const UINT32 height = freerdp_settings_get_uint32(settings, FreeRDP_DesktopHeight);
	if (width == 0 || width > 4096 || height == 0 || height > 4096 ||
	    !gdi_init(instance, PIXEL_FORMAT_XRGB32))
		return FALSE;
	instance->context->update->EndPaint = end_paint;
	instance->context->update->DesktopResize = desktop_resize;
	return TRUE;
}

static int eligible_endpoint(Session *s, const char *host, int port)
{
	return s && host && strcmp(host, s->host) == 0 && port == s->port && !s->dialed;
}

static rdpTransportLayer *bound_layer(rdpTransport *transport,
                                      const char *host, int port, DWORD timeout)
{
	Session *s = (Session *)transport_get_context(transport);
	if (!eligible_endpoint(s, host, port) || !s->connect_layer)
		return NULL;
	s->dialed = 1;
	return s->connect_layer(transport, host, port, timeout);
}

static BOOL configure(freerdp *instance, const char *host, uint16_t port,
                      const char *domain, const char *user, const char *password)
{
	Session *s = (Session *)instance->context;
	rdpSettings *settings = s->context.settings;
	s->host = _strdup(host);
	s->domain = _strdup(domain);
	s->user = _strdup(user);
	s->port = port;
	if (!s->host || !s->domain || !s->user ||
	    !freerdp_settings_set_string(settings, FreeRDP_ServerHostname, host) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_ServerPort, port) ||
	    !freerdp_settings_set_string(settings, FreeRDP_Domain, domain[0] ? domain : NULL) ||
	    !freerdp_settings_set_string(settings, FreeRDP_Username, user) ||
	    !freerdp_settings_set_string(settings, FreeRDP_Password, password) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_AuthenticationLevel, 1) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_ProxyType, PROXY_TYPE_IGNORE) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_TcpConnectTimeout, 5000) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_DesktopWidth, 1280) ||
	    !freerdp_settings_set_uint32(settings, FreeRDP_DesktopHeight, 720) ||
	    !freerdp_settings_set_string(settings, FreeRDP_CertificateAcceptedFingerprints, ""))
		return FALSE;
	const size_t off[] = {
		FreeRDP_RdpSecurity, FreeRDP_TlsSecurity, FreeRDP_RdstlsSecurity, FreeRDP_AadSecurity,
		FreeRDP_ExtSecurity, FreeRDP_UseRdpSecurityLayer, FreeRDP_IgnoreCertificate, FreeRDP_GatewayEnabled,
		FreeRDP_AutoReconnectionEnabled, FreeRDP_SupportMultitransport, FreeRDP_SupportMultitransportUdp,
		FreeRDP_SupportGraphicsPipeline, FreeRDP_SupportDynamicChannels, FreeRDP_RemoteApplicationMode,
		FreeRDP_BitmapCachePersistEnabled, FreeRDP_DeviceRedirection, FreeRDP_RedirectClipboard,
		FreeRDP_RedirectDrives, FreeRDP_RedirectHomeDrive, FreeRDP_RedirectSmartCards,
		FreeRDP_RedirectWebAuthN, FreeRDP_RedirectPrinters, FreeRDP_RedirectSerialPorts,
		FreeRDP_RedirectParallelPorts
	};
	for (size_t i = 0; i < sizeof(off)/sizeof(off[0]); i++)
		if (!freerdp_settings_set_bool(settings, off[i], FALSE)) return FALSE;
	const size_t on[] = { FreeRDP_NlaSecurity, FreeRDP_Authentication,
	                     FreeRDP_ExternalCertificateManagement, FreeRDP_AutoDenyCertificate };
	for (size_t i = 0; i < sizeof(on)/sizeof(on[0]); i++)
		if (!freerdp_settings_set_bool(settings, on[i], TRUE)) return FALSE;
	if (freerdp_settings_get_uint32(settings, FreeRDP_StaticChannelCount) != 0 ||
	    freerdp_settings_get_uint32(settings, FreeRDP_DynamicChannelCount) != 0)
		return FALSE;
	instance->VerifyX509Certificate = verify_x509;
	instance->Redirect = deny_redirect;
	instance->PostConnect = post_connect;
	s->context.update->SaveSessionInfo = session_info;
	const rdpTransportIo *defaults = freerdp_get_io_callbacks(instance->context);
	if (!defaults || !defaults->ConnectLayer) return FALSE;
	rdpTransportIo io = *defaults;
	s->connect_layer = io.ConnectLayer;
	io.ConnectLayer = bound_layer;
	return freerdp_set_io_callbacks(instance->context, &io);
}

void pinable_rdp_free(Session *s)
{
	if (!s) return;
	freerdp *instance = s->context.instance;
	if (s->connected) freerdp_disconnect(instance);
	if (s->context.gdi) gdi_free(instance);
	X509_STORE_free(s->trust);
	free(s->host);
	free(s->domain);
	free(s->user);
	freerdp_context_free(instance);
	freerdp_free(instance);
}

Session *pinable_rdp_new(const char *host, uint16_t port, const char *domain,
                        const char *user, const char *password, const char *ca, size_t ca_len,
                        const unsigned char *pin)
{
	/* 恰好一种信任来源：显式 CA 或用户信任过的证书指纹。 */
	if (pin ? ca != NULL : (!ca || ca_len == 0 || ca_len > (64U << 10))) return NULL;
	freerdp *instance = freerdp_new();
	if (!instance) return NULL;
	instance->ContextSize = sizeof(Session);
	if (!freerdp_context_new(instance)) {
		freerdp_free(instance);
		return NULL;
	}
	Session *s = (Session *)instance->context;
	if (pin) {
		memcpy(s->pin, pin, sizeof(s->pin));
		s->pinned = 1;
		if (!configure(instance, host, port, domain, user, password)) {
			pinable_rdp_free(s);
			return NULL;
		}
		return s;
	}
#ifndef _WIN32
	pinable_rdp_free(s);
	return NULL;
#else
	s->trust = X509_STORE_new();
	BIO *bio = BIO_new_mem_buf(ca, (int)ca_len);
	X509 *root = bio ? PEM_read_bio_X509(bio, NULL, NULL, NULL) : NULL;
	const int trusted = s->trust && root && X509_check_ca(root) > 0 &&
	                    X509_STORE_add_cert(s->trust, root) == 1;
	X509_free(root);
	BIO_free(bio);
	if (!trusted || !configure(instance, host, port, domain, user, password)) {
		pinable_rdp_free(s);
		return NULL;
	}
	return s;
#endif
}

int pinable_rdp_connect(Session *s)
{
	if (s->dialed || !freerdp_connect(s->context.instance)) return 0;
	s->connected = 1;
	return s->identity;
}

void pinable_rdp_abort(Session *s) { freerdp_abort_connect_context(&s->context); }

int pinable_rdp_poll(Session *s)
{
	if (freerdp_shall_disconnect_context(&s->context)) return 0;
	HANDLE handles[MAXIMUM_WAIT_OBJECTS];
	DWORD n = freerdp_get_event_handles(&s->context, handles, MAXIMUM_WAIT_OBJECTS);
	if (!n || n > MAXIMUM_WAIT_OBJECTS) return 0;
	DWORD result = WaitForMultipleObjects(n, handles, FALSE, 0);
	if (result == WAIT_TIMEOUT) return 1;
	return result < n && freerdp_check_event_handles(&s->context);
}

int pinable_rdp_size(Session *s, int *width, int *height)
{
	const rdpGdi *gdi = s->context.gdi;
	if (!s->connected || !s->generation || !s->identity || !s->logged_on || !valid_surface(gdi))
		return 0;
	*width = gdi->width;
	*height = gdi->height;
	return 1;
}

int pinable_rdp_copy(Session *s, unsigned char *rgba, size_t capacity)
{
	int width, height;
	if (!pinable_rdp_size(s, &width, &height) || (size_t)width * height * 4 > capacity)
		return 0;
	const rdpGdi *gdi = s->context.gdi;
	for (int y = 0; y < height; y++) {
		const BYTE *src = gdi->primary_buffer + (size_t)y * gdi->stride;
		for (int x = 0; x < width; x++, src += 4, rgba += 4) {
			rgba[0] = src[2];
			rgba[1] = src[1];
			rgba[2] = src[0];
			rgba[3] = 255;
		}
	}
	return 1;
}
