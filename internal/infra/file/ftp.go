package file

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// ftpClient 按主配置连接 FTP。路径是 basePath 加对象路径，访问地址仍走本服务的下载接口。
// 数据连接使用 PASV。主动模式要求服务器回连客户端，容器里通常不可达，这里不走 PORT。
type ftpClient struct {
	id       int64
	domain   string
	host     string
	port     int
	username string
	password string
	basePath string
}

func newFTP(id int64, cfg ClientConfig) (ObjectClient, error) {
	if cfg.Host == "" || cfg.Username == "" || cfg.Port == 0 {
		return nil, &Error{Code: 400, Msg: "FTP 存储器需要 host、port 和 username"}
	}
	return &ftpClient{
		id: id, domain: cfg.Domain, host: cfg.Host, port: cfg.Port,
		username: cfg.Username, password: cfg.Password, basePath: cfg.BasePath,
	}, nil
}

func (c *ftpClient) Upload(ctx context.Context, content []byte, objectPath, _ string) (string, error) {
	remote, err := c.remote(objectPath)
	if err != nil {
		return "", err
	}
	err = c.with(ctx, func(conn *ftpSession) error {
		if err := conn.mkdirs(parentDir(remote)); err != nil {
			return err
		}
		return conn.stor(remote, content)
	})
	if err != nil {
		return "", err
	}
	return downloadURL(c.domain, c.id, objectPath), nil
}

func (c *ftpClient) Delete(ctx context.Context, objectPath string) error {
	remote, err := c.remote(objectPath)
	if err != nil {
		return err
	}
	return c.with(ctx, func(conn *ftpSession) error {
		code, msg, err := conn.cmd("DELE %s", remote)
		if err != nil {
			return err
		}
		if code != 250 && code != 200 {
			return fmt.Errorf("删除 FTP 文件失败：%s", strings.TrimSpace(msg))
		}
		return nil
	})
}

func (c *ftpClient) Get(ctx context.Context, objectPath string) ([]byte, error) {
	remote, err := c.remote(objectPath)
	if err != nil {
		return nil, err
	}
	var content []byte
	err = c.with(ctx, func(conn *ftpSession) error {
		got, err := conn.retr(remote)
		content = got
		return err
	})
	return content, err
}

func (c *ftpClient) PresignPut(context.Context, string) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *ftpClient) PresignGet(context.Context, string, int) (string, error) {
	return "", &Error{Code: 400, Msg: "当前存储器不支持预签名"}
}

func (c *ftpClient) remote(objectPath string) (string, error) {
	if !validRelative(objectPath) {
		return "", &Error{Code: 1_001_003_003, Msg: "文件路径不正确"}
	}
	base := strings.Trim(c.basePath, "/")
	if base == "" {
		return objectPath, nil
	}
	return base + "/" + objectPath, nil
}

func (c *ftpClient) with(ctx context.Context, fn func(*ftpSession) error) error {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.host, strconv.Itoa(c.port)))
	if err != nil {
		return err
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	session := &ftpSession{conn: raw, reader: bufio.NewReader(raw)}
	if _, _, err := session.readReply(); err != nil {
		return err
	}
	if err := session.expect(331, "USER %s", c.username); err != nil {
		return err
	}
	if err := session.expect(230, "PASS %s", c.password); err != nil {
		return err
	}
	_ = session.expect(200, "TYPE I")
	return fn(session)
}

type ftpSession struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (s *ftpSession) expect(want int, format string, args ...any) error {
	code, msg, err := s.cmd(format, args...)
	if err != nil {
		return err
	}
	if code != want {
		return fmt.Errorf("FTP 响应 %d，期望 %d：%s", code, want, strings.TrimSpace(msg))
	}
	return nil
}

func (s *ftpSession) cmd(format string, args ...any) (int, string, error) {
	line := fmt.Sprintf(format, args...) + "\r\n"
	if _, err := s.conn.Write([]byte(line)); err != nil {
		return 0, "", err
	}
	return s.readReply()
}

