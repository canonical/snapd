#define _GNU_SOURCE
#include <linux/watch_queue.h>
#include <stdio.h>
#include <unistd.h>

int main(void) {
  int fd[2];
  if (pipe2(fd, O_NOTIFICATION_PIPE|O_CLOEXEC) < 0) {
    perror("pipe2");
    return 1;
  }
  close(fd[0]);
  close(fd[1]);
  return 0;
}
