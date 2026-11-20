package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"veloria/internal/user"
)

const (
	testOrigin    = "https://veloria.invalid"
	routeClientID = "route-client"
	otherClientID = "other-client"
	chosenState   = "STATE-CHOSEN-BY-THE-CALLER"
)

// stubIdP answers the Atlassian token and profile endpoints in-process and
// refuses every other request, so no test traffic leaves the process.
type stubIdP struct {
	mu        sync.Mutex
	grants    map[string]string // unused authorization code -> provider account ID
	tokens    map[string]string // access token -> provider account ID
	exchanges []string          // codes presented to the token endpoint
}

func newStubIdP() *stubIdP {
	return &stubIdP{grants: make(map[string]string), tokens: make(map[string]string)}
}

// grant issues a single-use authorization code for a provider account.
func (s *stubIdP) grant(code, account string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[code] = account
}

// exchanged returns the codes presented to the token endpoint so far.
func (s *stubIdP) exchanged() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.exchanges...)
}

func (s *stubIdP) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case req.Method == http.MethodPost && req.URL.String() == atlassianTokenURL:
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil, err
		}
		code := form.Get("code")
		s.exchanges = append(s.exchanges, code)
		account, ok := s.grants[code]
		if !ok {
			return jsonResponse(req, http.StatusBadRequest, `{"error":"invalid_grant"}`), nil
		}
		delete(s.grants, code)
		token := "token-for-" + account
		s.tokens[token] = account
		return jsonResponse(req, http.StatusOK,
			fmt.Sprintf(`{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, token)), nil

	case req.Method == http.MethodGet && req.URL.String() == atlassianUserURL:
		account, ok := s.tokens[strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")]
		if !ok {
			return jsonResponse(req, http.StatusUnauthorized, `{}`), nil
		}
		return jsonResponse(req, http.StatusOK,
			fmt.Sprintf(`{"account_id":%q,"email":%q,"name":%q}`, account, account+"@example.test", account)), nil
	}
	return nil, fmt.Errorf("stub identity provider: unexpected request to %s", req.URL)
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	res := rec.Result()
	res.Request = req
	return res
}

// localUserID is the local user the fixture resolves a provider account to.
func localUserID(account string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(account))
}

// authFixture serves the auth routes on the same paths as the router, with
// an in-memory session store, a stubbed identity provider, and no database.
type authFixture struct {
	t       *testing.T
	store   *memSessionStore
	idp     *stubIdP
	handler http.Handler
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()

	store := newMemSessionStore()
	idp := newStubIdP()
	client := &http.Client{Transport: idp}

	routed := NewAtlassianProvider(routeClientID, "dummy-secret", testOrigin+"/auth/atlassian/callback")
	routed.HTTPClient = client
	other := NewAtlassianProvider(otherClientID, "dummy-secret", testOrigin+"/auth/other/callback")
	other.SetName("other")
	other.HTTPClient = client

	prevStore := gothic.Store
	gothic.Store = store
	goth.UseProviders(routed, other)
	t.Cleanup(func() {
		gothic.Store = prevStore
		goth.ClearProviders()
	})

	h := NewHandler(nil, &SessionStore{store: store}, zap.NewNop())
	h.resolveUser = func(gu goth.User) (*user.User, error) {
		return &user.User{ID: localUserID(gu.UserID), Email: gu.Email}, nil
	}

	r := chi.NewRouter()
	r.Get("/logout", h.Logout)
	r.Route("/auth", func(r chi.Router) {
		r.Get("/{provider}", h.BeginAuth)
		r.Get("/{provider}/callback", h.Callback)
	})

	return &authFixture{t: t, store: store, idp: idp, handler: r}
}

// browser is one cookie jar that sends requests straight to the handler.
type browser struct {
	f      *authFixture
	origin *url.URL
	jar    *cookiejar.Jar
}

func (f *authFixture) browser() *browser {
	origin, _ := url.Parse(testOrigin)
	jar, _ := cookiejar.New(nil)
	return &browser{f: f, origin: origin, jar: jar}
}

func (b *browser) get(target string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, testOrigin+target, nil)
	for _, c := range b.jar.Cookies(b.origin) {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.f.handler.ServeHTTP(rec, req)
	res := rec.Result()
	b.jar.SetCookies(b.origin, res.Cookies())
	return res
}

func (b *browser) cookie(name string) string {
	for _, c := range b.jar.Cookies(b.origin) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func (b *browser) setCookie(name, value string) {
	b.jar.SetCookies(b.origin, []*http.Cookie{{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true}})
}

// signIn runs the ordinary flow: begin, then the callback carrying the state
// the server put in the provider URL.
func (b *browser) signIn(code string) {
	t := b.f.t
	t.Helper()

	res := b.get("/auth/atlassian")
	require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
	state := providerURL(t, res).Query().Get("state")
	require.NotEmpty(t, state)

	res = b.get("/auth/atlassian/callback?" + url.Values{"code": {code}, "state": {state}}.Encode())
	require.Equal(t, http.StatusSeeOther, res.StatusCode)
	require.Equal(t, "/", res.Header.Get("Location"))
}

func providerURL(t *testing.T, res *http.Response) *url.URL {
	t.Helper()
	loc, err := res.Location()
	require.NoError(t, err)
	return loc
}

func TestBeginAuthIgnoresRequestStateAndProvider(t *testing.T) {
	f := newAuthFixture(t)

	seen := make(map[string]bool)
	for range 50 {
		res := f.browser().get("/auth/atlassian?state=" + chosenState + "&provider=other")
		require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)

		loc := providerURL(t, res)
		require.Equal(t, "auth.atlassian.com", loc.Host)
		require.Equal(t, routeClientID, loc.Query().Get("client_id"), "provider must come from the route")

		state := loc.Query().Get("state")
		require.NotEqual(t, chosenState, state, "state must not come from the request")
		require.GreaterOrEqual(t, len(state), 26, "state must carry at least 128 bits")
		require.False(t, seen[state], "state %q was issued twice", state)
		seen[state] = true
	}

	res := f.browser().get("/auth/unknown?provider=atlassian")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "a provider named only in the query must not be used")
}

func TestBeginAuthConcurrentRequests(t *testing.T) {
	f := newAuthFixture(t)

	const n = 64
	states := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			res := f.browser().get("/auth/atlassian?state=" + chosenState)
			if loc, err := res.Location(); err == nil {
				states[i] = loc.Query().Get("state")
			}
		})
	}
	wg.Wait()

	seen := make(map[string]bool, n)
	for _, state := range states {
		require.NotEmpty(t, state)
		require.NotEqual(t, chosenState, state)
		require.False(t, seen[state], "state %q was issued twice", state)
		seen[state] = true
	}
}

func TestCallbackRejectsStateNotIssuedByServer(t *testing.T) {
	tests := []struct {
		name  string
		begin string
		state string
	}{
		{name: "state chosen in the begin request", begin: "/auth/atlassian?state=" + chosenState, state: chosenState},
		{name: "state guessed after an ordinary begin", begin: "/auth/atlassian", state: chosenState},
		{name: "no state after an ordinary begin", begin: "/auth/atlassian"},
		{name: "no begin request", state: chosenState},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthFixture(t)
			f.idp.grant("attacker-code", "acct-attacker")
			b := f.browser()

			if tt.begin != "" {
				res := b.get(tt.begin)
				require.Equal(t, http.StatusTemporaryRedirect, res.StatusCode)
			}

			query := url.Values{"code": {"attacker-code"}}
			if tt.state != "" {
				query.Set("state", tt.state)
			}
			res := b.get("/auth/atlassian/callback?" + query.Encode())

			assert.Equal(t, http.StatusSeeOther, res.StatusCode)
			assert.Equal(t, "/login?error=auth_failed", res.Header.Get("Location"))
			assert.Empty(t, f.idp.exchanged(), "authorization code reached the token endpoint")
			assert.Empty(t, b.cookie(sessionName), "a session was issued")
		})
	}
}

func TestLoginFlowSignsIn(t *testing.T) {
	f := newAuthFixture(t)
	f.idp.grant("victim-code", "acct-victim")
	f.idp.grant("second-code", "acct-victim")

	b := f.browser()
	b.signIn("victim-code")

	assert.Equal(t, []string{"victim-code"}, f.idp.exchanged())
	assert.Equal(t, localUserID("acct-victim").String(), f.store.userIDFor(t, b.cookie(sessionName)))
	assert.Empty(t, b.cookie(gothic.SessionName), "the pending flow must be consumed")

	// A second begin gets a new state, and the flow works again.
	b.signIn("second-code")
	assert.Equal(t, localUserID("acct-victim").String(), f.store.userIDFor(t, b.cookie(sessionName)))
}

func TestLoginRotatesSession(t *testing.T) {
	tests := []struct {
		name   string
		logout bool
	}{
		{name: "presented session is live"},
		{name: "presented session was logged out", logout: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newAuthFixture(t)
			f.idp.grant("attacker-code", "acct-attacker")
			f.idp.grant("victim-code", "acct-victim")

			// Someone else obtains a session cookie value by signing in.
			attacker := f.browser()
			attacker.signIn("attacker-code")
			known := attacker.cookie(sessionName)
			require.NotEmpty(t, known)
			if tt.logout {
				res := attacker.get("/logout")
				require.Equal(t, http.StatusSeeOther, res.StatusCode)
				require.False(t, f.store.has(known))
			}

			// That value is present in the browser that signs in next.
			victim := f.browser()
			victim.setCookie(sessionName, known)
			victim.signIn("victim-code")

			issued := victim.cookie(sessionName)
			assert.NotEqual(t, known, issued, "login kept the session ID the request presented")
			assert.Equal(t, localUserID("acct-victim").String(), f.store.userIDFor(t, issued))
			assert.False(t, f.store.has(known), "pre-login session row still exists")
			assert.Empty(t, f.store.userIDFor(t, known), "pre-login session ID resolves to a user")
		})
	}
}
