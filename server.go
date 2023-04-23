package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Server struct {
	cfg    *Config
	logger *zap.SugaredLogger
}

func (s *Server) handleSession(conn *SMTPConn) error {
	if err := conn.Ready(s.cfg.AdvertiseName); err != nil {
		return err
	}

	if err := conn.WaitEHLO(); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(conn, "250-Hello %s\r\n", conn.EHLODomain); err != nil {
		return err
	}

	for _, cap := range capabilities {
		if _, err := fmt.Fprintf(conn, "%s\r\n", cap); err != nil {
			return err
		}
	}

	if err := conn.WaitSTARTTLS(); err != nil {
		return err
	}

	fmt.Fprintf(conn, "220 2.0.0 Ready to start TLS\r\n")

	var serverName string

	tlsConfig := &tls.Config{
		GetConfigForClient: func(clientHello *tls.ClientHelloInfo) (*tls.Config, error) {
			serverName = clientHello.ServerName
			return nil, errors.New("not implemented")
		},
	}

	captureConn := NewCaptureConn(conn.Conn)

	err := tls.Server(captureConn, tlsConfig).Handshake()

	if serverName == "" {
		return &SMTPError{
			BasicStatus:    501,
			EnhancedStatus: "5.5.4",
			Message:        "No server name provided",
			Underlying:     err,
		}
	}

	conn.l.Infof("TLS handshake complete, server name: %s", serverName)

	for _, backend := range s.cfg.Backends {
		if backend.Hostname == serverName {
			backendConn, err := net.Dial(backend.Protocol, backend.Address)
			if err != nil {
				return &SMTPError{
					BasicStatus:    451,
					EnhancedStatus: "4.3.0",
					Message:        "Backend unavailable",
					Underlying:     err,
				}
			}

			eg1 := errgroup.Group{}

			eg1.Go(func() error {
				buf := bufio.NewReader(backendConn)
				for {
					line, err := buf.ReadBytes('\n')
					if err != nil {
						return err
					}

					// Wait for "Ready for start TLS"
					if bytes.HasPrefix(line, []byte("220 2.0.0")) {
						conn.l.Infof("Backend ready for start TLS")
						break
					}
				}

				return nil
			})

			fmt.Fprintf(backendConn, "EHLO %s\r\n", conn.EHLODomain)
			fmt.Fprintf(backendConn, "STARTTLS\r\n")

			// Wait for "Ready for start TLS"
			eg1.Wait()

			// Replay the TLS client hello
			_, err = backendConn.Write(captureConn.teeBuffer.Bytes())
			if err != nil {
				return &SMTPError{
					BasicStatus:    451,
					EnhancedStatus: "4.3.0",
					Message:        "Backend unavailable",
					Underlying:     err,
				}
			}

			eg := errgroup.Group{}

			eg.Go(func() error {
				defer backendConn.Close()
				defer conn.Conn.Close()
				_, err := io.Copy(conn.Conn, backendConn)
				return err
			})

			eg.Go(func() error {
				defer backendConn.Close()
				defer conn.Conn.Close()
				_, err := io.Copy(backendConn, conn.Conn)
				return err
			})

			return eg.Wait()
		}
	}

	return &SMTPError{
		BasicStatus:    501,
		EnhancedStatus: "5.5.4",
		Message:        fmt.Sprintf("No backend found for server name %q", serverName),
		Underlying:     errors.Errorf("no backend found for server name %q", serverName),
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	c := &SMTPConn{
		Conn: conn,
		rd:   bufio.NewReader(conn),
		l:    s.logger.Named(conn.RemoteAddr().String()),
	}

	err := s.handleSession(c)

	var smtpError *SMTPError
	if errors.As(err, &smtpError) {
		c.l.Errorf("SMTP error: %+v", err)
		smtpError.Write(c)
		return
	} else if err != nil {
		c.l.Errorf("unhandled error: %+v", err)

		var opError *net.OpError
		if !errors.As(err, &opError) && !errors.Is(err, io.EOF) {
			// Connection is likely still alive, so send a 421
			smtpError := &SMTPError{
				BasicStatus:    451,
				EnhancedStatus: "4.3.0",
				Message:        "Service Unavailable",
				Underlying:     err,
			}
			smtpError.Write(c)
			return
		}

		return
	}
}
