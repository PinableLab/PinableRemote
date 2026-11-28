// SPDX-License-Identifier: GPL-2.0-or-later
#ifndef PINABLE_NATIVE_RDP_H
#define PINABLE_NATIVE_RDP_H
#include <stddef.h>
#include <stdint.h>
typedef struct pinable_rdp pinable_rdp;
pinable_rdp *pinable_rdp_new(const char *host, uint16_t port, const char *domain,
  const char *user, const char *password, const char *ca, size_t ca_len);
int pinable_rdp_connect(pinable_rdp *driver);
void pinable_rdp_abort(pinable_rdp *driver);
int pinable_rdp_poll(pinable_rdp *driver);
int pinable_rdp_size(pinable_rdp *driver, int *width, int *height);
int pinable_rdp_copy(pinable_rdp *driver, unsigned char *rgba, size_t capacity);
void pinable_rdp_free(pinable_rdp *driver);
#endif
