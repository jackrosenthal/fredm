package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/msteinert/pam/v2"
)

// authenticate runs PAM authentication and account checks for user. On
// success it returns the open transaction, which the caller must End.
func authenticate(service, user, password string) (*pam.Transaction, error) {
	var (
		messages     []string
		passwordUsed bool
	)
	tx, err := pam.StartFunc(service, user, func(style pam.Style, msg string) (string, error) {
		switch style {
		case pam.PromptEchoOn:
			return user, nil
		case pam.PromptEchoOff:
			// Only the password can be answered without asking the
			// user; a second secret prompt (e.g. a new password) fails.
			if passwordUsed {
				return "", fmt.Errorf("unsupported prompt %q", msg)
			}
			passwordUsed = true
			return password, nil
		case pam.ErrorMsg, pam.TextInfo:
			messages = append(messages, strings.TrimSpace(msg))
			return "", nil
		default:
			return "", fmt.Errorf("unsupported PAM message style %d", style)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("pam start: %w", err)
	}

	fail := func(err error) (*pam.Transaction, error) {
		_ = tx.End()
		if len(messages) > 0 {
			return nil, errors.New(messages[len(messages)-1])
		}
		return nil, err
	}

	if err := tx.Authenticate(pam.DisallowNullAuthtok); err != nil {
		return fail(err)
	}
	if err := tx.AcctMgmt(pam.DisallowNullAuthtok); err != nil {
		if errors.Is(err, pam.ErrNewAuthtokReqd) {
			return fail(errors.New("password expired, change it with passwd"))
		}
		return fail(err)
	}
	return tx, nil
}
