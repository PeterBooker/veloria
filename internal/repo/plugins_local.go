package repo

import (
	"context"
	"fmt"
)

var localPluginSlugs = []string{
	"bbpress",
	"classic-editor",
	"contact-form-7",
	"elementor",
	"gutenberg",
	"jetpack",
	"mailpoet",
	"woocommerce",
	"wordpress-seo",
}

func FetchLocalPlugins(ctx context.Context) ([]Plugin, error) {
	var plugins []Plugin
	for _, slug := range localPluginSlugs {
		p, err := FetchPluginInfo(ctx, slug)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch plugin info for %s: %w", slug, err)
		}
		plugins = append(plugins, *p)
	}

	return plugins, nil
}
