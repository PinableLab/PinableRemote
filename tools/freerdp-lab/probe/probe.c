/* FreeRDP 3.33.1 SDK feasibility fixture. No remote connection.
 * Tests the API hooks future managedrdp.Driver would implement. */
#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#include <errno.h>
#include <linux/filter.h>
#include <linux/seccomp.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#endif
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <openssl/pem.h>
#include <openssl/x509v3.h>
#include <freerdp/freerdp.h>
#include <freerdp/gdi/gdi.h>
#include <freerdp/input.h>
#include <freerdp/settings.h>
#include <freerdp/session.h>
#include <freerdp/transport_io.h>

#define REQUIRE(v) do { if (!(v)) { fprintf(stderr, "fixture check failed at line %d\n", __LINE__); abort(); } } while (0)

typedef struct Session {
	rdpContext context;
	X509_STORE *trust;
	const char *host;
	uint16_t port;
	int identity;
	int logged_on;
	uint64_t generation;
	unsigned int blocked_connects;
	unsigned int rejected_redirects;
} Session;

static int verify_chain(Session *s, const unsigned char *pem, size_t length,
                        const char *host, uint16_t port, uint32_t flags)
{
	if (!s || !s->trust || !host || !pem || strcmp(host, s->host) != 0 || port != s->port
	    || flags != 0 || length == 0 || length > (256 * 1024))
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

static int verify_x509(freerdp *instance, const BYTE *data, size_t length,
                       const char *hostname, UINT16 port, DWORD flags)
{
	Session *s = (Session *)instance->context;
	s->identity = verify_chain(s, data, length, hostname, port, flags);
	s->logged_on = 0;
	s->generation = 0;
	return s->identity ? 2 : 0; /* session-only, never trust unknown certs */
}

static BOOL deny_redirect(freerdp *instance)
{
	Session *s = (Session *)instance->context;
	s->identity = s->logged_on = 0;
	s->generation = 0;
	s->rejected_redirects++;
	return FALSE;
}

static rdpTransportLayer *fixture_deny_layer(rdpTransport *transport,
                            const char *hostname, int port, DWORD timeout)
{
	(void)timeout;
	Session *s = (Session *)transport_get_context(transport);
	if (hostname && strcmp(hostname, s->host) == 0 && port == s->port)
		s->blocked_connects++;
	return NULL; /* proof fixture: never call a socket/dial function */
}

static BOOL session_info(rdpContext *context, UINT32 type, const void *data)
{
	Session *s = (Session *)context;
	if (type == INFO_TYPE_LOGON || type == INFO_TYPE_LOGON_LONG) {
		const logon_info *info = data;
		const char *requested = freerdp_settings_get_string(context->settings, FreeRDP_Username);
		s->logged_on = s->identity && requested && info && info->username &&
		               strcmp(info->username, requested) == 0;
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
		s->generation++;
	return TRUE;
}

static BOOL fixture_snapshot(Session *s, BYTE *buf, size_t capacity)
{
	const rdpGdi *gdi = s->context.gdi;
	if (!s->generation || !s->identity || !s->logged_on || !valid_surface(gdi) ||
	    (size_t)gdi->width * gdi->height * 4 > capacity)
		return FALSE;
	for (int y = 0; y < gdi->height; y++)
		memcpy(buf + (size_t)y * gdi->width * 4, gdi->primary_buffer + (size_t)y * gdi->stride,
		       (size_t)gdi->width * 4);
	return TRUE;
}

static freerdp *fixture_init(const char *host, uint16_t port, const char *user)
{
	freerdp *instance = freerdp_new();
	REQUIRE(instance);
	instance->ContextSize = sizeof(Session);
	REQUIRE(freerdp_context_new(instance));
	Session *s = (Session *)instance->context;
	s->host = host;
	s->port = port;
	s->trust = X509_STORE_new();
	REQUIRE(s->trust);
	rdpSettings *settings = s->context.settings;
	REQUIRE(freerdp_settings_set_string(settings, FreeRDP_ServerHostname, host));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_ServerPort, port));
	REQUIRE(freerdp_settings_set_string(settings, FreeRDP_Username, user));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_AuthenticationLevel, 1));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_ProxyType, PROXY_TYPE_IGNORE));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_RedirectionPreferType, 7));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_DesktopWidth, 320));
	REQUIRE(freerdp_settings_set_uint32(settings, FreeRDP_DesktopHeight, 200));
	const size_t off[] = {
		FreeRDP_RdpSecurity, FreeRDP_TlsSecurity, FreeRDP_RdstlsSecurity, FreeRDP_AadSecurity,
		FreeRDP_UseRdpSecurityLayer, FreeRDP_IgnoreCertificate, FreeRDP_GatewayEnabled,
		FreeRDP_AutoReconnectionEnabled, FreeRDP_SupportMultitransport, FreeRDP_SupportMultitransportUdp,
		FreeRDP_SupportGraphicsPipeline, FreeRDP_RemoteApplicationMode, FreeRDP_BitmapCachePersistEnabled,
		FreeRDP_DeviceRedirection, FreeRDP_RedirectClipboard, FreeRDP_RedirectDrives,
		FreeRDP_RedirectHomeDrive, FreeRDP_RedirectSmartCards, FreeRDP_RedirectWebAuthN,
		FreeRDP_RedirectPrinters, FreeRDP_RedirectSerialPorts, FreeRDP_RedirectParallelPorts
	};
	for (size_t i = 0; i < sizeof(off)/sizeof(off[0]); i++)
		REQUIRE(freerdp_settings_set_bool(settings, off[i], FALSE));
	const size_t on[] = { FreeRDP_NlaSecurity, FreeRDP_ExtSecurity, FreeRDP_Authentication,
	                     FreeRDP_ExternalCertificateManagement, FreeRDP_AutoDenyCertificate };
	for (size_t i=0; i<sizeof(on)/sizeof(on[0]); i++)
		REQUIRE(freerdp_settings_set_bool(settings, on[i], TRUE));
	REQUIRE(freerdp_settings_set_string(settings, FreeRDP_CertificateAcceptedFingerprints, ""));
	REQUIRE(freerdp_settings_get_uint32(settings, FreeRDP_StaticChannelCount) == 0);
	REQUIRE(freerdp_settings_get_uint32(settings, FreeRDP_DynamicChannelCount) == 0);
	instance->VerifyX509Certificate = verify_x509;
	instance->Redirect = deny_redirect;
	s->context.update->SaveSessionInfo = session_info;
	rdpTransportIo io = *freerdp_get_io_callbacks(instance->context);
	io.ConnectLayer = fixture_deny_layer;
	REQUIRE(freerdp_set_io_callbacks(instance->context, &io));
	return instance;
}

