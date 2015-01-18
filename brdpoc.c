#include <fcntl.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>

#include <sys/socket.h>
#include <sys/ioctl.h>

#include <linux/if.h>
#include <linux/if_tun.h>

int tun_alloc(char *dev)
{
    struct ifreq ifr;
    int fd, err;

    if ((fd = open("/dev/net/tun", O_RDWR)) < 0) {
        printf("open failed.\n");
        return -1;
    }

    memset(&ifr, 0, sizeof(ifr));

    /* Flags: IFF_TUN   - TUN device (no Ethernet headers)
     *        IFF_TAP   - TAP device
     *
     *        IFF_NO_PI - Do not provide packet information
     */
    ifr.ifr_flags = IFF_TUN | IFF_NO_PI;
    if (*dev) {
        strncpy(ifr.ifr_name, dev, IFNAMSIZ);
    }


    if ((err = ioctl(fd, TUNSETIFF, (void *) &ifr)) < 0) {
        close(fd);
        printf("no dice.\n");
        return err;
    }

    strcpy(dev, ifr.ifr_name);
    printf("got a dev.\n");
    return fd;
}

int main(int argc, char **argv) {
    char iname[1024];
    strcpy(iname, "tun%d");
    int fd = tun_alloc(iname);
    printf("%s %d\n", iname, fd);

    unsigned char buff[8192];
    while (1) {
        ssize_t s = read(fd, buff, 8192);
        printf("read %d\n", s);
        write(fd, buff, s);
    }

    return 0;
}
