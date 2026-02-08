package repo

import (
	"fmt"
	"time"
)

var localCoreVersions = []string{
	"6.8.2",
	"6.8.1",
	"6.8",
	"6.7.3",
	"6.7.2",
	"6.7.1",
	"6.7",
}

var coreZipDownloadURL = "https://wordpress.org/wordpress-%s.zip"

func FetchLocalCores() ([]Core, error) {
	var cores []Core
	for _, version := range localCoreVersions {
		c := &Core{
			Name:     "",
			Version:  version,
			ZipURL:   fmt.Sprintf(coreZipDownloadURL, version),
			Released: time.Now(),
		}
		cores = append(cores, *c)
	}

	return cores, nil
}
