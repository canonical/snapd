#include <stdbool.h>

#ifndef CORE_LAUNCHER_UTILS_H
#define CORE_LAUNCHER_UTILS_H

void die(const char *fmt, ...);
bool error(const char *fmt, ...);
void debug(const char *fmt, ...);
void write_string_to_file(const char *filepath, const char *buf);

// snprintf version that dies on any error condition
void must_snprintf(char *str, size_t size, const char *format, ...);

#endif
