package rest

import (
	"runtime"

	"gofr.dev/pkg/gofr"

	"github.com/dennis-dko/go-time-recording/internal/domain/model"
)

// BrandingResponse is the instance labelling.
type BrandingResponse struct {
	Title string `json:"title"`

	// TabTitle names the browser tab where an installation wants it to say
	// something shorter than the header does. Empty means the title.
	TabTitle string `json:"tabTitle"`

	Banner string `json:"banner"`
	Logo   string `json:"logo"`

	// LogoHeader and LogoBanner are the logo at the size those two places draw
	// it, derived when it was saved.
	//
	// Sent instead of the original, which is a wordmark of a few hundred
	// kilobytes and went to every visitor of the sign-in screen inside this
	// answer. These are a few kilobytes each. Empty on an installation whose logo
	// predates them, where the interface falls back to the original.
	LogoHeader  string `json:"logoHeader,omitempty"`
	LogoBanner  string `json:"logoBanner,omitempty"`
	FooterText  string `json:"footerText"`
	CompanyName string `json:"companyName"`
	CompanyURL  string `json:"companyUrl"`
	LegalNotice string `json:"legalNotice"`

	// Translations carries the same four texts per language, for the interface to
	// pick from. Sent whole rather than resolved here: which language a reader
	// wants is decided in the browser - the switcher first, the browser's own
	// setting otherwise - and this endpoint answers before anyone has signed in.
	Translations map[string]BrandingTextResponse `json:"translations,omitempty"`

	// Crops carry which part of the logo each place uses, so the screen that
	// chose them can show them again. Fractions of the whole image; a place that
	// uses all of it is not listed, which is what most installations send.
	Crops map[string]CropResponse `json:"crops,omitempty"`
}

// CropResponse is a part of the logo, as fractions of the whole.
type CropResponse struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// BrandingTextResponse is one language's version of the texts.
type BrandingTextResponse struct {
	Title       string `json:"title"`
	TabTitle    string `json:"tabTitle"`
	Banner      string `json:"banner"`
	FooterText  string `json:"footerText"`
	LegalNotice string `json:"legalNotice"`
}

// InstanceResponse is the branding plus the build serving it.
//
// Embedded rather than nested, so the JSON stays the flat object the interface
// already reads. It is the GET shape only: SaveBranding still binds
// BrandingResponse, which is what keeps a PUT from carrying a version field
// that nothing could act on.
type InstanceResponse struct {
	BrandingResponse

	// Version is the build this process was compiled from - a tag for a
	// release, "dev" for a binary built without -ldflags. Public, like the rest
	// of the branding: it is in the footer of a page anyone can reach, and a
	// version number is not what keeps an installation safe.
	Version string `json:"version"`

	// OS is what this build runs on, shown beside the version as "v1.0 (windows)".
	//
	// The same version is published for four platforms and they do not all behave
	// alike - restarting from the interface works on Linux and cannot on Windows -
	// so "which version" is only half of what a support conversation needs. Public
	// for the same reason the version is: it is on a page anyone can reach, and an
	// installation that depends on nobody knowing its platform was not safe anyway.
	OS string `json:"os"`
}

// Branding handles GET /api/v1/branding.
//
// It is readable by anyone, signed in or not: the sign-in screen has to show
// the instance's own title and logo before there is a session.
func (h *SettingsHandler) Branding(c *gofr.Context) (any, error) {
	branding, err := h.settings.Branding(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	return InstanceResponse{
		BrandingResponse: newBrandingResponse(branding),
		Version:          h.version,
		OS:               runtime.GOOS,
	}, nil
}

// SaveBranding handles PUT /api/v1/settings/branding.
func (h *SettingsHandler) SaveBranding(c *gofr.Context) (any, error) {
	if err := h.requireAdmin(c); err != nil {
		return nil, err
	}

	var req BrandingResponse
	if err := bind(c, &req); err != nil {
		return nil, toHTTPError(err)
	}

	err := h.settings.SaveBranding(c, model.Branding{
		Title:       req.Title,
		TabTitle:    req.TabTitle,
		Banner:      req.Banner,
		LogoDataURI: req.Logo,
		FooterText:  req.FooterText,
		CompanyName: req.CompanyName,
		CompanyURL:  req.CompanyURL,
		LegalNotice: req.LegalNotice,
		HeaderCrop:  cropFrom(req.Crops["header"]),
		BannerCrop:  cropFrom(req.Crops["banner"]),
		IconCrop:    cropFrom(req.Crops["icon"]),
		Translations: func() map[string]model.BrandingText {
			if len(req.Translations) == 0 {
				return nil
			}

			out := make(map[string]model.BrandingText, len(req.Translations))

			for language, text := range req.Translations {
				// Only languages the interface has words for. A translation for a
				// language nothing can select is a row nobody will ever read, and
				// this is the one place a caller names the key.
				if !model.IsSupportedLanguage(language) {
					continue
				}

				out[language] = model.BrandingText{
					Title:       text.Title,
					TabTitle:    text.TabTitle,
					Banner:      text.Banner,
					FooterText:  text.FooterText,
					LegalNotice: text.LegalNotice,
				}
			}

			return out
		}(),
	})
	if err != nil {
		return nil, toHTTPError(err)
	}

	branding, err := h.settings.Branding(c)
	if err != nil {
		return nil, toHTTPError(err)
	}

	// The same shape GET returns, so the interface can render the saved result
	// with the code that renders a fetched one.
	return InstanceResponse{
		BrandingResponse: newBrandingResponse(branding),
		Version:          h.version,
		OS:               runtime.GOOS,
	}, nil
}

func newBrandingResponse(b model.Branding) BrandingResponse {
	return BrandingResponse{
		Title:       b.Title,
		TabTitle:    b.TabTitle,
		Banner:      b.Banner,
		Logo:        b.LogoDataURI,
		LogoHeader:  b.LogoHeader,
		LogoBanner:  b.LogoBanner,
		Crops:       cropsOf(b),
		FooterText:  b.FooterText,
		CompanyName: b.CompanyName,
		CompanyURL:  b.CompanyURL,
		LegalNotice: b.LegalNotice,
		Translations: func() map[string]BrandingTextResponse {
			if len(b.Translations) == 0 {
				return nil
			}

			out := make(map[string]BrandingTextResponse, len(b.Translations))

			for language, text := range b.Translations {
				out[language] = BrandingTextResponse{
					Title:       text.Title,
					TabTitle:    text.TabTitle,
					Banner:      text.Banner,
					FooterText:  text.FooterText,
					LegalNotice: text.LegalNotice,
				}
			}

			return out
		}(),
	}
}

// cropsOf is what the screen needs to show the parts that were chosen.
//
// Named rather than positional, and only the ones that are not the whole image:
// a logo nobody has cropped answers with nothing at all, which is what the great
// majority of installations will send and receive.
func cropsOf(b model.Branding) map[string]CropResponse {
	out := map[string]CropResponse{}

	for name, crop := range map[string]model.LogoCrop{
		"header": b.HeaderCrop,
		"banner": b.BannerCrop,
		"icon":   b.IconCrop,
	} {
		if crop.Whole() {
			continue
		}

		out[name] = CropResponse{X: crop.X, Y: crop.Y, W: crop.W, H: crop.H}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// cropFrom reads one back. An absent one is the whole image, which is both the
// default and the answer for anything that makes no sense - the scaler clamps
// what it is given rather than trusting it.
func cropFrom(c CropResponse) model.LogoCrop {
	return model.LogoCrop{X: c.X, Y: c.Y, W: c.W, H: c.H}
}
