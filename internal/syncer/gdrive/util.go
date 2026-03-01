package gdrive

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"google.golang.org/api/googleapi"
)

func splitPath(p string) []string {
	p = strings.Trim(filepath.ToSlash(p), "/")
	if p == "" {
		return nil
	}

	return strings.Split(p, "/")
}

func escapeName(name string) string {
	return strings.ReplaceAll(name, "'", "\\'")
}

func isNotFound(err error) bool {
	if apiErr, ok := errors.AsType[*googleapi.Error](err); ok {
		return apiErr.Code == http.StatusNotFound
	}

	return false
}
