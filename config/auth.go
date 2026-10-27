package config

import (
	"dtm/libs/textvalidate"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Auth struct {
	E2EAuthEnabled                                 bool
	E2EAuthEmail, E2EAuthPassword                  string
	ProjectID                                      string
	FrontendURL, RedirectPath, RedirectURL, APIKey string
}

func LoadDevEnv(dev bool) error {
	if !dev {
		return nil
	}
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return errors.New("cannot load .env")
	}
	return nil
}

// FrontendURL uses DEFAULT_FRONTEND_URL when the environment value is empty or unset.
func FrontendURL() string {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = DEFAULT_FRONTEND_URL
	}
	return strings.TrimRight(frontendURL, "/")
}

func LoadAuth(dev bool, port string) (Auth, error) {
	c := Auth{ProjectID: os.Getenv("GCIP_PROJECT_ID"), FrontendURL: FrontendURL(), RedirectPath: os.Getenv("OAUTH_REDIRECT_PATH"), APIKey: os.Getenv("GCIP_API_KEY")}
	if !textvalidate.ValidOrigin(c.FrontendURL, dev) {
		return c, errors.New("invalid FRONTEND_URL")
	}
	if textvalidate.ValidateNonBlank(c.ProjectID) != nil {
		return c, errors.New("GCIP_PROJECT_ID must be set")
	}
	if textvalidate.ValidateNonBlank(c.APIKey) != nil {
		return c, errors.New("GCIP_API_KEY must be set")
	}
	if !textvalidate.ValidRedirectPath(c.RedirectPath) {
		return c, errors.New("invalid OAUTH_REDIRECT_PATH")
	}
	// GCIP requires a full URL; the cookie uses the same public path.
	c.RedirectURL = c.FrontendURL + c.RedirectPath
	if raw := os.Getenv("E2E_AUTH_ENABLED"); raw != "" {
		var err error
		c.E2EAuthEnabled, err = strconv.ParseBool(raw)
		if err != nil {
			return c, errors.New("invalid E2E_AUTH_ENABLED")
		}
	}
	if c.E2EAuthEnabled {
		c.E2EAuthEmail = strings.TrimSpace(os.Getenv("E2E_AUTH_EMAIL"))
		c.E2EAuthPassword = os.Getenv("E2E_AUTH_PASSWORD")
		if textvalidate.ValidateNonBlank(c.E2EAuthEmail) != nil || textvalidate.ValidateNonBlank(c.E2EAuthPassword) != nil {
			return c, errors.New("E2E_AUTH_EMAIL and E2E_AUTH_PASSWORD must be set when E2E_AUTH_ENABLED is true")
		}
	}
	return c, nil
}
