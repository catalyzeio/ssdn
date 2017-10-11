GIT_REV=$(shell git rev-parse --short HEAD)
VERSION=0.8.5-dev-0
ITERATION=${GIT_REV}

MULTI=ssdn
ALIASES=l2link l3bridge l3direct l3node cdns

all: test ${MULTI} ${ALIASES}

test:
	go test ./...

ssdn:
	go build -ldflags="-w -s"

l2link: ${MULTI}
	ln -sf ${MULTI} $@

l3bridge: ${MULTI}
	ln -sf ${MULTI} $@

l3direct: ${MULTI}
	ln -sf ${MULTI} $@

l3node: ${MULTI}
	ln -sf ${MULTI} $@

cdns: ${MULTI}
	ln -sf ${MULTI} $@

deb: pkgs

pkgs: all
	# reset build directory
	rm -rf build

	# add binaries
	mkdir -p build/usr/sbin
	cp -av ${MULTI} ${ALIASES} dnetns build/usr/sbin/

	# add config files
	mkdir -p build/etc
	cp -av conf build/etc/ssdn

	# add docs
	mkdir -p build/usr/share/doc/ssdn
	cp README.md build/usr/share/doc/ssdn/

	# add placeholder for run directory
	mkdir -p build/var/run/ssdn

	# build .deb and .rpm files
	fakeroot fpm -s dir -t rpm -n ssdn -v ${VERSION} -a amd64 --config-files /etc/ssdn --iteration ${ITERATION} -C build .
	fakeroot fpm -s dir -t deb -n ssdn -v ${VERSION} -a amd64 --config-files /etc/ssdn --iteration ${ITERATION} -C build .

clean:
	rm -f *.deb
	rm -f *.rpm
	rm -rf build
	rm -f ${MULTI} ${ALIASES}

.PHONY: clean all test deb ${MULTI}
