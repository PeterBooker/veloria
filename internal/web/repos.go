package web

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// ReposPage renders the repositories listing page.
func ReposPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Manager == nil {
			http.Error(w, "Repositories are unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		pluginTotal, pluginIndexed := d.Manager.GetPluginRepo().Stats()
		themeTotal, themeIndexed := d.Manager.GetThemeRepo().Stats()
		coreTotal, coreIndexed := d.Manager.GetCoreRepo().Stats()

		repoSummaries := []RepoSummary{
			BuildRepoSummary("plugins", "Plugins", pluginTotal, pluginIndexed),
			BuildRepoSummary("themes", "Themes", themeTotal, themeIndexed),
			BuildRepoSummary("cores", "Core", coreTotal, coreIndexed),
		}

		data := ReposData{
			PageData:      d.PageData(r),
			RepoSummaries: repoSummaries,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "repos.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// RepoPage renders a single repository view.
func RepoPage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Manager == nil || d.DB == nil {
			http.Error(w, "Repository data is unavailable while the database is offline.", http.StatusServiceUnavailable)
			return
		}
		repoType := chi.URLParam(r, "type")

		var total, indexed int
		var title string
		var activeInstalls, fileCount, fileSize ChartData

		var largestExtensions []LargestExtension

		switch repoType {
		case "plugins":
			total, indexed = d.Manager.GetPluginRepo().Stats()
			title = "Plugins"
			activeInstalls = fetchActiveInstallsChart(d, "plugins")
			fileCount, fileSize = fetchFileStatsCharts(d, "plugins")
			largestExtensions = fetchLargestExtensions(d, "plugins", 25)
		case "themes":
			total, indexed = d.Manager.GetThemeRepo().Stats()
			title = "Themes"
			activeInstalls = fetchActiveInstallsChart(d, "themes")
			fileCount, fileSize = fetchFileStatsCharts(d, "themes")
			largestExtensions = fetchLargestExtensions(d, "themes", 25)
		case "cores":
			total, indexed = d.Manager.GetCoreRepo().Stats()
			title = "Core"
			fileCount, fileSize = fetchFileStatsCharts(d, "cores")
			largestExtensions = fetchLargestExtensions(d, "cores", 25)
		default:
			http.Error(w, "Repository not found", http.StatusNotFound)
			return
		}

		data := RepoData{
			PageData:          d.PageData(r),
			RepoSummary:       BuildRepoSummary(repoType, title, total, indexed),
			ActiveInstalls:    activeInstalls,
			FileCount:         fileCount,
			FileSize:          fileSize,
			LargestExtensions: largestExtensions,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "repo.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// RepoItemsPartial renders the paginated, searchable items list as an HTMX partial.
func RepoItemsPartial(d *Deps, repoType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Manager == nil || d.DB == nil {
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

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "repo-items.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
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

	indexStatus := d.Manager.GetPluginRepo().IndexStatus()
	items := make([]RepoItem, len(rows))
	for i, row := range rows {
		items[i] = RepoItem{
			ID:         row.ID,
			Name:       row.Name,
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

	indexStatus := d.Manager.GetThemeRepo().IndexStatus()
	items := make([]RepoItem, len(rows))
	for i, row := range rows {
		items[i] = RepoItem{
			ID:         row.ID,
			Name:       row.Name,
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

	indexStatus := d.Manager.GetCoreRepo().IndexStatus()
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

func fetchLargestExtensions(d *Deps, table string, limit int) []LargestExtension {
	nameCol := "name"
	slugCol := "slug"
	if table == "cores" {
		nameCol = "'WordPress ' || version"
		slugCol = "version"
	}

	var extensions []LargestExtension
	d.DB.Table(table).
		Select(slugCol+" AS slug, "+nameCol+" AS name, total_size").
		Where("deleted_at IS NULL AND total_size > 0").
		Order("total_size DESC").
		Limit(limit).
		Scan(&extensions)
	return extensions
}
