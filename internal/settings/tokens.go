package settings

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/auth"
	"veloria/internal/ui"
	"veloria/internal/ui/page"
	"veloria/internal/web"
)

// TokensPage renders the API tokens management page.
func TokensPage(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		db := d.DB()
		if db == nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}

		tokens, err := auth.ListTokens(r.Context(), db, currentUser.ID)
		if err != nil {
			http.Error(w, "Failed to load tokens", http.StatusInternalServerError)
			return
		}

		pd := d.PageData(r)
		data := ui.TokensPageData{
			PageData: pd,
			Tokens:   tokens,
		}
		d.RenderComponent(w, r, page.SettingsTokens(data))
	}
}

// CreateToken handles POST /settings/tokens to create a new API token.
func CreateToken(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		db := d.DB()
		if db == nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}

		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "Untitled token"
		}
		if len(name) > 100 {
			name = name[:100]
		}

		rawToken, _, err := auth.CreateToken(r.Context(), db, currentUser.ID, name)
		if err != nil {
			// Re-render the page with error.
			tokens, _ := auth.ListTokens(r.Context(), db, currentUser.ID)
			pd := d.PageData(r)
			data := ui.TokensPageData{
				PageData: pd,
				Tokens:   tokens,
				Error:    err.Error(),
			}
			d.RenderComponent(w, r, page.SettingsTokens(data))
			return
		}

		// Re-render the page showing the newly created token.
		tokens, _ := auth.ListTokens(r.Context(), db, currentUser.ID)
		pd := d.PageData(r)
		data := ui.TokensPageData{
			PageData:    pd,
			Tokens:      tokens,
			NewRawToken: rawToken,
		}
		d.RenderComponent(w, r, page.SettingsTokens(data))
	}
}

// DeleteToken handles DELETE /settings/tokens/{id}.
func DeleteToken(d *web.Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.UserFromContext(r.Context())
		if currentUser == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		db := d.DB()
		if db == nil {
			http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
			return
		}

		tokenID, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "Invalid token ID", http.StatusBadRequest)
			return
		}

		if err := auth.DeleteToken(r.Context(), db, currentUser.ID, tokenID); err != nil {
			http.Error(w, "Failed to delete token", http.StatusInternalServerError)
			return
		}

		// HTMX: redirect back to the tokens page to refresh the list.
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/settings/tokens")
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/settings/tokens", http.StatusSeeOther)
	}
}
