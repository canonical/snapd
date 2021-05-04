/***
  This file is part of PulseAudio.
  PulseAudio is free software; you can redistribute it and/or modify
  it under the terms of the GNU Lesser General Public License as published
  by the Free Software Foundation; either version 2.1 of the License,
  or (at your option) any later version.
  PulseAudio is distributed in the hope that it will be useful, but
  WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
  General Public License for more details.
  You should have received a copy of the GNU Lesser General Public License
  along with PulseAudio; if not, see <http://www.gnu.org/licenses/>.
***/
#ifdef HAVE_CONFIG_H
#include <config.h>
#endif
#include <stdio.h>
#include <unistd.h>
#include <string.h>
#include <errno.h>
#include <pulse/simple.h>
#include <pulse/error.h>
#define BUFSIZE 1024
/* A simple routine calling UNIX write() in a loop */
static ssize_t loop_write(int fd, const void*data, size_t size) {
    ssize_t ret = 0;
    while (size > 0) {
        ssize_t r;
        if ((r = write(fd, data, size)) < 0)
            return r;
        if (r == 0)
            break;
        ret += r;
        data = (const uint8_t*) data + r;
        size -= (size_t) r;
    }
    return ret;
}
int main(int argc, char*argv[]) {
    /* The sample type to use */
    static const pa_sample_spec ss = {
        .format = PA_SAMPLE_S16LE,
        .rate = 44100,
        .channels = 2
    };
    pa_simple *s = NULL;
    int ret = 1;
    int error;
    /* Create the recording stream */
    if (!(s = pa_simple_new(NULL, argv[0], PA_STREAM_RECORD, NULL, "record", &ss, NULL, NULL, &error))) {
        fprintf(stderr, __FILE__": pa_simple_new() failed: %s\n", pa_strerror(error));
        goto finish;
    }

    /* read (record) once */
    uint8_t buf[BUFSIZE];
    /* Record some data ... */
    if (pa_simple_read(s, buf, sizeof(buf), &error) < 0) {
        fprintf(stderr, __FILE__": pa_simple_read() failed: %s\n", pa_strerror(error));
        goto finish;
    }
    /* And write it to STDOUT */
    if (loop_write(STDOUT_FILENO, buf, sizeof(buf)) != sizeof(buf)) {
        fprintf(stderr, __FILE__": write() failed: %s\n", strerror(errno));
        goto finish;
    }

    ret = 0;
finish:
    if (s)
        pa_simple_free(s);
    return ret;
}
