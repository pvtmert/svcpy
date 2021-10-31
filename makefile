
default: build

build:
	go build

run-sv:
	./svcpy -listen=0:1234

run-cl:
	./svcpy -connect=0:1234

linux:
	GOOS=linux GOARCH=amd64 go build -o svcpy.linux

send:
	scp -34Cvl 102400 svcpy.linux singapore-origin02.mubicdn:svcpy
	scp -34Cvl 102400 svcpy.linux utah-origin01.mubicdn:svcpy
