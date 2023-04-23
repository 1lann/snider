package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
)

type CaptureConn struct {
	net.Conn
	teeBuffer *bytes.Buffer
	rd        io.Reader
}

func NewCaptureConn(conn net.Conn) *CaptureConn {
	buf := new(bytes.Buffer)
	return &CaptureConn{
		Conn:      conn,
		teeBuffer: buf,
		rd:        io.TeeReader(conn, buf),
	}
}

func (c *CaptureConn) Read(p []byte) (int, error) {
	return c.rd.Read(p)
}

func (c *CaptureConn) Write(p []byte) (int, error) {
	return len(p), nil
}

func (c *CaptureConn) Close() error {
	return nil
}

type SMTPConn struct {
	net.Conn
	rd         *bufio.Reader
	l          *zap.SugaredLogger
	EHLODomain string
}

var capabilities = []string{
	"250-PIPELINING",
	"250-8BITMIME",
	"250-ENHANCEDSTATUSCODES",
	"250-CHUNKING",
	"250-STARTTLS",
	"250-SMTPUTF8",
	"250 SIZE 33554432",
}

func (c *SMTPConn) Read(p []byte) (n int, err error) {
	return c.rd.Read(p)
}

func (c *SMTPConn) Close() error {
	return c.Conn.Close()
}

func (c *SMTPConn) Ready(domain string) error {
	_, err := fmt.Fprintf(c, "220 %s ESMTP server is submissive (and breedable)\r\n", domain)
	return err
}

func (c *SMTPConn) Readlinef(pattern string, args ...any) error {
	line, err := c.rd.ReadBytes('\n')
	if err != nil {
		return err
	}

	trimmedLine := bytes.TrimSuffix(line[:len(line)-1], []byte{'\r'})

	n, err := fmt.Sscanf(string(trimmedLine), pattern, args...)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("unexpected EOF during scanf")
		}

		return err
	}

	if n != len(args) {
		return fmt.Errorf("expected %d args, got %d", len(args), n)
	}

	return nil
}

func (c *SMTPConn) WaitEHLO() error {
	var command, domain string
	if err := c.Readlinef("%s %s", &command, &domain); err != nil {
		return err
	}

	if command != "EHLO" {
		return &SMTPError{
			BasicStatus:    503,
			EnhancedStatus: "5.5.1",
			Message:        "Bad sequence of commands",
			Underlying:     fmt.Errorf("expected EHLO, got %s", command),
		}
	}

	c.EHLODomain = domain

	return nil
}

func (c *SMTPConn) WaitSTARTTLS() error {
	var command string
	if err := c.Readlinef("%s", &command); err != nil {
		return err
	}

	if command != "STARTTLS" {
		return &SMTPError{
			BasicStatus:    503,
			EnhancedStatus: "5.5.1",
			Message:        "STARTTLS is required",
			Underlying:     fmt.Errorf("expected STARTTLS, got %s", command),
		}
	}

	return nil
}
