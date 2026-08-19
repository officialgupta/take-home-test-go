package providers

import (
	"errors"
	"math/rand/v2"
	"time"
)

var ErrSendFailed = errors.New("email send failed")

type EmailParams struct {
	To      string
	From    string
	Subject string
	Body    string
}

// SendEmail mirrors reference-ts/src/providers/sendgrid.ts
func SendEmail(params EmailParams) error {
	success := rand.Float64() < 0.95

	time.Sleep(time.Second)

	if !success {
		return ErrSendFailed
	}
	return nil
}
