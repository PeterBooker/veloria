package repo

import (
	"context"
	"fmt"
)

var localThemeSlugs = []string{
	"twentytwentyfive",
	"hello-elementor",
	"twentytwentyfour",
	"astra",
	"bluehost-blueprint",
	"twentytwentythree",
	"kadence",
	"hello-biz",
	"oceanwp",
}

func FetchLocalThemes(ctx context.Context, api *APIClient) ([]Theme, error) {
	var themes []Theme
	for _, slug := range localThemeSlugs {
		p, err := FetchThemeInfo(ctx, api, slug)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch theme info for %s: %w", slug, err)
		}
		themes = append(themes, *p)
	}

	return themes, nil
}
