#define _GNU_SOURCE
#include <termios.h>
#include <sys/ioctl.h>
#include <unistd.h>
#include <stdio.h>
#include <sys/syscall.h>
#include <errno.h>

static int ioctl64(int fd, unsigned long nr, void *arg) {
  errno = 0;
  return syscall(__NR_ioctl, fd, nr, arg);
}

int main(void) {
  int res;
  char pushmeback = '#';
  res = ioctl64(0, TIOCSTI, &pushmeback);
  printf("normal TIOCSTI: %d (%m)\n", res);
  res = ioctl64(0, TIOCSTI | (1UL<<32), &pushmeback);
  printf("high-bit-set TIOCSTI: %d (%m)\n", res);
  return res;
}
