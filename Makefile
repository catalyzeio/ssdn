GIT_REV=$(shell git rev-parse --short HEAD)
VERSION=0.6.0-dev-1-${GIT_REV}

MULTI=ssdn
ALIASES=l2link l3bridge l3direct cdns

all: test ${MULTI} ${ALIASES}

test:
	go test ./...

ssdn:
	go build

l2link: ${MULTI}
	ln -sf ${MULTI} $@

l3bridge: ${MULTI}
	ln -sf ${MULTI} $@

l3direct: ${MULTI}
	ln -sf ${MULTI} $@

cdns: ${MULTI}
	ln -sf ${MULTI} $@

deb: all
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

	# patch up control file
	mkdir -p build/DEBIAN/
	sed s/VERSION/$(VERSION)/ control.in > build/DEBIAN/control

	# record config files
	find conf -type f | sed s-conf-/etc/ssdn- > build/DEBIAN/conffiles

	# build .deb file
	fakeroot dpkg-deb -b build ssdn-${VERSION}.deb

clean:
	rm -f *.deb
	rm -rf build
	rm -f ${MULTI} ${ALIASES}

.PHONY: clean all test deb ${MULTI}
