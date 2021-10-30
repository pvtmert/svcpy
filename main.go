package main

import (
	"archive/tar"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileEntry struct {
	Time time.Time   `json:"time,omitempty"`
	Hash string      `json:"hash,omitempty"`
	Size int64       `json:"size,omitempty"`
	Name string      `json:"name,omitempty"`
	Path string      `json:"path,omitempty"`
	Mode fs.FileMode `json:"mode,omitempty"`
	Info os.FileInfo `json:"-"`
}

//func dummy(conn net.Conn) {
//	conn.Write([]byte("hello!"))
//	buf := make([]byte, 32)
//	conn.Read(buf[0:])
//	fmt.Println("test:", buf)
//}

func compareFileEntries(first FileEntry, second FileEntry) bool {
	if first.Name != second.Name {
		return false
	}
	if first.Path != second.Path {
		//fmt.Println("compareFileEntries:path:", first, second)
		return false
	}
	if first.Size != second.Size {
		return false
	}
	if first.Hash != second.Hash {
		return false
	}
	// if !first.Time.UTC().Equal(second.Time.UTC()) {
	// 	return false
	// }
	return true
}

func contains(haystack []FileEntry, needle FileEntry) bool {
	for _, v := range haystack {
		if compareFileEntries(v, needle) {
			return true
		}
	}
	return false
}

func checksum(path string) string {
	file, err := os.Open(path)
	if err != nil {
		log.Fatalln(err)
		return ""
	}
	defer file.Close()
	hash := md5.New()
	io.Copy(hash, file)
	sum := hash.Sum(nil)
	hex := hex.EncodeToString(sum)
	return hex
}

func discardFiles(all []FileEntry, subset []FileEntry) []FileEntry {
	files := make([]FileEntry, 0)
	for _, file := range all {
		if !contains(subset, file) {
			files = append(files, file)
		}
	}
	return files
}

func listFiles(dir string) (files []FileEntry) {
	files = make([]FileEntry, 0)
	filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			fmt.Println("listfiles:walk:", err)
			return err
		}
		//if !info.Mode().IsRegular() {
		//	return errors.New("Irregular file:" + info.ModTime().String())
		//}
		//fileChecksum := ""
		//if !info.IsDir() {
		//	fileChecksum = checksum(path)
		//}
		file := FileEntry{
			Name: info.Name(),
			Path: strings.TrimPrefix(strings.Replace(path, dir, "", 1), string(filepath.Separator)),
			Time: info.ModTime(),
			Size: info.Size(),
			Hash: checksum(path),
			Mode: info.Mode(),
			Info: info,
		}
		files = append(files, file)
		return nil
	})
	return files
}

func archiveFiles(files []FileEntry, stream io.Writer, path string) error {
	tarWriter := tar.NewWriter(stream)
	//defer tarWriter.Close()
	for _, file := range files {
		header, err := tar.FileInfoHeader(file.Info, file.Path)
		header.Name = file.Path
		//header.Name = strings.TrimPrefix(strings.Replace(file.Path, path, "", 1), string(filepath.Separator))
		if err != nil {
			log.Println("archive:header:info:", err)
			continue
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			log.Println("archive:header:tar:", err)
			continue
		}
		if file.Info.IsDir() {
			continue
		}
		fp, err := os.Open(filepath.Join(path, file.Path))
		if err != nil {
			log.Println("archive:open:", err)
			continue
		}
		defer fp.Close()
		if _, err := io.Copy(tarWriter, fp); err != nil {
			log.Println("archive:copy:", err)
			continue
		}
		continue
	}
	tarWriter.Close()
	return nil
}

