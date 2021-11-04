
SERVERS := $(addsuffix .mubicdn, \
	new-york-origin01 \
	singapore-origin02 \
	utah-origin01 \
) root@$(shell cat /volumes/work/mubi/.ssh).aws.mubi

default: build

build: main.go
	go build

svcpy.%: main.go
	GOOS=$* GOARCH=amd64 go build -o svcpy.$*

send: $(addprefix send/, $(SERVERS))
send/%: svcpy.linux
	scp -34Cl 102400 "$<" "$*:svcpy"

rclean: $(addprefix rclean/, $(SERVERS))
rclean/%:
	ssh "$*" -- rm -vf -- svcpy

run-sv:
	./svcpy -listen=0:1234

run-cl:
	./svcpy -connect=0:1234

clean:
	go clean
	rm -vf svcpy svcpy.*

xsend: svcpy.linux
	scp -34Cl 8192 "$<" "mert:/srv/$<"

xpull: xsend $(addprefix xpull/, $(SERVERS))
xpull/%:
	ssh "$*" -- curl -#Lko svcpy src.n0pe.me/svcpy.linux
	ssh "$*" -- chmod ug+x svcpy
