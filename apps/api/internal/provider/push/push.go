// Package push defines push notifications as a small interface. Push is post-MVP.
//
// No vendor is chosen: push is OPEN (docs/DECISIONS.md D-OPEN-8). This package ships only the "none" driver.
// A real adapter goes in its own sub-package (e.g. provider/push/<vendor>) only after D-OPEN-8 is decided.
package push

import (
	"context"
	"errors"
	"fmt"
)

// ErrDisabled means push is turned off.
var ErrDisabled = errors.New("push: disabled")

// Sender delivers one notification to one device.
type Sender interface {
	Send(ctx context.Context, deviceToken string, title, body string) error
}

// DriverNone is the only driver until D-OPEN-8 is decided.
const DriverNone = "none"

// New returns the sender for driver.
func New(driver string) (Sender, error) {
	switch driver {
	case DriverNone, "":
		return None{}, nil
	default:
		return nil, fmt.Errorf("push: unknown driver %q", driver)
	}
}

// None is the disabled driver: every Send returns ErrDisabled.
type None struct{}

func (None) Send(context.Context, string, string, string) error { return ErrDisabled }
