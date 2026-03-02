package web

import (
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"veloria/internal/ui/page"
	"veloria/internal/ui/partial"
)

// ReposPage renders the repositories listing page.
func ReposPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Stats == nil {
			http.Error(w, "Repositories are unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		pluginTotal, pluginIndexed, _ := d.Stats.Stats("plugins")
		themeTotal, themeIndexed, _ := d.Stats.Stats("themes")
		coreTotal, coreIndexed, _ := d.Stats.Stats("cores")

		repoSummaries := []RepoSummary{
			BuildRepoSummary("plugins", "Plugins", pluginTotal, pluginIndexed),
			BuildRepoSummary("themes", "Themes", themeTotal, themeIndexed),
			BuildRepoSummary("cores", "Core", coreTotal, coreIndexed),
		}

		pd := d.PageData(r)
		pd.OG.Title = "Repositories - Veloria"
		pd.OG.Description = "Browse WordPress plugin, theme, and core repositories indexed by Veloria."

		data := ReposData{
			PageData:      pd,
			RepoSummaries: repoSummaries,
		}

		d.RenderComponent(w, r, page.Repos(data))
	}
}

// RepoPage renders a single repository view.
func RepoPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Stats == nil || d.DB == nil {
			http.Error(w, "Repository data is unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		repoType := chi.URLParam(r, "type")

		var total, indexed int
		var title string
		var activeInstalls, fileCount, fileSize ChartData

		var largestBySize, largestByFileCount []LargestExtension

		switch repoType {
		case "plugins":
			total, indexed, _ = d.Stats.Stats("plugins")
			title = "Plugins"
			activeInstalls = fetchActiveInstallsChart(d, "plugins")
			fileCount, fileSize = fetchFileStatsCharts(d, "plugins")
			largestBySize = fetchLargestExtensions(d, "plugins", 25, "total_size")
			largestByFileCount = fetchLargestExtensions(d, "plugins", 25, "file_count")
		case "themes":
			total, indexed, _ = d.Stats.Stats("themes")
			title = "Themes"
			activeInstalls = fetchActiveInstallsChart(d, "themes")
			fileCount, fileSize = fetchFileStatsCharts(d, "themes")
			largestBySize = fetchLargestExtensions(d, "themes", 25, "total_size")
			largestByFileCount = fetchLargestExtensions(d, "themes", 25, "file_count")
		case "cores":
			total, indexed, _ = d.Stats.Stats("cores")
			title = "Core"
			fileCount, fileSize = fetchFileStatsCharts(d, "cores")
			largestBySize = fetchLargestExtensions(d, "cores", 25, "total_size")
			largestByFileCount = fetchLargestExtensions(d, "cores", 25, "file_count")
		default:
			http.Error(w, "Repository not found", http.StatusNotFound)
			return
		}

		pd := d.PageData(r)
		pd.OG.Title = fmt.Sprintf("%s Repository - Veloria", title)
		pd.OG.Description = fmt.Sprintf("Browse %d %s (%d indexed) in the Veloria code search index.", total, repoType, indexed)

		// Show only the most recent failure per slug, excluding slugs whose
		// latest event is a success (i.e. they recovered since the failure).
		var failedEvents []FailedIndexEvent
		d.DB.Raw(`
			SELECT slug, error_message, created_at FROM (
				SELECT DISTINCT ON (slug) slug, status, error_message, created_at
				FROM index_events
				WHERE repo_type = ?
				ORDER BY slug, created_at DESC
			) latest
			WHERE status = 'failed'
			ORDER BY created_at DESC
			LIMIT 50`, repoType).Scan(&failedEvents)

		data := RepoData{
			PageData:           pd,
			RepoSummary:        BuildRepoSummary(repoType, title, total, indexed),
			ActiveInstalls:     activeInstalls,
			FileCount:          fileCount,
			FileSize:           fileSize,
			LargestBySize:      largestBySize,
			LargestByFileCount: largestByFileCount,
			FailedEvents:       failedEvents,
		}

		d.RenderComponent(w, r, page.Repo(data))
	}
}

// RepoItemsPartial renders the paginated, searchable items list as an HTMX partial.
func RepoItemsPartial(d *Deps, repoType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Stats == nil || d.DB == nil {
			http.Error(w, "Repository data is unavailable.", http.StatusServiceUnavailable)
			return
		}

		const pageSize = 25
		page := 1
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
				page = parsed
			}
		}
		search := r.URL.Query().Get("search")

		var items []RepoItem
		var total int

		switch repoType {
		case "plugins":
			items, total = fetchPluginItems(d, page, pageSize, search)
		case "themes":
			items, total = fetchThemeItems(d, page, pageSize, search)
		case "cores":
			items, total = fetchCoreItems(d, page, pageSize, search)
		default:
			http.Error(w, "Repository not found", http.StatusNotFound)
			return
		}

		totalPages := int(math.Ceil(float64(total) / float64(pageSize)))
		if totalPages == 0 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}

		data := RepoItemsData{
			Repo:       repoType,
			Items:      items,
			Page:       page,
			TotalPages: totalPages,
			Search:     search,
		}

		d.RenderComponent(w, r, partial.RepoItems(data))
	}
}

