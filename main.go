package main

import (
	"archive/tar"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
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

func compareFileEntries(first FileEntry, second FileEntry) bool {
	if first.Name != second.Name {
		return false
	}
	if first.Path != second.Path {
		//log.Println("compareFileEntries:path:", first, second)
		return false
	}
	if first.Size != second.Size {
		return false
	}
	if first.Hash != "" && second.Hash != "" {
		if first.Hash != second.Hash {
			return false
		}
	}
	// if !first.Time.UTC().Equal(second.Time.UTC()) {
	// 	return false
	// }
	return true
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
	lookup := make(map[string]FileEntry)
	for _, file := range subset {
		lookup[file.Path] = file
	}
	for _, file := range all {
		if _, exists := lookup[file.Path]; exists {
			continue
		}
		if !compareFileEntries(file, lookup[file.Path]) {
			files = append(files, file)
		}
	}
	return files
}

func listFiles(dir string, chksum bool) (files []FileEntry) {
	files = make([]FileEntry, 0)
	filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			log.Println("listfiles:walk:", err)
			return err
		}
		//if !info.Mode().IsRegular() {
		//	return errors.New("Irregular file:" + info.ModTime().String())
		//}
		fileChecksum := ""
		if chksum && !info.IsDir() {
			fileChecksum = checksum(path)
		}
		file := FileEntry{
			Name: info.Name(),
			Path: strings.TrimPrefix(strings.Replace(path, dir, "", 1), string(filepath.Separator)),
			Time: info.ModTime(),
			Size: info.Size(),
			Hash: fileChecksum,
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
		log.Println("unarchive:mkdir:", err)
		return
	}
	for {
		header, err := tarReader.Next()
		switch {
		case err == io.EOF:
			return
		case err != nil:
			log.Println("download:tar:", err)
			return
		case header == nil:
			continue
		}
		log.Println("download:tar:", header, err)
		target := filepath.Join(path, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					log.Println("download:tar:dir:", err)
				}
			}
		case tar.TypeReg:
			fp, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			defer fp.Close()
			if err != nil {
				log.Println("download:tar:open:", err)
				continue
			}
			if _, err := io.Copy(fp, tarReader); err != nil {
				log.Println("download:tar:copy:", err)
				continue
			}
			continue
		}
	}
	return
}

func handshake(conn net.Conn) (count uint32, files []FileEntry, err error) {
	if err = binary.Read(conn, binary.BigEndian, &count); err != nil {
		log.Println("handshake:count:", err)
		return 0, nil, err
	}
	files = make([]FileEntry, count)
	if err = json.NewDecoder(conn).Decode(&files); err != nil {
		log.Println("handshake:json:", err)
		return 0, nil, err
	}
	return count, files, nil
}

func handle(conn net.Conn, path string, checksum bool) {
	defer conn.Close()
	log.Println("handle:", path, conn.LocalAddr(), conn.RemoteAddr())

	// optimization while waiting
	log.Println("handle:", "Generating file list with checksum:", checksum)
	localFiles := listFiles(path, checksum)
	log.Println("handle:", "File list generated:", len(localFiles), "files found.")

	remoteCount, clientFiles, err := handshake(conn)
	if err != nil {
		log.Println("handle:handshake:", err)
		return
	}
	log.Println("handle:", "Received info from client:", remoteCount, "/", len(clientFiles))

	diffFiles := discardFiles(localFiles, clientFiles)
	log.Println("handle:", "Diff created:", len(diffFiles))

	if len(diffFiles) != int(math.Abs(float64(uint32(len(localFiles)) - remoteCount))) {
		log.Println("handle:diff:(diff/local/remote):", len(diffFiles), len(localFiles), remoteCount)
	}

	//if err := json.NewEncoder(conn).Encode(diffFiles); err != nil {
	//	log.Println("handle:json:encode:", err)
	//	return
	//}
	log.Println("handle:", "Sending archive...")
	archiveFiles(diffFiles, conn, path)
	return
}

func serve(listener net.Listener, path string, checksum bool) {
	log.Println("serve:listening:", path, listener, listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println("serve:accept:", err)
			return
		}
		go handle(conn, path, checksum)
		continue
	}
	return
}

func download(conn net.Conn, path string, checksum bool) {
	defer conn.Close()
	if err := os.MkdirAll(path, 0755); err != nil {
		log.Println("download:mkdir:", err)
		return
	}

	log.Println("download:", "Generating file list with checksum:", checksum)

	filesLocal := listFiles(path, checksum)

	log.Println("download:", "File list generated:", len(filesLocal), "files found.")

	if err := binary.Write(conn, binary.BigEndian, uint32(len(filesLocal))); err != nil {
		log.Println("download:count:", err)
		return
	}

	log.Println("download:", "Sending file list...")
	if err := json.NewEncoder(conn).Encode(filesLocal); err != nil {
		log.Println("download:json:", err)
		return
	}
	log.Println("download:", "Receiving files...")
	unarchiveFiles(conn, path)
	log.Println("download:", "Action completed.")
	return
}

func main() {
	listen   := flag.String("listen",  "",  "Listen/Bind address:port")
	connect  := flag.String("connect", "",  "Server address:port")
	path     := flag.String("path",    "",  "Path to use for files")
	checksum := flag.Bool("checksum", true, "Enable/disable checksums (default: true)")
	flag.Parse()
	if *path == "" {
		*path = "./"
	}
	if *connect != "" {
		log.Println("main:connect:", *connect)
		conn, err := net.Dial("tcp", *connect)
		if err != nil {
			log.Println("main:connect:err:", err)
			return
		}
		download(conn, *path, *checksum)
	}
	if *listen != "" {
		log.Println("main:listen:", *listen)
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			log.Println("main:listen:err:", err)
			return
		}
		serve(listener, *path, *checksum)
	}
	return
}
