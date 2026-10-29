package rest

import (
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/support/apperror"
	"github.com/dennis-dko/go-time-recording/internal/support/qrcode"
)

// AddressResponse is this installation's address, and the same as a code.
type AddressResponse struct {
	Address string  `json:"address"`
	QR      *string `json:"qr,omitempty"`
}

// Address handles GET /api/v1/me/address: this installation's address with a
// QR code of it, which the card offering to install the page shows, for opening
// the installation on a phone without typing its address.
//
// The page names its own address as ?origin=, because a browser sends no Origin
// with a GET a page makes of its own installation, and the address the page
// shows is the one to encode. It is taken where it names the host the request
// reached; anything else gets the address as reached instead.
func (h *AuthHandler) Address(c *gofr.Context) (any, error) {
	if _, err := h.authz.Principal(c); err != nil {
		return nil, err
	}

	request := requestOf(c)
	if request == nil {
		return nil, toHTTPError(apperror.Internal(errors.New("the request behind this context is not there")))
	}

	answer := AddressResponse{Address: addressNamed(request, c.Param("origin"))}

	// Not a secret and not essential: without a code, the card simply has none.
	if code, err := qrcode.SVGDataURI(answer.Address); err == nil {
		answer.QR = &code
	} else {
		c.Logger.Errorf("could not render the address's QR code: %v", err)
	}

	return answer, nil
}
