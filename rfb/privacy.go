package rfb

import (
	"context"
	"encoding/binary"
	"errors"
	"image"
	"time"
)

var (
	ErrPrivacyUnsupported = errors.New("rfb: server does not support verified screen privacy")
	ErrPrivacyUnconfirmed = errors.New("rfb: server did not confirm screen privacy")
	ErrPrivacyLost        = errors.New("rfb: screen privacy was lost")
)

// DisplayInfo2 body (without its length prefix). The geometry uses network
// byte order; Apple servers have reported the session bits in either order.
func applePrivacyFlags(body []byte) (available, awayFromConsole, ok bool) {
	if len(body) < 18 {
		return false, false, false
	}
	version := binary.BigEndian.Uint16(body)
	if version < 4 || version > 16 {
		return false, false, false
	}
	big, little := body[17]&0x1e, body[14]&0x1e
	if big != 0 && little != 0 && big != little {
		return false, false, false
	}
	flags := big | little
	return flags&0x02 != 0, flags&0x1c == 0, true
}

func (c *Client) observePrivacyLayout(body []byte) error {
	if !c.opt.RequireScreenPrivacy {
		return nil
	}
	available, hidden, valid := applePrivacyFlags(body)
	if c.privacyReady && !(valid && available && hidden) {
		return ErrPrivacyLost
	}
	if valid {
		c.privacyObserved = true
		c.privacyAvailable = available
		c.privacyHidden = available && hidden
	}
	return nil
}

// confirmPrivacy runs before the viewer is published and before any input can
// be sent. The normal decoder consumes early updates without exposing them.
func (c *Client) confirmPrivacy(ctx context.Context) error {
	const confirmTimeout = 8 * time.Second
	deadline := time.Now().Add(confirmTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return err
	}
	h := c.h
	c.h = quietPrivacyHandler{}
	defer func() { c.h = h }()
	asked := false
	for {
		if err := c.readMsg(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ErrPrivacyUnconfirmed
		}
		if !c.privacyObserved {
			continue
		}
		if !c.privacyAvailable {
			return ErrPrivacyUnsupported
		}
		if c.privacyHidden {
			if err := c.conn.SetDeadline(time.Time{}); err != nil {
				return err
			}
			c.fb = newFramebuffer(c.width, c.height) // discard pixels captured before confirmation
			if err := c.write(framebufferUpdateRequest(false, c.width, c.height)); err != nil {
				return ErrPrivacyUnconfirmed
			}
			c.privacyReady = true
			return nil
		}
		if !asked {
			if err := c.Curtain(false, ""); err != nil {
				return ErrPrivacyUnconfirmed
			}
			asked = true
		}
	}
}

// ScreenPrivacyRequired reports the policy captured at connection time.
func (c *Client) ScreenPrivacyRequired() bool { return c.opt.RequireScreenPrivacy }

type quietPrivacyHandler struct{}

func (quietPrivacyHandler) Resize(int, int)                 {}
func (quietPrivacyHandler) Updated(image.Rectangle)         {}
func (quietPrivacyHandler) UpdateDone()                     {}
func (quietPrivacyHandler) Cursor(*image.RGBA, image.Point) {}
func (quietPrivacyHandler) Bell()                           {}
func (quietPrivacyHandler) Clipboard(string)                {}
func (quietPrivacyHandler) Name(string)                     {}
