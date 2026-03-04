package web

import "veloria/internal/ui"

// Type aliases — canonical definitions live in internal/ui/types.go.
// These aliases keep all existing handler code compiling unchanged.
type (
	OGMeta                 = ui.OGMeta
	PageData               = ui.PageData
	LoginData              = ui.LoginData
	SearchSummary          = ui.SearchSummary
	DataSourceSummary      = ui.DataSourceSummary
	HomeData               = ui.HomeData
	SearchesData           = ui.SearchesData
	SearchResultsData      = ui.SearchResultsData
	SearchViewData         = ui.SearchViewData
	SearchExtensionsData   = ui.SearchExtensionsData
	ExtensionResultSummary = ui.ExtensionResultSummary
	ExtensionResultsData   = ui.ExtensionResultsData
	SearchContextLine      = ui.SearchContextLine
	SearchContextData      = ui.SearchContextData
	DataSourcesData        = ui.DataSourcesData
	DataSourceItem         = ui.DataSourceItem
	ChartData              = ui.ChartData
	LargestExtension       = ui.LargestExtension
	DataSourceData         = ui.DataSourceData
	FileStat               = ui.FileStat
	LargestDataSourceFile  = ui.LargestDataSourceFile
	DataSourceItemsData    = ui.DataSourceItemsData
	ReportedSearchItem     = ui.ReportedSearchItem
	ReportsPageData        = ui.ReportsPageData
	VisibilityToggleData   = ui.VisibilityToggleData
	ExtensionData          = ui.ExtensionData
	FailedIndexEvent       = ui.FailedIndexEvent
	FailedIndexData        = ui.FailedIndexData
)
