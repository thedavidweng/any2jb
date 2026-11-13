/* Replaces only libusbd and the monotonic clock/sleep boundary of the pinned
   mac2jb helpers, so every report they would write is recorded with the
   virtual time at which it would be written. Included before the helper source. */
#include <libusbd.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

static unsigned long long parity_ns;
static FILE *parity_capture;

static int parity_write(libusbd_ctx_t *ctx, uint8_t iface, uint64_t ep, const void *data, uint32_t len, uint64_t timeout) {
    (void)ctx; (void)iface; (void)ep; (void)timeout;
    if (len != 8) return LIBUSBD_NOT_ENUMERATED;
    if (!parity_capture) {
        const char *path = getenv("PARITY_CAPTURE");
        if (!path || !(parity_capture = fopen(path, "a"))) abort();
    }
    const uint8_t *b = data;
    fprintf(parity_capture, "%llu %02x%02x%02x%02x%02x%02x%02x%02x\n", parity_ns, b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7]);
    fflush(parity_capture);
    return (int)len;
}
static int parity_clock(clockid_t id, struct timespec *value) {
    (void)id;
    value->tv_sec = (time_t)(parity_ns / 1000000000ULL);
    value->tv_nsec = (long)(parity_ns % 1000000000ULL);
    return 0;
}
static int parity_nanosleep(const struct timespec *value, struct timespec *remaining) {
    (void)remaining;
    parity_ns += (unsigned long long)value->tv_sec * 1000000000ULL + (unsigned long long)value->tv_nsec;
    return 0;
}
static unsigned int parity_sleep(unsigned int seconds) {
    parity_ns += (unsigned long long)seconds * 1000000000ULL;
    return 0;
}

#define libusbd_init(ctx) (*(ctx) = (libusbd_ctx_t *)1, 0)
#define libusbd_free(...) (0)
#define libusbd_set_pid(...) (0)
#define libusbd_set_version(...) (0)
#define libusbd_set_class(...) (0)
#define libusbd_set_subclass(...) (0)
#define libusbd_set_protocol(...) (0)
#define libusbd_set_manufacturer_str(...) (0)
#define libusbd_set_product_str(...) (0)
#define libusbd_set_serial_str(...) (0)
#define libusbd_iface_alloc(ctx, iface) (*(iface) = 0, 0)
#define libusbd_config_finalize(...) (0)
#define libusbd_iface_set_class(...) (0)
#define libusbd_iface_set_subclass(...) (0)
#define libusbd_iface_set_protocol(...) (0)
#define libusbd_iface_standard_desc(...) (0)
#define libusbd_iface_nonstandard_desc(...) (0)
#define libusbd_iface_add_endpoint(ctx, iface, type, direction, size, interval, unknown, ep) (*(ep) = 0, 0)
#define libusbd_iface_finalize(...) (0)
#define libusbd_ep_write parity_write
#define clock_gettime parity_clock
#define nanosleep parity_nanosleep
#define sleep parity_sleep
