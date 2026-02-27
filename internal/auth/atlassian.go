package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/markbates/goth"
	"golang.org/x/oauth2"
)

const (
	atlassianAuthURL  = "https://auth.atlassian.com/authorize"
	atlassianTokenURL = "https://auth.atlassian.com/oauth/token" // #nosec G101 -- not a credential, just a URL
	atlassianUserURL  = "https://api.atlassian.com/me"
)

// AtlassianProvider implements goth.Provider for Atlassian OAuth 2.0.
type AtlassianProvider struct {
	ClientKey    string // #nosec G117 -- not serialized, OAuth config field
	Secret       string // #nosec G117 -- not serialized, OAuth config field
	CallbackURL  string
	HTTPClient   *http.Client
	config       *oauth2.Config
	providerName string
}

// NewAtlassianProvider creates a new Atlassian OAuth provider.
func NewAtlassianProvider(clientKey, secret, callbackURL string) *AtlassianProvider {
	p := &AtlassianProvider{
		ClientKey:    clientKey,
		Secret:       secret,
		CallbackURL:  callbackURL,
		providerName: "atlassian",
	}
	p.config = &oauth2.Config{
		ClientID:     clientKey,
		ClientSecret: secret,
		RedirectURL:  callbackURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  atlassianAuthURL,
			TokenURL: atlassianTokenURL,
		},
		Scopes: []string{"read:me", "read:account"},
	}
	return p
}

// Name returns the provider name.
func (p *AtlassianProvider) Name() string {
	return p.providerName
}

// SetName sets the provider name.
func (p *AtlassianProvider) SetName(name string) {
	p.providerName = name
}

// Client returns the HTTP client to use.
func (p *AtlassianProvider) Client() *http.Client {
	return goth.HTTPClientWithFallBack(p.HTTPClient)
}

// BeginAuth starts the OAuth flow.
func (p *AtlassianProvider) BeginAuth(state string) (goth.Session, error) {
	url := p.config.AuthCodeURL(state, oauth2.SetAuthURLParam("audience", "api.atlassian.com"))
	return &AtlassianSession{
		AuthURL: url,
	}, nil
}

// UnmarshalSession unmarshals a session string.
func (p *AtlassianProvider) UnmarshalSession(data string) (goth.Session, error) {
	s := &AtlassianSession{}
	err := json.Unmarshal([]byte(data), s)
	return s, err
}

// FetchUser fetches user data from Atlassian.
func (p *AtlassianProvider) FetchUser(session goth.Session) (user goth.User, err error) {
	s := session.(*AtlassianSession)
	user = goth.User{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		ExpiresAt:    s.ExpiresAt,
		Provider:     p.Name(),
	}

	if user.AccessToken == "" {
		return user, fmt.Errorf("access token not found")
	}

	req, err := http.NewRequest("GET", atlassianUserURL, nil)
	if err != nil {
		return user, err
	}
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.Client().Do(req) // #nosec G704 -- URL is the constant atlassianUserURL
	if err != nil {
		return user, err
	}
	defer func() {
		if cerr := resp.Body.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return user, fmt.Errorf("atlassian API responded with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return user, err
	}

	var atlassianUser struct {
		AccountID string `json:"account_id"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		Picture   string `json:"picture"`
		Nickname  string `json:"nickname"`
	}
	if err := json.Unmarshal(body, &atlassianUser); err != nil {
		return user, err
	}

	user.UserID = atlassianUser.AccountID
	user.Email = atlassianUser.Email
	user.Name = atlassianUser.Name
	user.AvatarURL = atlassianUser.Picture
	user.NickName = atlassianUser.Nickname

	return user, nil
}

// Debug returns debug info.
func (p *AtlassianProvider) Debug(debug bool) {}

// RefreshToken refreshes the OAuth token.
func (p *AtlassianProvider) RefreshToken(refreshToken string) (*oauth2.Token, error) {
	ctx := goth.ContextForClient(p.Client())
	return p.config.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken}).Token()
}

// RefreshTokenAvailable indicates refresh is available.
func (p *AtlassianProvider) RefreshTokenAvailable() bool {
	return true
}

// AtlassianSession stores session data.
type AtlassianSession struct {
	AuthURL      string
	AccessToken  string // #nosec G117 -- required by goth.Session interface
	RefreshToken string // #nosec G117 -- required by goth.Session interface
	ExpiresAt    time.Time
}

// GetAuthURL returns the auth URL.
func (s *AtlassianSession) GetAuthURL() (string, error) {
	if s.AuthURL == "" {
		return "", fmt.Errorf("auth URL not found")
	}
	return s.AuthURL, nil
}

// Authorize completes the OAuth flow.
func (s *AtlassianSession) Authorize(provider goth.Provider, params goth.Params) (string, error) {
	p := provider.(*AtlassianProvider)
	token, err := p.config.Exchange(goth.ContextForClient(p.Client()), params.Get("code"))
	if err != nil {
		return "", err
	}

	s.AccessToken = token.AccessToken
	s.RefreshToken = token.RefreshToken
	s.ExpiresAt = token.Expiry
	return token.AccessToken, nil
}

// Marshal returns a JSON string of the session.
func (s *AtlassianSession) Marshal() string {
	b, _ := json.Marshal(s) // #nosec G117 -- required by goth.Session interface; token fields must be serialized for session persistence
	return string(b)
}
