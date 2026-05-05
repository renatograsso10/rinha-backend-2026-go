#define _GNU_SOURCE

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <netinet/tcp.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

#define MAX_EVENTS 2048
#define BUF_SIZE 8192

typedef struct side side_t;

typedef struct pair {
    side_t *client;
    side_t *backend;
} pair_t;

struct side {
    int fd;
    int closed;
    pair_t *pair;
    side_t *peer;
    char buf[BUF_SIZE];
    size_t off;
    size_t len;
};

static const char *backend_paths[2] = {"/sockets/api1.sock", "/sockets/api2.sock"};
static uint32_t rr;

static int env_int(const char *name, int fallback) {
    const char *v = getenv(name);
    return v && *v ? atoi(v) : fallback;
}

static int set_nonblock(int fd) {
    int flags = fcntl(fd, F_GETFL, 0);
    if (flags < 0) return -1;
    return fcntl(fd, F_SETFL, flags | O_NONBLOCK);
}

static int mod_side(int epfd, side_t *s, uint32_t events) {
    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = events | EPOLLRDHUP;
    ev.data.ptr = s;
    return epoll_ctl(epfd, EPOLL_CTL_MOD, s->fd, &ev);
}

static void free_pair(int epfd, pair_t *p) {
    if (!p) return;
    if (p->client) {
        epoll_ctl(epfd, EPOLL_CTL_DEL, p->client->fd, NULL);
        close(p->client->fd);
        free(p->client);
    }
    if (p->backend) {
        epoll_ctl(epfd, EPOLL_CTL_DEL, p->backend->fd, NULL);
        close(p->backend->fd);
        free(p->backend);
    }
    free(p);
}

static int listen_tcp(void) {
    int fd = socket(AF_INET, SOCK_STREAM | SOCK_NONBLOCK, 0);
    if (fd < 0) return -1;
    int one = 1;
    setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &one, sizeof(one));
    setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));

    struct sockaddr_in addr;
    memset(&addr, 0, sizeof(addr));
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_ANY);
    addr.sin_port = htons((uint16_t)env_int("PORT", 9999));
    if (bind(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) return -1;
    if (listen(fd, env_int("BACKLOG", 8192)) != 0) return -1;
    return fd;
}

static int connect_backend(void) {
    const char *path = backend_paths[(rr++) & 1u];
    int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) return -1;
    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    snprintf(addr.sun_path, sizeof(addr.sun_path), "%s", path);
    if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) != 0) {
        close(fd);
        return -1;
    }
    set_nonblock(fd);
    return fd;
}

static void add_pair(int epfd, int client_fd) {
    int backend_fd = connect_backend();
    if (backend_fd < 0) {
        close(client_fd);
        return;
    }

    pair_t *p = calloc(1, sizeof(*p));
    side_t *c = calloc(1, sizeof(*c));
    side_t *b = calloc(1, sizeof(*b));
    if (!p || !c || !b) {
        close(client_fd);
        close(backend_fd);
        free(p);
        free(c);
        free(b);
        return;
    }

    c->fd = client_fd;
    b->fd = backend_fd;
    c->pair = p;
    b->pair = p;
    c->peer = b;
    b->peer = c;
    p->client = c;
    p->backend = b;

    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = EPOLLIN | EPOLLRDHUP;
    ev.data.ptr = c;
    if (epoll_ctl(epfd, EPOLL_CTL_ADD, client_fd, &ev) != 0) {
        free_pair(epfd, p);
        return;
    }
    ev.data.ptr = b;
    if (epoll_ctl(epfd, EPOLL_CTL_ADD, backend_fd, &ev) != 0) {
        free_pair(epfd, p);
    }
}

static void accept_loop(int epfd, int server_fd) {
    for (;;) {
        int fd = accept4(server_fd, NULL, NULL, SOCK_NONBLOCK | SOCK_CLOEXEC);
        if (fd >= 0) {
            int one = 1;
            setsockopt(fd, IPPROTO_TCP, TCP_NODELAY, &one, sizeof(one));
            add_pair(epfd, fd);
            continue;
        }
        if (errno == EAGAIN || errno == EWOULDBLOCK) return;
        if (errno == EINTR) continue;
        return;
    }
}

static void update_interest(int epfd, side_t *s) {
    uint32_t events = 0;
    if (!s->closed && s->peer->len < BUF_SIZE) events |= EPOLLIN;
    if (s->len > s->off) events |= EPOLLOUT;
    if (events == 0 && s->closed && s->peer->closed) {
        free_pair(epfd, s->pair);
        return;
    }
    mod_side(epfd, s, events);
}

static void read_side(int epfd, side_t *s) {
    side_t *dst = s->peer;
    while (dst->len < BUF_SIZE) {
        ssize_t n = recv(s->fd, dst->buf + dst->len, BUF_SIZE - dst->len, 0);
        if (n > 0) {
            dst->len += (size_t)n;
            continue;
        }
        if (n == 0) {
            s->closed = 1;
            shutdown(dst->fd, SHUT_WR);
            break;
        }
        if (errno == EAGAIN || errno == EWOULDBLOCK) break;
        if (errno == EINTR) continue;
        free_pair(epfd, s->pair);
        return;
    }
    update_interest(epfd, s);
    update_interest(epfd, dst);
}

static void write_side(int epfd, side_t *s) {
    while (s->off < s->len) {
        ssize_t n = send(s->fd, s->buf + s->off, s->len - s->off, MSG_NOSIGNAL);
        if (n > 0) {
            s->off += (size_t)n;
            continue;
        }
        if (errno == EAGAIN || errno == EWOULDBLOCK) break;
        if (errno == EINTR) continue;
        free_pair(epfd, s->pair);
        return;
    }
    if (s->off == s->len) {
        s->off = 0;
        s->len = 0;
    }
    update_interest(epfd, s);
    update_interest(epfd, s->peer);
}

int main(void) {
    signal(SIGPIPE, SIG_IGN);
    const char *b1 = getenv("BACKEND1");
    const char *b2 = getenv("BACKEND2");
    if (b1 && *b1) backend_paths[0] = b1;
    if (b2 && *b2) backend_paths[1] = b2;

    int server_fd = listen_tcp();
    if (server_fd < 0) {
        perror("listen");
        return 1;
    }

    int epfd = epoll_create1(EPOLL_CLOEXEC);
    if (epfd < 0) {
        perror("epoll");
        return 1;
    }

    struct epoll_event ev;
    memset(&ev, 0, sizeof(ev));
    ev.events = EPOLLIN;
    ev.data.ptr = NULL;
    if (epoll_ctl(epfd, EPOLL_CTL_ADD, server_fd, &ev) != 0) {
        perror("epoll_ctl");
        return 1;
    }

    struct epoll_event events[MAX_EVENTS];
    for (;;) {
        int n = epoll_wait(epfd, events, MAX_EVENTS, -1);
        if (n < 0) {
            if (errno == EINTR) continue;
            perror("epoll_wait");
            return 1;
        }
        for (int i = 0; i < n; i++) {
            side_t *s = (side_t *)events[i].data.ptr;
            if (!s) {
                accept_loop(epfd, server_fd);
                continue;
            }
            if (events[i].events & (EPOLLERR | EPOLLHUP | EPOLLRDHUP)) {
                free_pair(epfd, s->pair);
            } else {
                if (events[i].events & EPOLLIN) read_side(epfd, s);
                if (events[i].events & EPOLLOUT) write_side(epfd, s);
            }
        }
    }
}
