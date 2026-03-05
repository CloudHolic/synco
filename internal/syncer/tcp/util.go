package tcp

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func GetOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}

	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

func PostRemoteDaemon(host, path, nodeID, body string) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	url := fmt.Sprintf("https://%s%s", host, path)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Synco-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	req.Header.Set("X-Synco-Node-Id", nodeID)

	return client.Do(req)
}
