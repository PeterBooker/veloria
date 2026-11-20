package auth

import (
	"crypto/rand"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memSessionStore is an in-memory sessions.Store that follows pgstore's rules
// for session IDs: the ID in a presented cookie is kept even when no row
// exists for it, Save generates an ID only for a session that has none, and a
// negative MaxAge deletes the row. The cookie value is the bare ID.
type memSessionStore struct {
	mu         sync.Mutex
	rows       map[string]map[any]any
	destroyErr error
}

func newMemSessionStore() *memSessionStore {
	return &memSessionStore{rows: make(map[string]map[any]any)}
}

func (m *memSessionStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(m, name)
}

func (m *memSessionStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(m, name)
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	session.IsNew = true

	if c, err := r.Cookie(name); err == nil && c.Value != "" {
		session.ID = c.Value
		m.mu.Lock()
		if values, ok := m.rows[session.ID]; ok {
			session.Values = maps.Clone(values)
			session.IsNew = false
		}
		m.mu.Unlock()
	}
	return session, nil
}

func (m *memSessionStore) Save(_ *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session.Options.MaxAge < 0 {
		if m.destroyErr != nil {
			return m.destroyErr
		}
		delete(m.rows, session.ID)
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	if session.ID == "" {
		session.ID = rand.Text()
	}
	if _, exists := m.rows[session.ID]; session.IsNew || exists {
		m.rows[session.ID] = maps.Clone(session.Values)
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), session.ID, session.Options))
	return nil
}

// put stores a session row directly, as if an earlier request had saved it.
func (m *memSessionStore) put(id string, values map[any]any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[id] = values
}

func (m *memSessionStore) has(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.rows[id]
	return ok
}

// userIDFor returns the user ID that a veloria-session cookie value resolves
// to, read the way SessionStore.GetUser reads it before its database lookup.
func (m *memSessionStore) userIDFor(t *testing.T, cookieValue string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: sessionName, Value: cookieValue})
	session, err := m.New(r, sessionName)
	require.NoError(t, err)
	userID, _ := session.Values["user_id"].(string)
	return userID
}

// sessionCookies returns the veloria-session cookies set on a response.
func sessionCookies(res *http.Response) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == sessionName {
			out = append(out, c)
		}
	}
	return out
}

func TestSetUserRotatesSession(t *testing.T) {
	const presented = "ID-KNOWN-BEFORE-LOGIN"
	earlier := uuid.New()

	tests := []struct {
		name      string
		presented string
		row       map[any]any
	}{
		{
			name:      "presented session has a row",
			presented: presented,
			row:       map[any]any{"user_id": earlier.String()},
		},
		{
			name:      "presented session was destroyed",
			presented: presented,
		},
		{
			name: "no session presented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newMemSessionStore()
			if tt.row != nil {
				store.put(tt.presented, tt.row)
			}
			s := &SessionStore{store: store}

			r := httptest.NewRequest(http.MethodGet, "/auth/atlassian/callback", nil)
			if tt.presented != "" {
				r.AddCookie(&http.Cookie{Name: sessionName, Value: tt.presented})
			}
			w := httptest.NewRecorder()
			userID := uuid.New()

			require.NoError(t, s.SetUser(w, r, userID))

			cookies := sessionCookies(w.Result())
			require.Len(t, cookies, 1, "exactly one session cookie in the response")
			issued := cookies[0]
			assert.NotEmpty(t, issued.Value)
			assert.Positive(t, issued.MaxAge)
			assert.NotEqual(t, presented, issued.Value, "login kept the session ID the request presented")
			assert.Equal(t, userID.String(), store.userIDFor(t, issued.Value))

			assert.False(t, store.has(presented), "pre-login session row still exists")
			assert.Empty(t, store.userIDFor(t, presented), "pre-login session ID resolves to a user")

			// Later reads in the same request see the new session.
			session, err := store.Get(r, sessionName)
			require.NoError(t, err)
			assert.Equal(t, issued.Value, session.ID)
			assert.Equal(t, userID.String(), session.Values["user_id"])
		})
	}
}

func TestSetUserFailsWhenOldSessionCannotBeDestroyed(t *testing.T) {
	const presented = "ID-KNOWN-BEFORE-LOGIN"
	earlier := uuid.New()

	store := newMemSessionStore()
	store.put(presented, map[any]any{"user_id": earlier.String()})
	store.destroyErr = errors.New("database unavailable")
	s := &SessionStore{store: store}

	r := httptest.NewRequest(http.MethodGet, "/auth/atlassian/callback", nil)
	r.AddCookie(&http.Cookie{Name: sessionName, Value: presented})
	w := httptest.NewRecorder()

	err := s.SetUser(w, r, uuid.New())

	require.ErrorIs(t, err, store.destroyErr)
	assert.Empty(t, sessionCookies(w.Result()), "no session may be issued when the old one survives")
	assert.Equal(t, earlier.String(), store.userIDFor(t, presented), "old session must be left as it was")
	assert.Len(t, store.rows, 1)
}