func (s *ftpSession) readReply() (int, string, error) {
	line, err := s.reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	if len(line) < 3 {
		return 0, line, fmt.Errorf("FTP 响应过短")
	}
	code, err := strconv.Atoi(line[:3])
	if err != nil {
		return 0, line, err
	}
	if len(line) > 3 && line[3] == '-' {
		for {
			next, err := s.reader.ReadString('\n')
			if err != nil {
				return code, line, err
			}
			line += next
			if len(next) >= 4 && next[:3] == line[:3] && next[3] == ' ' {
				break
			}
		}
	}
	return code, line, nil
}

func (s *ftpSession) mkdirs(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	var current string
	for _, part := range strings.Split(dir, "/") {
		if part == "" {
			continue
		}
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		code, _, err := s.cmd("MKD %s", current)
		if err != nil {
			return err
		}
		// 257 是创建成功，550 多半是目录已经存在。
		if code != 257 && code != 550 {
			return fmt.Errorf("创建 FTP 目录失败：%d", code)
		}
	}
	return nil
}

func (s *ftpSession) stor(remote string, content []byte) error {
	data, err := s.passive()
	if err != nil {
		return err
	}
	defer data.Close()
	code, msg, err := s.cmd("STOR %s", remote)
	if err != nil {
		return err
	}
	if code != 150 && code != 125 {
		return fmt.Errorf("上传 FTP 文件失败：%s", strings.TrimSpace(msg))
	}
	if _, err := data.Write(content); err != nil {
		return err
	}
	if err := data.Close(); err != nil {
		return err
	}
	code, msg, err = s.readReply()
	if err != nil {
		return err
	}
	if code != 226 && code != 250 {
		return fmt.Errorf("上传 FTP 文件没有完成：%s", strings.TrimSpace(msg))
	}
	return nil
}

func (s *ftpSession) retr(remote string) ([]byte, error) {
	data, err := s.passive()
	if err != nil {
		return nil, err
	}
	defer data.Close()
	code, msg, err := s.cmd("RETR %s", remote)
	if err != nil {
		return nil, err
	}
	if code == 550 {
		return nil, nil
	}
	if code != 150 && code != 125 {
		return nil, fmt.Errorf("读取 FTP 文件失败：%s", strings.TrimSpace(msg))
	}
	content, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	_ = data.Close()
	if _, _, err := s.readReply(); err != nil {
		return nil, err
	}
	return content, nil
}

func (s *ftpSession) passive() (net.Conn, error) {
	code, msg, err := s.cmd("PASV")
	if err != nil {
		return nil, err
	}
	if code != 227 {
		return nil, fmt.Errorf("FTP 没有进入被动模式：%s", strings.TrimSpace(msg))
	}
	host, port, err := parsePASV(msg)
	if err != nil {
		return nil, err
	}
	return net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 3*time.Second)
}

func parsePASV(msg string) (string, int, error) {
	start := strings.LastIndex(msg, "(")
	end := strings.LastIndex(msg, ")")
	if start < 0 || end <= start {
		return "", 0, fmt.Errorf("无法解析 PASV：%s", strings.TrimSpace(msg))
	}
	parts := strings.Split(msg[start+1:end], ",")
	if len(parts) != 6 {
		return "", 0, fmt.Errorf("无法解析 PASV：%s", strings.TrimSpace(msg))
	}
	p1, err1 := strconv.Atoi(strings.TrimSpace(parts[4]))
	p2, err2 := strconv.Atoi(strings.TrimSpace(parts[5]))
	if err1 != nil || err2 != nil {
		return "", 0, fmt.Errorf("无法解析 PASV 端口")
	}
	host := strings.Join(parts[:4], ".")
	return host, p1*256 + p2, nil
}

func parentDir(remote string) string {
	i := strings.LastIndex(remote, "/")
	if i <= 0 {
		return ""
	}
	return remote[:i]
}
