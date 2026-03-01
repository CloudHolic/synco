package auth

import (
	"context"

	"github.com/dropbox/dropbox-sdk-go-unofficial/v6/dropbox/files"
	"google.golang.org/api/drive/v3"
)

type Provider interface {
	Authorize() error
}

type GDriveProvider interface {
	Provider
	NewService(ctx context.Context) (*drive.Service, error)
}

type DropboxProvider interface {
	Provider
	NewClient() (files.Client, error)
}

var (
	GDrive  GDriveProvider  = &gdriveProvider{}
	Dropbox DropboxProvider = &dropboxProvider{}
)
