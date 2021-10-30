# svcpy: server to server copy

a basic server-to-server copy application.

on a single binary, it can be a server or a client.

## example usage:

- on the server side: `./svcpy -listen=:4321 -path=/srv/ftp`
- on the client side  `./svcpy -connect=1.2.3.4:4321 -path=/mnt/backups/ftp`

---

## notes:

- prevent information leak by taking diff on the server side
- general flow:
	- server starts to listen at port
	- client connects
	- client generates a file-list (local)
	- client sends #-of-files to the server
	- server reads #-of-files from the client
	- client sends filelist as json to the server
	- server reads filelist from the client
	- server generates a diff from the list received and what it has locally
	- server verifies diff in terms of file counts
	- server creates tar archive and pushes to socket
	- client reads the tar archive and unpacks to path specified

## todo:

- add some kind of authentication (token, openid etc)
- generate filelist in deferred fashion.
	- eg: checksums should be async (much like python's yield keyword)
- add compression (gzip/xz) to speed up transfer
- add bandwidth limitation
