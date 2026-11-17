#include <errno.h>
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <unistd.h>

/* Android host adapter only. Keep the launching Java thread alive until this process exits. */
int main(int argc, char **argv) {
    if (argc != 2 || argv[1][0] != '/') {
        fputs("invalid server executable\n", stderr);
        return 126;
    }
    char runner[PATH_MAX], server[PATH_MAX];
    ssize_t count = readlink("/proc/self/exe", runner, sizeof runner - 1);
    if (count < 0 || !realpath(argv[1], server)) {
        fputs("cannot verify packaged executable\n", stderr);
        return 126;
    }
    runner[count] = '\0';
    char *runner_name = strrchr(runner, '/');
    char *server_name = strrchr(server, '/');
    if (!runner_name || !server_name || strcmp(server_name + 1, "libqimeng.so") != 0) return 126;
    *runner_name = '\0';
    char expected[PATH_MAX];
    int written = snprintf(expected, sizeof expected, "%s/libqimeng.so", runner);
    if (written < 0 || written >= (int)sizeof expected || strcmp(expected, server) != 0) {
        fputs("server must be a packaged sibling\n", stderr);
        return 126;
    }
    pid_t parent = getppid();
    if (parent <= 1 || prctl(PR_SET_PDEATHSIG, SIGTERM) != 0 || getppid() != parent) {
        fputs("host exited before server launch\n", stderr);
        return 125;
    }
    char *const server_argv[] = {server, NULL};
    execv(server, server_argv);
    int failure = errno;
    fputs("cannot execute packaged server\n", stderr);
    return failure == ENOENT ? 127 : 126;
}
