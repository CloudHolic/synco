package tcp

import (
	"fmt"
	"strings"
)

type Endpoint struct {
	Host string
	Path string
}

func ParseEndpoint(raw string) Endpoint {
	colonIdx := strings.Index(raw, ":")
	slashIdx := strings.Index(raw, "/")

	// Colon 없으면 로컬 경로
	if colonIdx == -1 {
		return Endpoint{Path: raw}
	}

	// Colon이 Slash보다 앞이어도 로컬 경로 (ex. /home/...)
	if slashIdx != -1 && slashIdx < colonIdx {
		return Endpoint{Path: raw}
	}

	// host:port/path
	rest := raw[colonIdx+1:]
	nextSlash := strings.Index(rest, "/")
	if nextSlash == -1 {
		return Endpoint{Host: raw, Path: ""}
	}

	host := raw[:colonIdx+1+nextSlash]
	path := rest[nextSlash:]
	return Endpoint{Host: host, Path: path}
}

func (e Endpoint) IsRemote() bool {
	return e.Host != ""
}

func (e Endpoint) DaemonURL(apiPath string) string {
	return fmt.Sprintf("http://%s%s", e.Host, apiPath)
}
