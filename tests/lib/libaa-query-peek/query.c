#include <dlfcn.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/apparmor.h>

static void *libaa_handle = NULL;
static int (*real_aa_query_label)(uint32_t mask, char *query, size_t size, int *allowed, int *audited);

__attribute__((constructor)) static void init() {
    libaa_handle = dlopen("libapparmor.so.1", RTLD_LAZY);
    if (!libaa_handle) {
        fprintf(stderr, "cannot open libapparmor.so.1: %s\n", dlerror());
        exit(EXIT_FAILURE);
    }

    (void)dlerror(); /* Clear any existing error */

    real_aa_query_label = (int (*)(uint32_t, char *, size_t, int *, int *))dlsym(libaa_handle, "aa_query_label");

    if (real_aa_query_label == NULL) {
        fprintf(stderr, "cannot lookup symbol for aa_query_label: %s\n", dlerror());
        exit(EXIT_FAILURE);
    }
}

__attribute__((destructor)) static void fini() {
    if (libaa_handle) {
        dlclose(libaa_handle);
    }
}

int aa_query_label(uint32_t mask, char *query, size_t size, int *allowed, int *audited) {
    int rc = real_aa_query_label(mask, query, size, allowed, audited);
    char *query_buf = NULL;
    size_t query_buf_size = 0;
    FILE *f = open_memstream(&query_buf, &query_buf_size);
    if (!f) {
        fprintf(stderr, "cannot open memstream\n");
        exit(EXIT_FAILURE);
    }
    for (size_t i = 0; i < size; ++i) {
        int c = query[i];
        /* Escape control characters and space (32). The last of the mediation
         * classes, AA_CLASS_DBUS, has the value 32 and is otherwise confusing to
         * in logs as it comes up just before the string identifying the type of
         * bus (session or system) being used. */
        if (c <= 32)
            fprintf(f, "\\x%02x", c);
        else
            fputc(c, f);
    }
    fflush(f);
    fprintf(stderr,
            "aa_query_label mask:%#x, query:%*s, size:%zd, -> %d, allowed:%#x, "
            "audited:%#x\n",
            mask, (int)query_buf_size, query_buf, size, rc, allowed ? *allowed : 0, audited ? *audited : 0);
    fclose(f);
    free(query_buf);
    return rc;
}