func unarchiveFiles(conn net.Conn, path string) {
	tarReader := tar.NewReader(conn)
	if err := os.MkdirAll(path, 0755); err != nil {
		fmt.Println("unarchive:mkdir:", err)
		return
	}
	for {
		header, err := tarReader.Next()
		switch {
		case err == io.EOF:
			return
		case err != nil:
			fmt.Println("download:tar:", err)
			return
		case header == nil:
			continue
		}
		fmt.Println("download:tar:", header, err)
		target := filepath.Join(path, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					fmt.Println("download:tar:dir:", err)
				}
			}
		case tar.TypeReg:
			fp, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			defer fp.Close()
			if err != nil {
				fmt.Println("download:tar:open:", err)
				continue
			}
			if _, err := io.Copy(fp, tarReader); err != nil {
				fmt.Println("download:tar:copy:", err)
				continue
			}
			continue
		}
	}
	return
}

func handshake(conn net.Conn) (count uint32, files []FileEntry, err error) {
	if err = binary.Read(conn, binary.BigEndian, &count); err != nil {
		fmt.Println("handshake:count:", err)
		return 0, nil, err
	}
	files = make([]FileEntry, count)
	if err = json.NewDecoder(conn).Decode(&files); err != nil {
		fmt.Println("handshake:json:", err)
		return 0, nil, err
	}
	return count, files, nil
}

func handle(conn net.Conn, path string) {
	defer conn.Close()
	fmt.Println("handle:", path, conn.LocalAddr(), conn.RemoteAddr())

	remoteCount, clientFiles, err := handshake(conn)
	if err != nil {
		fmt.Println("handle:handshake:", err)
		return
	}

	localFiles := listFiles(path)
	diffFiles := discardFiles(localFiles, clientFiles)

	if len(diffFiles) != int(math.Abs(float64(uint32(len(localFiles)) - remoteCount))) {
		fmt.Println("handle:diff:(diff/local/remote):", len(diffFiles), len(localFiles), remoteCount)
	}

	//if err := json.NewEncoder(conn).Encode(diffFiles); err != nil {
	//	fmt.Println("handle:json:encode:", err)
	//	return
	//}
	archiveFiles(diffFiles, conn, path)
	return
}

func serve(listener net.Listener, path string) {
	fmt.Println("serve:listening:", path, listener, listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("serve:accept:", err)
			return
		}
		go handle(conn, path)
		continue
	}
	return
}

func download(conn net.Conn, path string) {
	defer conn.Close()
	if err := os.MkdirAll(path, 0755); err != nil {
		fmt.Println("download:mkdir:", err)
		return
	}
	filesLocal := listFiles(path)
	countLocal := len(filesLocal)
	if err := binary.Write(conn, binary.BigEndian, uint32(countLocal)); err != nil {
		fmt.Println("download:count:", err)
		return
	}
	if err := json.NewEncoder(conn).Encode(filesLocal); err != nil {
		fmt.Println("download:json:", err)
		return
	}
	unarchiveFiles(conn, path)
	return
}

func test() {
	testFiles := []FileEntry{
		{
			Path: "readme.md",
			Name: "readme.md",
			Time: time.Now(),
			Size: 757,
			Hash: "c542ca2aebab8c302e4a296b9acd7b9c",
		},
		{
			Path: "makefile",
			Name: "makefile",
			Time: time.Now(),
			Size: 30,
			Hash: "2db2523f146590db96961dd692ed37b3",
		},
	}
	localFiles := listFiles(".")
	diffFiles := discardFiles(localFiles, testFiles)
	for _, file := range diffFiles {
		fmt.Println(file)
	}
	return
}

func main() {
	listen  := flag.String("listen",  "", "Listen/Bind address:port")
	connect := flag.String("connect", "", "Server address:port")
	path    := flag.String("path",    "", "Path to use for files")
	flag.Parse()
	if *path == "" {
		*path = "./"
	}
	if *connect != "" {
		fmt.Println("main:connect:", *connect)
		conn, err := net.Dial("tcp", *connect)
		if err != nil {
			fmt.Println("main:connect:err:", err)
			return
		}
		download(conn, *path)
	}
	if *listen != "" {
		fmt.Println("main:listen:", *listen)
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			fmt.Println("main:listen:err:", err)
			return
		}
		serve(listener, *path)
	}
	return
}
