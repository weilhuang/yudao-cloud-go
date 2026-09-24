package file

import (
	"bytes"
	"context"
	"io"
	"net"
	"path"
	"strconv"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpClient 按主配置用账号密码连接 SFTP。
// Java 的 JSch 默认也不校验证书指纹，这里同样接受服务器主机密钥，避免换后端后已有配置连不上。
type sftpClient struct {
	id       int64
	domain   string
	host     string
	port     int
	username string
	password string
	basePath string
}

func newSFTP(id int64, cfg ClientConfig) (ObjectClient, error) {
	if cfg.Host == "" || cfg.Username == "" {
		return nil, &Error{Code: 400, Msg: "SFTP 存储器需要 host 和 username"}
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	return &sftpClient{
		id: id, domain: cfg.Domain, host: cfg.Host, port: port,
		username: cfg.Username, password: cfg.Password, basePath: cfg.BasePath,
	}, nil
}

func (c *sftpClient) Upload(ctx context.Context, content []byte, objectPath, _ string) (string, error) {
	remote, err := c.remote(objectPath)
	if err != nil {
		return "", err
	}
	err = c.with(ctx, func(client *sftp.Client) error {
		if dir := path.Dir(remote); dir != "." && dir != "/" {
			if err := client.MkdirAll(dir); err != nil {
				return err
			}
		}
		file, err := client.Create(remote)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.Write(content)
		return err
	})
	if err != nil {
		return "", err
	}
	return downloadURL(c.domain, c.id, objectPath), nil
}

func (c *sftpClient) Delete(ctx context.Context, objectPath string) error {
	remote, err := c.remote(objectPath)
	if err != nil {
		return err
	}
	return c.with(ctx, func(client *sftp.Client) error {
		err := client.Remove(remote)
		if err == nil || isNotExist(err) {
			return nil
		}
		return err
	})
}

func (c *sftpClient) Get(ctx context.Context, objectPath string) ([]byte, error) {
	remote, err := c.remote(objectPath)
	if err != nil {
		return nil, err
	}
	var content []byte
	err = c.with(ctx, func(client *sftp.Client) error {
		file, err := client.Open(remote)
		if err != nil {
			if isNotExist(err) {
				return nil
			}
			return err
		}
		defer file.Close()
		content, err = io.ReadAll(file)
		return err
	})
	return content, err
}

func (c *sftpClient) PresignPut(context.Context, string) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *sftpClient) PresignGet(context.Context, string, int) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *sftpClient) remote(objectPath string) (string, error) {
	if !validRelative(objectPath) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	base := path.Clean("/" + stringsTrim(c.basePath))
	return path.Clean(base + "/" + objectPath), nil
}

func (c *sftpClient) with(ctx context.Context, fn func(*sftp.Client) error) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(raw, addr, &ssh.ClientConfig{
		User:            c.username,
		Auth:            []ssh.AuthMethod{ssh.Password(c.password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         3 * time.Second,
	})
	if err != nil {
		return err
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()
	session, err := sftp.NewClient(client)
	if err != nil {
		return err
	}
	defer session.Close()
	return fn(session)
}

func isNotExist(err error) bool {
	return err != nil && (sftp.ErrSSHFxNoSuchFile == unwrap(err) || bytes.Contains([]byte(err.Error()), []byte("no such file")))
}

func unwrap(err error) error {
	type unwrapper interface{ Unwrap() error }
	for {
		u, ok := err.(unwrapper)
		if !ok || u.Unwrap() == nil {
			return err
		}
		err = u.Unwrap()
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == '/' || s[0] == '\\') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '/' || s[len(s)-1] == '\\') {
		s = s[:len(s)-1]
	}
	return s
}
