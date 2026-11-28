// SPDX-License-Identifier: GPL-2.0-or-later
/* Test production native callbacks without reading credentials or dialing. */
#include "../../internal/managedrdp/native_rdp_windows.c"
#define CHECK(v) do { if (!(v)) { fprintf(stderr, "line %d\n", __LINE__); return 1; } } while (0)

static size_t load(const char *name, char *buf, size_t capacity)
{
	FILE *file = fopen(name, "rb");
	if (!file) return 0;
	size_t length = fread(buf, 1, capacity, file);
	fclose(file);
	return length == capacity ? 0 : length;
}

int main(int argc, char **argv)
{
	CHECK(argc == 3);
	char ca[8192], leaf[8192];
	size_t ca_len = load(argv[1], ca, sizeof(ca));
	size_t leaf_len = load(argv[2], leaf, sizeof(leaf));
	CHECK(ca_len && leaf_len);
	const char *host = "server-fixture.example.test";
	Session *s = pinable_rdp_new(host, 3389, "FIXTURE", "test-user", "unused", ca, ca_len);
	CHECK(s);
	freerdp *instance = s->context.instance;
	CHECK(!verify_x509(instance, (BYTE *)leaf, leaf_len, host, 3388, 0));
	CHECK(!verify_x509(instance, (BYTE *)leaf, leaf_len, "invalid.example.test", 3389, 0));
	CHECK(!verify_x509(instance, (BYTE *)leaf, leaf_len, host, 3389, VERIFY_CERT_FLAG_REDIRECT));
	CHECK(verify_x509(instance, (BYTE *)leaf, leaf_len, host, 3389, 0));
	/* No network: test the guard without calling the transport. */
	CHECK(eligible_endpoint(s, host, 3389));
	CHECK(!eligible_endpoint(s, host, 3390));
	CHECK(!eligible_endpoint(s, "other.example.test", 3389));
	s->dialed = 1;
	CHECK(!eligible_endpoint(s, host, 3389));
	s->connected = 1;
	BYTE pixels[] = {0x31,0x42,0x53,0,0,0,0,0};
	BYTE image[4] = {0};
	rdpGdi gdi = {.width=1, .height=1, .stride=8,
	              .dstFormat=PIXEL_FORMAT_XRGB32, .primary_buffer=pixels};
	s->context.gdi = &gdi;
	CHECK(session_info(&s->context, INFO_TYPE_LOGON_PLAIN_NOTIFY, NULL));
	CHECK(end_paint(&s->context) && !pinable_rdp_copy(s, image, sizeof(image)));
	const logon_info wrong = {1,"test-user","OTHER"};
	CHECK(!session_info(&s->context, INFO_TYPE_LOGON_LONG, &wrong));
	const logon_info right = {1,"test-user","fixture"};
	CHECK(session_info(&s->context, INFO_TYPE_LOGON_LONG, &right));
	CHECK(!pinable_rdp_copy(s, image, sizeof(image)));
	CHECK(end_paint(&s->context) && !pinable_rdp_copy(s, image, 3));
	CHECK(pinable_rdp_copy(s, image, sizeof(image)));
	CHECK(image[0] == 0x53 && image[1] == 0x42 && image[2] == 0x31 && image[3] == 255);
	CHECK(!deny_redirect(instance));
	CHECK(!pinable_rdp_copy(s, image, sizeof(image)));
	s->context.gdi = NULL;
	s->connected = 0;
	pinable_rdp_free(s);
	return 0;
}
