#include <errno.h>
#include <fcntl.h>
#include <linux/uhid.h>
#include <poll.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <termios.h>
#include <unistd.h>

/*
 * For more information about the whole uhid example please read:
 * https://elixir.free-electrons.com/linux/latest/source/samples/uhid/uhid-example.c
 *
 * HID Report Desciptor
 * We emulate a basic 3 button mouse with wheel and 3 keyboard LEDs. This is
 * the report-descriptor as the kernel will parse it:
 *
 * INPUT(1)[INPUT]
 *   Field(0)
 *     Physical(GenericDesktop.Pointer)
 *     Application(GenericDesktop.Mouse)
 *     Usage(3)
 *       Button.0001
 *       Button.0002
 *       Button.0003
 *     Logical Minimum(0)
 *     Logical Maximum(1)
 *     Report Size(1)
 *     Report Count(3)
 *     Report Offset(0)
 *     Flags( Variable Absolute )
 *   Field(1)
 *     Physical(GenericDesktop.Pointer)
 *     Application(GenericDesktop.Mouse)
 *     Usage(3)
 *       GenericDesktop.X
 *       GenericDesktop.Y
 *       GenericDesktop.Wheel
 *     Logical Minimum(-128)
 *     Logical Maximum(127)
 *     Report Size(8)
 *     Report Count(3)
 *     Report Offset(8)
 *     Flags( Variable Relative )
 * OUTPUT(2)[OUTPUT]
 *   Field(0)
 *     Application(GenericDesktop.Keyboard)
 *     Usage(3)
 *       LED.NumLock
 *       LED.CapsLock
 *       LED.ScrollLock
 *     Logical Minimum(0)
 *     Logical Maximum(1)
 *     Report Size(1)
 *     Report Count(3)
 *     Report Offset(0)
 *     Flags( Variable Absolute )
 *
 * This is the mapping that we expect:
 *   Button.0001 ---> Key.LeftBtn
 *   Button.0002 ---> Key.RightBtn
 *   Button.0003 ---> Key.MiddleBtn
 *   GenericDesktop.X ---> Relative.X
 *   GenericDesktop.Y ---> Relative.Y
 *   GenericDesktop.Wheel ---> Relative.Wheel
 *   LED.NumLock ---> LED.NumLock
 *   LED.CapsLock ---> LED.CapsLock
 *   LED.ScrollLock ---> LED.ScrollLock
 *
 * This information can be verified by reading /sys/kernel/debug/hid/<dev>/rdesc
 * This file should print the same information as showed above.
 */

static unsigned char rdesc[] = {
    0x05, 0x01, /* USAGE_PAGE (Generic Desktop) */
    0x09, 0x02, /* USAGE (Mouse) */
    0xa1, 0x01, /* COLLECTION (Application) */
    0x09, 0x01, /* USAGE (Pointer) */
    0xa1, 0x00, /* COLLECTION (Physical) */
    0x85, 0x01, /* REPORT_ID (1) */
    0x05, 0x09, /* USAGE_PAGE (Button) */
    0x19, 0x01, /* USAGE_MINIMUM (Button 1) */
    0x29, 0x03, /* USAGE_MAXIMUM (Button 3) */
    0x15, 0x00, /* LOGICAL_MINIMUM (0) */
    0x25, 0x01, /* LOGICAL_MAXIMUM (1) */
    0x95, 0x03, /* REPORT_COUNT (3) */
    0x75, 0x01, /* REPORT_SIZE (1) */
    0x81, 0x02, /* INPUT (Data,Var,Abs) */
    0x95, 0x01, /* REPORT_COUNT (1) */
    0x75, 0x05, /* REPORT_SIZE (5) */
    0x81, 0x01, /* INPUT (Cnst,Var,Abs) */
    0x05, 0x01, /* USAGE_PAGE (Generic Desktop) */
    0x09, 0x30, /* USAGE (X) */
    0x09, 0x31, /* USAGE (Y) */
    0x09, 0x38, /* USAGE (WHEEL) */
    0x15, 0x81, /* LOGICAL_MINIMUM (-127) */
    0x25, 0x7f, /* LOGICAL_MAXIMUM (127) */
    0x75, 0x08, /* REPORT_SIZE (8) */
    0x95, 0x03, /* REPORT_COUNT (3) */
    0x81, 0x06, /* INPUT (Data,Var,Rel) */
    0xc0, /* END_COLLECTION */
    0xc0, /* END_COLLECTION */
    0x05, 0x01, /* USAGE_PAGE (Generic Desktop) */
    0x09, 0x06, /* USAGE (Keyboard) */
    0xa1, 0x01, /* COLLECTION (Application) */
    0x85, 0x02, /* REPORT_ID (2) */
    0x05, 0x08, /* USAGE_PAGE (Led) */
    0x19, 0x01, /* USAGE_MINIMUM (1) */
    0x29, 0x03, /* USAGE_MAXIMUM (3) */
    0x15, 0x00, /* LOGICAL_MINIMUM (0) */
    0x25, 0x01, /* LOGICAL_MAXIMUM (1) */
    0x95, 0x03, /* REPORT_COUNT (3) */
    0x75, 0x01, /* REPORT_SIZE (1) */
    0x91, 0x02, /* Output (Data,Var,Abs) */
    0x95, 0x01, /* REPORT_COUNT (1) */
    0x75, 0x05, /* REPORT_SIZE (5) */
    0x91, 0x01, /* Output (Cnst,Var,Abs) */
    0xc0, /* END_COLLECTION */
};

static int uhid_write(int fd, const struct uhid_event* ev)
{
    ssize_t ret;

    ret = write(fd, ev, sizeof(*ev));
    if (ret < 0) {
        fprintf(stderr, "Cannot write to uhid: %m\n");
        return -1;
    } else if (ret != sizeof(*ev)) {
        fprintf(stderr, "Wrong size written to uhid: %ld != %lu\n",
            ret, sizeof(*ev));
        return -1;
    }
    return 0;
}

static int create(int fd)
{
    struct uhid_event ev = {
        .type = UHID_CREATE,
        .u = {.create.rd_data = rdesc,
            .create.rd_size = sizeof(rdesc),
            .create.bus = BUS_USB,
            .create.vendor = 0x15d9,
            .create.product = 0x0a37,
            .create.version = 0,
            .create.country = 0 }

    };
    strcpy((char*)ev.u.create.name, "test-uhid-device");

    return uhid_write(fd, &ev);
}

static void destroy(int fd)
{
    struct uhid_event ev;

    memset(&ev, 0, sizeof(ev));
    ev.type = UHID_DESTROY;

    uhid_write(fd, &ev);
}

int main(int argc, char** argv)
{
    int fd;
    const char* path = "/dev/uhid";
    int ret;

    printf("Open uhid-cdev %s\n", path);
    fd = open(path, O_RDWR | O_CLOEXEC);
    if (fd < 0) {
        fprintf(stderr, "Cannot open uhid-cdev %s: %m\n", path);
        return EXIT_FAILURE;
    }

    printf("Create uhid device\n");
    ret = create(fd);
    if (ret != 0) {
        close(fd);
        return EXIT_FAILURE;
    }

    printf("Destroy uhid device\n");
    destroy(fd);
    return EXIT_SUCCESS;
}