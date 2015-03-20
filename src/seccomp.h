#include <stdbool.h>

#ifndef CORE_LAUNCHER_SECCOMP_H
#define CORE_LAUNCHER_SECCOMP_H

int seccomp_load_filters(const char *filter_profile);

#endif
