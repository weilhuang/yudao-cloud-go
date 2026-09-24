package message

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// smtpSend 用账号上的 host、端口和 SSL/STARTTLS 发信。
func smtpSend(account MailAccount, to []string, subject, body string) error {
	if account.Host == "" || account.Port == 0 {
		return fmt.Errorf("邮箱服务器地址不能为空")
	}
	addr := fmt.Sprintf("%s:%d", account.Host, account.Port)
	msg := []byte("From: " + account.Mail + "\r\nTo: " + strings.Join(to, ",") + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + body)
	auth := smtp.PlainAuth("", account.Username, account.Password, account.Host)
	if account.SSLEnable {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: account.Host})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, account.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return err
		}
		return deliver(client, account.Mail, to, msg)
	}
	if account.StartTLSEnable {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, account.Host)
		if err != nil {
			return err
		}
		defer client.Close()
		if err := client.StartTLS(&tls.Config{ServerName: account.Host}); err != nil {
			return err
		}
		if account.Username != "" {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		return deliver(client, account.Mail, to, msg)
	}
	return smtp.SendMail(addr, auth, account.Mail, to, msg)
}

func deliver(client *smtp.Client, from string, to []string, msg []byte) error {
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		return err
	}
	return writer.Close()
}
