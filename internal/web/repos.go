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
		var items []RepoItem
		var activeInstallsLine, fileCountLine, fileSizeLine LineSeries

		const pageSize = 50
		page := 1
		if pageStr := r.URL.Query().Get("page"); pageStr != "" {
			if parsed, err := strconv.Atoi(pageStr); err == nil && parsed > 0 {
				page = parsed
			}
		}

		var largestFiles []LargestRepoFile

		switch repoType {
		case "plugins":
			total, indexed = d.Manager.GetPluginRepo().Stats()
			title = "Plugins"
			items = fetchPluginItems(d, page, pageSize)
			activeInstallsLine = fetchActiveInstallsLine(d, "plugins")
			fileCountLine, fileSizeLine = fetchFileStatsLines(d, "plugins")
			largestFiles = fetchLargestRepoFiles(d, "plugins", 50)
		case "themes":
			total, indexed = d.Manager.GetThemeRepo().Stats()
			title = "Themes"
			items = fetchThemeItems(d, page, pageSize)
			activeInstallsLine = fetchActiveInstallsLine(d, "themes")
			fileCountLine, fileSizeLine = fetchFileStatsLines(d, "themes")
			largestFiles = fetchLargestRepoFiles(d, "themes", 50)
		case "cores":
			total, indexed = d.Manager.GetCoreRepo().Stats()
			title = "Core"
			items = fetchCoreItems(d, page, pageSize)
			fileCountLine, fileSizeLine = fetchFileStatsLines(d, "cores")
			largestFiles = fetchLargestRepoFiles(d, "cores", 50)
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

		data := RepoData{
			PageData:           d.PageData(r),
			RepoSummary:        BuildRepoSummary(repoType, title, total, indexed),
			Items:              items,
			Page:               page,
			TotalPages:         totalPages,
			ActiveInstallsLine: activeInstallsLine,
			FileCountLine:      fileCountLine,
			FileSizeLine:       fileSizeLine,
			LargestFiles:       largestFiles,
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := d.Templates.Render(w, "repo.html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func fetchPluginItems(d *Deps, page int, pageSize int) []RepoItem {
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

	var rows []pluginRow
	d.DB.Table("plugins").
		Select("id, name, slug, version, downloaded, file_count, total_size").
		Where("deleted_at IS NULL").
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
	return items
}

func fetchThemeItems(d *Deps, page int, pageSize int) []RepoItem {
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

	var rows []themeRow
	d.DB.Table("themes").
		Select("id, name, slug, version, downloaded, file_count, total_size").
		Where("deleted_at IS NULL").
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
	return items
}

func fetchCoreItems(d *Deps, page int, pageSize int) []RepoItem {
	offset := (page - 1) * pageSize

	type coreRow struct {
		ID        uuid.UUID
		Version   string
		FileCount int
		TotalSize int64
	}

	var rows []coreRow
	d.DB.Table("cores").
		Select("id, version, file_count, total_size").
		Where("deleted_at IS NULL").
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
	return items
}

func fetchActiveInstallsLine(d *Deps, table string) LineSeries {
	cacheKey := "active_installs_line:" + table
	if d.Cache != nil {
		if v, ok := d.Cache.Get(cacheKey); ok {
			return v.(LineSeries)
		}
	}

	type row struct {
		ActiveInstalls int64
	}

	var rows []row
	d.DB.Table(table).
		Select("active_installs").
		Where("deleted_at IS NULL").
		Order("name ASC").
		Scan(&rows)

	values := make([]int64, len(rows))
	for i, r := range rows {
		values[i] = r.ActiveInstalls
	}

	result := BuildLineSeries(values)
	if d.Cache != nil {
		d.Cache.Set(cacheKey, result, 5*time.Minute)
	}
	return result
}

func fetchFileStatsLines(d *Deps, table string) (LineSeries, LineSeries) {
	cacheKey := "file_stats_lines:" + table
	if d.Cache != nil {
		if v, ok := d.Cache.Get(cacheKey); ok {
			pair := v.([2]LineSeries)
			return pair[0], pair[1]
		}
	}

	nameCol := "name"
	if table == "cores" {
		nameCol = "version"
	}

	type row struct {
		FileCount int64
		TotalSize int64
	}

	var rows []row
	d.DB.Table(table).
		Select("file_count, total_size").
		Where("deleted_at IS NULL").
		Order(nameCol + " ASC").
		Scan(&rows)

	counts := make([]int64, len(rows))
	sizes := make([]int64, len(rows))
	for i, r := range rows {
		counts[i] = r.FileCount
		sizes[i] = r.TotalSize
	}

	countLine, sizeLine := BuildLineSeries(counts), BuildLineSeries(sizes)
	if d.Cache != nil {
		d.Cache.Set(cacheKey, [2]LineSeries{countLine, sizeLine}, 5*time.Minute)
	}
	return countLine, sizeLine
}

func fetchLargestRepoFiles(d *Deps, repoType string, limit int) []LargestRepoFile {
	var files []LargestRepoFile
	d.DB.Table("largest_repo_files").
		Select("path, size, slug, name").
		Where("repo_type = ?", repoType).
		Order("size DESC").
		Limit(limit).
		Scan(&files)
	return files
}
