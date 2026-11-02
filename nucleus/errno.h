/* errno.h - error numbers. Identical to Linux, because the nucleus answers
 * the Linux system-call ABI (ADR-0003): functions return -E<name>. */
#ifndef NUCLEUS_ERRNO_H
#define NUCLEUS_ERRNO_H
#define EPERM      1
#define ENOENT     2
#define EINTR      4
#define EBADF      9
#define EAGAIN    11
#define ENOMEM    12
#define EFAULT    14
#define EEXIST    17
#define ENODEV    19
#define EINVAL    22
#define EMFILE    24
#define ENOSPC    28
#define ENOSYS    38
#define ETIMEDOUT 110
#endif
