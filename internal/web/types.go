package web

import "veloria/internal/ui"

// Type aliases — canonical definitions live in internal/ui/types.go.
// These aliases keep all existing handler code compiling unchanged.
type (
	OGMeta                 = ui.OGMeta
	PageData               = ui.PageData
	LoginData              = ui.LoginData
	SearchSummary          = ui.SearchSummary
	RepoSummary            = ui.RepoSummary
	HomeData               = ui.HomeData
	SearchesData           = ui.SearchesData
	MySearchesData         = ui.MySearchesData
	SearchResultsData      = ui.SearchResultsData
	SearchViewData         = ui.SearchViewData
	SearchExtensionsData   = ui.SearchExtensionsData
	ExtensionResultSummary = ui.ExtensionResultSummary
	ExtensionResultsData   = ui.ExtensionResultsData
	SearchContextLine      = ui.SearchContextLine
	SearchContextData      = ui.SearchContextData
	ReposData              = ui.ReposData
	RepoItem               = ui.RepoItem
	ChartData              = ui.ChartData
	LargestExtension       = ui.LargestExtension
	RepoData               = ui.RepoData
	FileStat               = ui.FileStat
	LargestRepoFile        = ui.LargestRepoFile
	RepoItemsData          = ui.RepoItemsData
	ReportedSearchItem     = ui.ReportedSearchItem
	ReportsPageData        = ui.ReportsPageData
	VisibilityToggleData   = ui.VisibilityToggleData
	ExtensionData          = ui.ExtensionData
	FailedIndexEvent       = ui.FailedIndexEvent
)
