package tcp

import "net"

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
