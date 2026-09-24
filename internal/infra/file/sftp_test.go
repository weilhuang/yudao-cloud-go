package file

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPUploadRoundTrip(t *testing.T) {
	addr, stop := startSFTP(t)
	defer stop()
	host, portText, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portText)
	client, err := newSFTP(9, ClientConfig{
		Domain: "http://127.0.0.1:48080", Host: host, Port: port,
		Username: "foo", Password: "pass", BasePath: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	url, err := client.Upload(context.Background(), []byte("hello-sftp"), "c/d.txt", "text/plain")
	if err != nil || !strings.Contains(url, "/admin-api/infra/file/9/get/c/d.txt") {
		t.Fatal(url, err)
	}
	got, err := client.Get(context.Background(), "c/d.txt")
	if err != nil || string(got) != "hello-sftp" {
		t.Fatal(string(got), err)
	}
	if err := client.Delete(context.Background(), "c/d.txt"); err != nil {
		t.Fatal(err)
	}
}

func startSFTP(t *testing.T) (string, func()) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == "foo" && string(pass) == "pass" {
				return nil, nil
			}
			return nil, ssh.ErrNoAuth
		},
	}
	config.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSFTP(conn, config)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func serveSFTP(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func(in <-chan *ssh.Request) {
			for req := range in {
				ok := false
				if req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp" {
					ok = true
				}
				_ = req.Reply(ok, nil)
			}
		}(requests)
		// 用本机文件系统，才能创建父目录。内存处理器在缺目录时会直接报文件不存在。
		handler, err := sftp.NewServer(channel)
		if err != nil {
			return
		}
		_ = handler.Serve()
		_ = handler.Close()
	}
}
