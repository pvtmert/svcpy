
default: build

build:
	go build

run-sv:
	./svcpy -listen=0:1234

run-cl:
	./svcpy -connect=0:1234