func fetchPluginItems(d *Deps, page int, pageSize int, search string) ([]RepoItem, int) {
	offset := (page - 1) * pageSize

	type pluginRow struct {
		ID         uuid.UUID
		Name       string
		Slug       string
		Version    string
		Downloaded int
		FileCount  int
		TotalSize  int64
	}

	query := d.DB.Table("plugins").Where("deleted_at IS NULL")
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}

	var total int64
	query.Count(&total)

	var rows []pluginRow
	query.
		Select("id, name, slug, version, downloaded, file_count, total_size").
		Order("updated_at DESC").
		Order("slug ASC").
		Limit(pageSize).
		Offset(offset).
		Scan(&rows)

	indexStatus := d.Stats.IndexStatus("plugins")
	items := make([]RepoItem, len(rows))
	for i, row := range rows {
		items[i] = RepoItem{
			ID:         row.ID,
			Name:       html.UnescapeString(row.Name),
			Slug:       row.Slug,
			Version:    row.Version,
			Indexed:    indexStatus[row.Slug],
			Downloaded: row.Downloaded,
			FileCount:  row.FileCount,
			TotalSize:  row.TotalSize,
		}
	}
	return items, int(total)
}

func fetchThemeItems(d *Deps, page int, pageSize int, search string) ([]RepoItem, int) {
	offset := (page - 1) * pageSize

	type themeRow struct {
		ID         uuid.UUID
		Name       string
		Slug       string
		Version    string
		Downloaded int
		FileCount  int
		TotalSize  int64
	}

	query := d.DB.Table("themes").Where("deleted_at IS NULL")
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}

	var total int64
	query.Count(&total)

	var rows []themeRow
	query.
		Select("id, name, slug, version, downloaded, file_count, total_size").
		Order("updated_at DESC").
		Order("slug ASC").
		Limit(pageSize).
		Offset(offset).
		Scan(&rows)

	indexStatus := d.Stats.IndexStatus("themes")
	items := make([]RepoItem, len(rows))
	for i, row := range rows {
		items[i] = RepoItem{
			ID:         row.ID,
			Name:       html.UnescapeString(row.Name),
			Slug:       row.Slug,
			Version:    row.Version,
			Indexed:    indexStatus[row.Slug],
			Downloaded: row.Downloaded,
			FileCount:  row.FileCount,
			TotalSize:  row.TotalSize,
		}
	}
	return items, int(total)
}

func fetchCoreItems(d *Deps, page int, pageSize int, search string) ([]RepoItem, int) {
	offset := (page - 1) * pageSize

	type coreRow struct {
		ID        uuid.UUID
		Version   string
		FileCount int
		TotalSize int64
	}

	query := d.DB.Table("cores").Where("deleted_at IS NULL")
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("version ILIKE ?", like)
	}

	var total int64
	query.Count(&total)

	var rows []coreRow
	query.
		Select("id, version, file_count, total_size").
		Order("version DESC").
		Limit(pageSize).
		Offset(offset).
		Scan(&rows)

	indexStatus := d.Stats.IndexStatus("cores")
	items := make([]RepoItem, len(rows))
	for i, row := range rows {
		items[i] = RepoItem{
			ID:        row.ID,
			Name:      "WordPress " + row.Version,
			Slug:      row.Version,
			Version:   row.Version,
			Indexed:   indexStatus[row.Version],
			FileCount: row.FileCount,
			TotalSize: row.TotalSize,
		}
	}
	return items, int(total)
}

func fetchActiveInstallsChart(d *Deps, table string) ChartData {
	cacheKey := "active_installs_chart:" + table
	if d.Cache != nil {
		if v, ok := d.Cache.Get(cacheKey); ok {
			return v.(ChartData)
		}
	}

	type row struct {
		ActiveInstalls int64
	}

	var rows []row
	d.DB.Table(table).
		Select("active_installs").
		Where("deleted_at IS NULL").
		Order("active_installs ASC").
		Scan(&rows)

	values := make([]int64, len(rows))
	for i, r := range rows {
		values[i] = r.ActiveInstalls
	}

	result := BuildChartData(values)
	if d.Cache != nil {
		d.Cache.Set(cacheKey, result, 5*time.Minute)
	}
	return result
}

func fetchFileStatsCharts(d *Deps, table string) (ChartData, ChartData) {
	cacheKey := "file_stats_charts:" + table
	if d.Cache != nil {
		if v, ok := d.Cache.Get(cacheKey); ok {
			pair := v.([2]ChartData)
			return pair[0], pair[1]
		}
	}

	type sizeRow struct{ TotalSize int64 }
	var sizeRows []sizeRow
	d.DB.Table(table).
		Select("total_size").
		Where("deleted_at IS NULL").
		Order("total_size ASC").
		Scan(&sizeRows)

	sizes := make([]int64, len(sizeRows))
	for i, r := range sizeRows {
		sizes[i] = r.TotalSize
	}

	type countRow struct{ FileCount int64 }
	var countRows []countRow
	d.DB.Table(table).
		Select("file_count").
		Where("deleted_at IS NULL").
		Order("file_count ASC").
		Scan(&countRows)

	counts := make([]int64, len(countRows))
	for i, r := range countRows {
		counts[i] = r.FileCount
	}

	countChart, sizeChart := BuildChartData(counts), BuildChartData(sizes)
	if d.Cache != nil {
		d.Cache.Set(cacheKey, [2]ChartData{countChart, sizeChart}, 5*time.Minute)
	}
	return countChart, sizeChart
}

func fetchLargestExtensions(d *Deps, table string, limit int, orderCol string) []LargestExtension {
	nameCol := "name"
	slugCol := "slug"
	if table == "cores" {
		nameCol = "'WordPress ' || version"
		slugCol = "version"
	}

	var extensions []LargestExtension
	d.DB.Table(table).
		Select(slugCol+" AS slug, "+nameCol+" AS name, total_size, file_count").
		Where("deleted_at IS NULL AND "+orderCol+" > 0").
		Order(orderCol + " DESC").
		Limit(limit).
		Scan(&extensions)
	for i := range extensions {
		extensions[i].Name = html.UnescapeString(extensions[i].Name)
	}
	return extensions
}
