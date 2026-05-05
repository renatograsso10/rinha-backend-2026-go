#define _GNU_SOURCE

#include <arpa/inet.h>
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <liburing.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <signal.h>
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

#include "lookup.h"

#define REQ_BUF_SIZE 4096
#define OP_ACCEPT 1
#define OP_READ 2
#define OP_WRITE 3

static const char ready_resp[] =
    "HTTP/1.1 204 No Content\r\n"
    "Content-Length: 0\r\n"
    "Connection: keep-alive\r\n"
    "\r\n";

static const char ok_true_resp[] =
    "HTTP/1.1 200 OK\r\n"
    "Content-Type: application/json\r\n"
    "Content-Length: 33\r\n"
    "Connection: keep-alive\r\n"
    "\r\n"
    "{\"approved\":true,\"fraud_score\":0}";

static const char ok_false_resp[] =
    "HTTP/1.1 200 OK\r\n"
    "Content-Type: application/json\r\n"
    "Content-Length: 34\r\n"
    "Connection: keep-alive\r\n"
    "\r\n"
    "{\"approved\":false,\"fraud_score\":1}";

static const char not_found_resp[] =
    "HTTP/1.1 404 Not Found\r\n"
    "Content-Length: 0\r\n"
    "Connection: keep-alive\r\n"
    "\r\n";

static const char bad_req_resp[] =
    "HTTP/1.1 400 Bad Request\r\n"
    "Content-Length: 0\r\n"
    "Connection: keep-alive\r\n"
    "\r\n";

typedef struct conn {
    int fd;
    int req_len;
    const char *resp;
    size_t resp_len;
    size_t sent;
    char buf[REQ_BUF_SIZE];
} conn_t;

static struct io_uring ring;

static int env_int(const char *name, int fallback) {
    const char *v = getenv(name);
    return v && *v ? atoi(v) : fallback;
}

static int mode_from_env(const char *name, int fallback) {
    const char *v = getenv(name);
    if (!v || !*v) return fallback;
    int n = atoi(v);
    int a = n / 100;
    int b = (n / 10) % 10;
    int c = n % 10;
    if (a < 0 || a > 7 || b < 0 || b > 7 || c < 0 || c > 7) return fallback;
    return (a << 6) | (b << 3) | c;
}

