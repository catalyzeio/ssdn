GIT_REV=$(shell git rev-parse --short HEAD)

VERSION=0.2.0-dev.0-${GIT_REV}

deb: clean build
	mkdir -p build/usr/bin
	cp -av l2link/l2link build/usr/bin/
	cp -av shadowfax build/usr/bin/
	cp -av dnetns build/usr/bin/

	mkdir -p build/etc
	cp -av conf build/etc/shadowfax

	mkdir -p build/var/run/shadowfax

	mkdir -p build/DEBIAN/
	sed s/VERSION/$(VERSION)/ control.in > build/DEBIAN/control

	find conf -type f | sed s-conf-/etc/shadowfax- > build/DEBIAN/conffiles

	fakeroot dpkg-deb -b build shadowfax-${VERSION}.deb

build:
	go build
	cd l2link && go build

clean:
	go clean
	cd l2link && go clean
	rm -f *.deb
	rm -rf build

.PHONY: clean build
