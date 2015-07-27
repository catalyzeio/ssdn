image:
	(cd root && fakeroot tar cvf ../root.tar .)
	docker build -t cdns .

clean:
	rm -f root.tar

.PHONY: image
