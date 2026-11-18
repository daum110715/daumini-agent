package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var rawFS embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(rawFS, "dist")
	if err != nil {
		return rawFS
	}
	return sub
}
