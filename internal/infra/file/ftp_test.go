package file

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestFTPUploadRoundTrip(t *testing.T) {
	addr, stop := startFTP(t)
	defer stop()
	host, portText, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portText)
	client, err := newFTP(8, ClientConfig{
		Domain: "http://127.0.0.1:48080", Host: host, Port: port,
		Username: "foo", Password: "pass", BasePath: "/upload",
	})
	if err != nil {
		t.Fatal(err)
	}
	url, err := client.Upload(context.Background(), []byte("hello-ftp"), "a/b.txt", "text/plain")
	if err != nil || !strings.Contains(url, "/admin-api/infra/file/8/get/a/b.txt") {
		t.Fatal(url, err)
	}
	got, err := client.Get(context.Background(), "a/b.txt")
	if err != nil || string(got) != "hello-ftp" {
		t.Fatal(string(got), err)
	}
	if err := client.Delete(context.Background(), "a/b.txt"); err != nil {
		t.Fatal(err)
	}
	got, err = client.Get(context.Background(), "a/b.txt")
	if err != nil || got != nil {
		t.Fatal(got, err)
	}
}

func startFTP(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	var mu sync.Mutex
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFTP(conn, files, &mu)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func serveFTP(conn net.Conn, files map[string][]byte, mu *sync.Mutex) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	write := func(line string) {
		_, _ = conn.Write([]byte(line + "\r\n"))
	}
	write("220 ready")
	var dataLn net.Listener
	defer func() {
		if dataLn != nil {
			_ = dataLn.Close()
		}
	}()
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		cmd, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(cmd) {
		case "USER":
			write("331 need password")
		case "PASS":
			write("230 logged in")
		case "TYPE":
			write("200 type set")
		case "MKD":
			write("257 created")
		case "PASV":
			if dataLn != nil {
				_ = dataLn.Close()
			}
			dataLn, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				write("425 cannot open data")
				continue
			}
			_, portText, _ := net.SplitHostPort(dataLn.Addr().String())
			port, _ := strconv.Atoi(portText)
			write(fmt.Sprintf("227 Entering Passive Mode (127,0,0,1,%d,%d)", port/256, port%256))
		case "STOR":
			write("150 opening")
			data, err := dataLn.Accept()
			if err != nil {
				write("426 data failed")
				continue
			}
			content, _ := io.ReadAll(data)
			_ = data.Close()
			mu.Lock()
			files[arg] = content
			mu.Unlock()
			write("226 transfer complete")
		case "RETR":
			mu.Lock()
			content, ok := files[arg]
			mu.Unlock()
			if !ok {
				write("550 not found")
				continue
			}
			write("150 opening")
			data, err := dataLn.Accept()
			if err != nil {
				write("426 data failed")
				continue
			}
			_, _ = data.Write(content)
			_ = data.Close()
			write("226 transfer complete")
		case "DELE":
			mu.Lock()
			delete(files, arg)
			mu.Unlock()
			write("250 deleted")
		case "QUIT":
			write("221 bye")
			return
		default:
			write("200 ok")
		}
	}
}
