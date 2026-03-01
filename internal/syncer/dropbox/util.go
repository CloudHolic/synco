package dropbox

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
)

func ensureFolder(client files.Client, path string) error {
	arg := files.NewCreateFolderArg(path)
	arg.Autorename = false

	if _, err := client.CreateFolderV2(arg); err != nil {
		if isConflict(err) {
			return nil
		}

		return err
	}

	return nil
}

func normalizePath(p string) string {
	p = "/" + strings.Trim(filepath.ToSlash(p), "/")
	return p
}

func isNotFound(err error) bool {
	if apiErr, ok := errors.AsType[files.DeleteV2APIError](err); ok {
		return apiErr.EndpointError != nil &&
			apiErr.EndpointError.PathLookup != nil &&
			apiErr.EndpointError.PathLookup.Tag == "not_found"
	}

	return false
}

func isConflict(err error) bool {
	if apiErr, ok := errors.AsType[files.CreateFolderV2APIError](err); ok {
		return apiErr.EndpointError != nil &&
			apiErr.EndpointError.Path != nil &&
			apiErr.EndpointError.Path.Tag == "conflict"
	}

	return false
}
