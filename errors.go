package main

import (
	"fmt"
	"io"
)

type SMTPError struct {
	BasicStatus    int
	EnhancedStatus string
	Message        string
	Underlying     error
}

func (e *SMTPError) Error() string {
	return fmt.Sprintf("SMTPError(%d %s %s): %v", e.BasicStatus, e.EnhancedStatus, e.Message, e.Underlying)
}

func (e *SMTPError) Unwrap() error {
	return e.Underlying
}

func (e *SMTPError) Write(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%d %s %s\r\n", e.BasicStatus, e.EnhancedStatus, e.Message)
	return err
}