static void free_fixture(freerdp *instance)
{
	Session *s = (Session *)instance->context;
	if (instance->context->gdi) gdi_free(instance);
	X509_STORE_free(s->trust);
	freerdp_context_free(instance);
	freerdp_free(instance);
}

static void trust_ca(Session *s, const char *file)
{
	BIO *bio = BIO_new_file(file, "r");
	REQUIRE(bio);
	X509 *ca = PEM_read_bio_X509(bio, NULL, NULL, NULL);
	REQUIRE(ca && X509_STORE_add_cert(s->trust, ca) == 1);
	X509_free(ca);
	BIO_free(bio);
}

#ifndef _WIN32
/* Deny network egress even if a future SDK bypasses the stub. Linux fixture. */
static void no_network(void)
{
	struct sock_filter insns[] = {
		BPF_STMT(BPF_LD | BPF_W | BPF_ABS, 0),
		BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, SYS_socket, 2, 0),
		BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, SYS_connect, 1, 0),
		BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ALLOW),
		BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | EPERM)
	};
	struct sock_fprog prog = { (unsigned short)(sizeof(insns)/sizeof(insns[0])), insns };
	REQUIRE(prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) == 0);
	REQUIRE(prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog) == 0);
}

#endif

int main(int argc, char **argv)
{
	REQUIRE(argc == 3);
#ifndef _WIN32
	no_network();
#endif
	freerdp *a = fixture_init("server-fixture.example.test", 3389, "test-user");
	freerdp *b = fixture_init("other-fixture.example.test", 3389, "test-user");
	Session *sa = (Session *)a->context;
	Session *sb = (Session *)b->context;
	FILE *leaf = fopen(argv[2], "rb");
	REQUIRE(leaf);
	BYTE pem[16384];
	size_t len = fread(pem, 1, sizeof(pem), leaf);
	fclose(leaf);
	REQUIRE(len > 0 && len < sizeof(pem));
	REQUIRE(!verify_x509(a, pem, len, sa->host, 3389, 0)); /* untrusted CA */
	trust_ca(sa, argv[1]);
	trust_ca(sb, argv[1]);
	REQUIRE(freerdp_settings_get_bool(a->context->settings, FreeRDP_NlaSecurity));
	REQUIRE(!freerdp_settings_get_bool(a->context->settings, FreeRDP_RdpSecurity));
	REQUIRE(!freerdp_settings_get_bool(a->context->settings, FreeRDP_TlsSecurity));
	REQUIRE(!verify_x509(a, pem, len, sa->host, 3390, 0)); /* wrong port */
	REQUIRE(!verify_x509(b, pem, len, sb->host, 3389, 0)); /* wrong authenticated DNS */
	REQUIRE(!verify_x509(a, pem, len, sa->host, 3389, VERIFY_CERT_FLAG_REDIRECT));
	REQUIRE(verify_x509(a, pem, len, sa->host, 3389, 0) == 2);
	/* Real gdi_init must be called from PostConnect after negotiated cache
	 * allocation. This network-free test uses the public surface struct with
	 * known padding and size so the copy/filter hook can be checked. */
	BOOL (*init_gdi)(freerdp *, UINT32) = gdi_init;
	REQUIRE(init_gdi);
	BYTE surface[200][1288] = {{0}};
	rdpGdi synthetic = {
		.context = a->context, .width = 320, .height = 200, .stride = sizeof(surface[0]),
		.dstFormat = PIXEL_FORMAT_XRGB32, .primary_buffer = &surface[0][0]
	};
	a->context->gdi = &synthetic;
	a->context->update->EndPaint = end_paint; /* same hook used after actual connect */
	BYTE copy[320 * 200 * 4] = { 0 };
	REQUIRE(!fixture_snapshot(sa, copy, sizeof(copy))); /* no logon, no frame */
	REQUIRE(session_info(a->context, INFO_TYPE_LOGON_PLAIN_NOTIFY, NULL));
	REQUIRE(!sa->logged_on);
	const logon_info unexpected = { 1, "other-account", "FIXTURE" };
	REQUIRE(!session_info(a->context, INFO_TYPE_LOGON_LONG, &unexpected));
	REQUIRE(!sa->logged_on);
	const logon_info logon = { 1, "test-user", "FIXTURE" };
	REQUIRE(session_info(a->context, INFO_TYPE_LOGON_LONG, &logon));
	REQUIRE(!fixture_snapshot(sa, copy, sizeof(copy))); /* no paint yet */
	sa->context.gdi->primary_buffer[0] = 0x7c;
	surface[1][0] = 0x2a;
	REQUIRE(a->context->update->EndPaint(a->context));
	REQUIRE(fixture_snapshot(sa, copy, sizeof(copy)) && copy[0] == 0x7c && copy[1280] == 0x2a);
	REQUIRE(!fixture_snapshot(sb, copy, sizeof(copy)));
	REQUIRE(!deny_redirect(a) && !fixture_snapshot(sa, copy, sizeof(copy)));
	a->context->gdi = NULL;
	/* Verify the live C API offers session-bound dispatch, without sending input. */
	BOOL (*mouse)(rdpInput *, UINT16, UINT16, UINT16) = freerdp_input_send_mouse_event;
	BOOL (*keys)(rdpInput *, BOOL, BOOL, UINT32) = freerdp_input_send_keyboard_event_ex;
	REQUIRE(mouse && keys && a->context->input != b->context->input);
	/* This connect-path probe requires Linux kernel-level egress blocking. */
#ifndef _WIN32
	REQUIRE(!freerdp_connect(b));
	REQUIRE(sb->blocked_connects >= 1);
#endif
	free_fixture(a);
	free_fixture(b);
#ifdef _WIN32
	puts("PASS: native Windows SDK hooks; no connect; verified host/CA; synthetic post-logon frame; two isolated contexts");
#else
	puts("PASS: 3.33.1 SDK hooks; no socket; verified host/CA; post-logon frame; two isolated contexts");
#endif
	return 0;
}