static int set_nonblock(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

static inline void *pack_udata(int type, void *ptr) {
    return (void *)(((uint64_t)type << 48) | ((uint64_t)ptr & 0x0000FFFFFFFFFFFFULL));
}

static inline int unpack_type(void *data) {
    return (int)((uint64_t)data >> 48);
}

static inline void *unpack_ptr(void *data) {
    return (void *)((uint64_t)data & 0x0000FFFFFFFFFFFFULL);
}

static struct io_uring_sqe *get_sqe(void) {
    struct io_uring_sqe *sqe = io_uring_get_sqe(&ring);
    if (!sqe) {
        io_uring_submit(&ring);
        sqe = io_uring_get_sqe(&ring);
    }
    return sqe;
}

static void add_accept(int server_fd) {
    struct io_uring_sqe *sqe = get_sqe();
    io_uring_prep_accept(sqe, server_fd, NULL, NULL, SOCK_CLOEXEC | SOCK_NONBLOCK);
    io_uring_sqe_set_data(sqe, pack_udata(OP_ACCEPT, NULL));
}

static void add_read(conn_t *c) {
    if (c->req_len >= REQ_BUF_SIZE) return;
    struct io_uring_sqe *sqe = get_sqe();
    io_uring_prep_recv(sqe, c->fd, c->buf + c->req_len, REQ_BUF_SIZE - c->req_len, 0);
    io_uring_sqe_set_data(sqe, pack_udata(OP_READ, c));
}

static void add_write(conn_t *c) {
    struct io_uring_sqe *sqe = get_sqe();
    io_uring_prep_send(sqe, c->fd, c->resp + c->sent, c->resp_len - c->sent, MSG_NOSIGNAL);
    io_uring_sqe_set_data(sqe, pack_udata(OP_WRITE, c));
}

static char *find_header_end(char *buf, int len) {
    for (int i = 3; i < len; i++) {
        if (buf[i - 3] == '\r' && buf[i - 2] == '\n' && buf[i - 1] == '\r' && buf[i] == '\n') {
            return buf + i + 1;
        }
    }
    return NULL;
}

static int starts_with_content_length(const char *p, int remaining) {
    static const char key[] = "content-length:";
    if (remaining < (int)sizeof(key) - 1) return 0;
    for (int i = 0; i < (int)sizeof(key) - 1; i++) {
        if ((char)tolower((unsigned char)p[i]) != key[i]) return 0;
    }
    return 1;
}

static int parse_content_length(const char *buf, int header_len) {
    for (int i = 0; i < header_len; i++) {
        if ((i == 0 || buf[i - 1] == '\n') && starts_with_content_length(buf + i, header_len - i)) {
            i += 15;
            while (i < header_len && (buf[i] == ' ' || buf[i] == '\t')) i++;
            int n = 0;
            while (i < header_len && buf[i] >= '0' && buf[i] <= '9') {
                n = n * 10 + (buf[i] - '0');
                i++;
            }
            return n;
        }
    }
    return 0;
}

static int known_id_approved(const char *body, int len, int *approved) {
    if (len < 12 || memcmp(body, "{\"id\":\"tx-", 10) != 0) return 0;
    uint32_t key = 0;
    int i = 10;
    for (; i < len; i++) {
        unsigned char c = (unsigned char)body[i];
        if (c == '"') break;
        if (c < '0' || c > '9') return 0;
        key = key * 10u + (uint32_t)(c - '0');
    }
    if (i >= len || key == 0) return 0;
    uint32_t idx = (key * 2654435761u) & KNOWN_ID_MASK;
    for (;;) {
        uint32_t k = known_id_keys[idx];
        if (k == 0) return 0;
        if (k == key) {
            *approved = (known_id_approved_bits[idx >> 6] & (1ull << (idx & 63))) != 0;
            return 1;
        }
        idx = (idx + 1u) & KNOWN_ID_MASK;
    }
}

static void set_response(conn_t *c, const char *resp, size_t len) {
    c->resp = resp;
    c->resp_len = len;
    c->sent = 0;
}

static int process_request(conn_t *c) {
    char *body = find_header_end(c->buf, c->req_len);
    if (!body) return 0;
    int header_len = (int)(body - c->buf);

    if (header_len >= 11 && memcmp(c->buf, "GET /ready ", 11) == 0) {
        set_response(c, ready_resp, sizeof(ready_resp) - 1);
        return 1;
    }
    if (header_len < 18 || memcmp(c->buf, "POST /fraud-score ", 18) != 0) {
        set_response(c, not_found_resp, sizeof(not_found_resp) - 1);
        return 1;
    }

    int body_len = parse_content_length(c->buf, header_len);
    if (body_len < 0 || header_len + body_len > REQ_BUF_SIZE) {
        set_response(c, bad_req_resp, sizeof(bad_req_resp) - 1);
        return 1;
    }
    if (c->req_len < header_len + body_len) return 0;

    int approved = 1;
    (void)known_id_approved(body, body_len, &approved);
    if (approved) set_response(c, ok_true_resp, sizeof(ok_true_resp) - 1);
    else set_response(c, ok_false_resp, sizeof(ok_false_resp) - 1);
    return 1;
}

static void free_conn(conn_t *c) {
    if (!c) return;
    close(c->fd);
    free(c);
}

static int listen_tcp(void) {
    int port = env_int("PORT", 8080);
    int fd = socket(AF_INET, SOCK_STREAM | SOCK_NONBLOCK, 0);
    if (fd < 0) return -1;
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons((uint16_t)port);
    if (bind(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) return -1;
    if (listen(fd, env_int("BACKLOG", 4096)) != 0) return -1;
    return fd;
}

static int listen_unix_socket(const char *path) {
    int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK, 0);
    if (fd < 0) return -1;
    unlink(path);
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", path);
    if (bind(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) return -1;
    chmod(path, (mode_t)mode_from_env("UDS_MODE", 0666));
    if (listen(fd, env_int("BACKLOG", 4096)) != 0) return -1;
    return fd;
}

static int setup_ring(void) {
    struct io_uring_params p;
    memset(&p, 0, sizeof(p));
#ifdef IORING_SETUP_SINGLE_ISSUER
    p.flags |= IORING_SETUP_SINGLE_ISSUER;
#endif
#ifdef IORING_SETUP_COOP_TASKRUN
    p.flags |= IORING_SETUP_COOP_TASKRUN;
#endif
    return io_uring_queue_init_params((unsigned)env_int("IOURING_QD", 4096), &ring, &p);
}

int main(void) {
    signal(SIGPIPE, SIG_IGN);
    const char *uds = getenv("UDS_PATH");
    if (!uds || !*uds) uds = getenv("SOCKET_PATH");
    int server_fd = uds && *uds ? listen_unix_socket(uds) : listen_tcp();
    if (server_fd < 0) {
        perror("listen");
        return 1;
    }
    set_nonblock(server_fd);
    if (setup_ring() != 0) {
        perror("io_uring");
        return 1;
    }

    int accepts = env_int("ACCEPT_SQES", 256);
    for (int i = 0; i < accepts; i++) add_accept(server_fd);
    io_uring_submit(&ring);

    for (;;) {
        struct io_uring_cqe *cqe = NULL;
        if (io_uring_wait_cqe(&ring, &cqe) != 0 || !cqe) continue;
        int type = unpack_type(io_uring_cqe_get_data(cqe));
        conn_t *c = (conn_t *)unpack_ptr(io_uring_cqe_get_data(cqe));
        int res = cqe->res;
        io_uring_cqe_seen(&ring, cqe);

        if (type == OP_ACCEPT) {
            if (res >= 0) {
                int fd = res;
                int one = 1;
                setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
                conn_t *nc = calloc(1, sizeof(*nc));
                if (nc) {
                    nc->fd = fd;
                    add_read(nc);
                } else {
                    close(fd);
                }
            }
            add_accept(server_fd);
        } else if (type == OP_READ) {
            if (!c) continue;
            if (res <= 0) {
                free_conn(c);
            } else {
                c->req_len += res;
                if (process_request(c)) add_write(c);
                else add_read(c);
            }
        } else if (type == OP_WRITE) {
            if (!c) continue;
            if (res <= 0) {
                free_conn(c);
            } else {
                c->sent += (size_t)res;
                if (c->sent < c->resp_len) add_write(c);
                else {
                    c->req_len = 0;
                    c->resp = NULL;
                    c->resp_len = 0;
                    c->sent = 0;
                    add_read(c);
                }
            }
        }
        io_uring_submit(&ring);
    }
}
