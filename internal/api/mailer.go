package api

import (
	"fmt"
	"net/smtp"
	"strings"
)

type smtpMailer struct {
	host     string
	port     string
	username string
	password string
	fromName string
	fromAddr string
}

func NewSMTPMailer(host, port, username, password, fromName, fromAddr string) *smtpMailer {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	fromAddr = strings.TrimSpace(fromAddr)
	fromName = strings.TrimSpace(fromName)

	if host == "" || port == "" || username == "" || password == "" || fromAddr == "" {
		return nil
	}
	return &smtpMailer{
		host:     host,
		port:     port,
		username: username,
		password: password,
		fromName: fromName,
		fromAddr: fromAddr,
	}
}

func (m *smtpMailer) send(toEmail, subject, plainBody string) error {
	if m == nil {
		return nil
	}
	toEmail = strings.TrimSpace(toEmail)
	if toEmail == "" {
		return fmt.Errorf("missing recipient")
	}

	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	addr := m.host + ":" + m.port
	fromHeader := m.fromAddr
	if m.fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", m.fromName, m.fromAddr)
	}

	msg := strings.Join([]string{
		fmt.Sprintf("From: %s", fromHeader),
		fmt.Sprintf("To: %s", toEmail),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		plainBody,
	}, "\r\n")

	return smtp.SendMail(addr, auth, m.fromAddr, []string{toEmail}, []byte(msg))
}
