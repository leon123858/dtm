package config

import (
	"errors"
	"net/url"
	"os"
	"path"
	"regexp"
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
	validURL := func(raw string, origin bool) bool {
		u, e := url.Parse(raw)
		return e == nil && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "https" || (dev && u.Scheme == "http")) && (!origin || u.Path == "")
	}
	if !validURL(c.FrontendURL, true) {
		return c, errors.New("invalid FRONTEND_URL")
	}
	if strings.TrimSpace(c.ProjectID) == "" {
		return c, errors.New("GCIP_PROJECT_ID must be set")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return c, errors.New("GCIP_API_KEY must be set")
	}
	if !validRedirectPath(c.RedirectPath) {
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
		if c.E2EAuthEmail == "" || strings.TrimSpace(c.E2EAuthPassword) == "" {
			return c, errors.New("E2E_AUTH_EMAIL and E2E_AUTH_PASSWORD must be set when E2E_AUTH_ENABLED is true")
		}
	}
	return c, nil
}

var redirectPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+)+$`)

func validRedirectPath(raw string) bool {
	u, err := url.Parse(raw)
	// Restrict to unescaped, cookie-safe path segments; never normalize input.
	return err == nil && u.Scheme == "" && u.Host == "" && u.User == nil &&
		u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		u.EscapedPath() == raw && redirectPathPattern.MatchString(raw) && path.Clean(raw) == raw
}
