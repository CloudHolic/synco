package tcp

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

type MessageType byte

const (
	MessageSync   MessageType = 0x01
	MessageDelete MessageType = 0x02
	MessagePing   MessageType = 0x03
)

type ResponseCode byte

const (
	ResponseOK   ResponseCode = 0x00
	ResponseErr  ResponseCode = 0x01
	ResponseSkip ResponseCode = 0x02
)

type Message struct {
	Type     MessageType
	OriginID string
	VClock   map[string]uint64
	Path     string
	ModTime  time.Time
	Checksum []byte
	Data     []byte
}

type Response struct {
	Code   ResponseCode
	Msg    string
	VClock map[string]uint64
}

func WriteMessage(w io.Writer, msg Message) error {
	if _, err := w.Write([]byte{byte(msg.Type)}); err != nil {
		return err
	}

	if err := writeString(w, msg.OriginID); err != nil {
		return err
	}

	if err := binary.Write(w, binary.BigEndian, uint32(len(msg.VClock))); err != nil {
		return err
	}
	for k, v := range msg.VClock {
		if err := writeString(w, k); err != nil {
			return err
		}

		if err := binary.Write(w, binary.BigEndian, v); err != nil {
			return err
		}
	}

	if err := writeString(w, msg.Path); err != nil {
		return err
	}

	if msg.Type == MessageSync {
		if err := binary.Write(w, binary.BigEndian, msg.ModTime.UnixNano()); err != nil {
			return err
		}

		if _, err := w.Write(msg.Checksum); err != nil {
			return err
		}

		if err := binary.Write(w, binary.BigEndian, uint64(len(msg.Data))); err != nil {
			return err
		}
		if _, err := w.Write(msg.Data); err != nil {
			return err
		}
	}

	return nil
}

func ReadMessage(r io.Reader) (Message, error) {
	var msg Message

	typeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, typeBuf); err != nil {
		return msg, err
	}
	msg.Type = MessageType(typeBuf[0])

	originID, err := readString(r)
	if err != nil {
		return msg, err
	}
	msg.OriginID = originID

	var clockLen uint32
	if err := binary.Read(r, binary.BigEndian, &clockLen); err != nil {
		return msg, err
	}
	msg.VClock = make(map[string]uint64, clockLen)
	for range clockLen {
		k, err := readString(r)
		if err != nil {
			return msg, err
		}

		var v uint64
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			return msg, err
		}

		msg.VClock[k] = v
	}

	msg.Path, err = readString(r)
	if err != nil {
		return msg, err
	}

	if msg.Type == MessageSync {
		var modTimeNano int64
		if err := binary.Read(r, binary.BigEndian, &modTimeNano); err != nil {
			return msg, err
		}
		msg.ModTime = time.Unix(0, modTimeNano)

		msg.Checksum = make([]byte, 32)
		if _, err := io.ReadFull(r, msg.Checksum); err != nil {
			return msg, err
		}

		var dataLen uint64
		if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
			return msg, err
		}
		msg.Data = make([]byte, dataLen)
		if _, err := io.ReadFull(r, msg.Data); err != nil {
			return msg, err
		}
	}

	return msg, nil
}

func WriteResponse(w io.Writer, resp Response) error {
	if _, err := w.Write([]byte{byte(resp.Code)}); err != nil {
		return err
	}

	if err := writeString(w, resp.Msg); err != nil {
		return err
	}

	if err := binary.Write(w, binary.BigEndian, uint32(len(resp.VClock))); err != nil {
		return err
	}
	for k, v := range resp.VClock {
		if err := writeString(w, k); err != nil {
			return err
		}

		if err := binary.Write(w, binary.BigEndian, v); err != nil {
			return err
		}
	}

	return nil
}

func ReadResponse(r io.Reader) (Response, error) {
	var resp Response

	codeBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, codeBuf); err != nil {
		return resp, err
	}
	resp.Code = ResponseCode(codeBuf[0])

	msg, err := readString(r)
	if err != nil {
		return resp, err
	}
	resp.Msg = msg

	var clockLen uint32
	if err := binary.Read(r, binary.BigEndian, &clockLen); err != nil {
		return resp, err
	}
	resp.VClock = make(map[string]uint64, clockLen)
	for range clockLen {
		k, err := readString(r)
		if err != nil {
			return resp, err
		}

		var v uint64
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			return resp, err
		}

		resp.VClock[k] = v
	}

	return resp, nil
}

func writeString(w io.Writer, s string) error {
	b := []byte(s)
	if err := binary.Write(w, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}

	_, err := w.Write(b)
	return err
}

func readString(r io.Reader) (string, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return "", err
	}

	b := make([]byte, length)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}

	return string(b), nil
}

func FileChecksum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}

	return h.Sum(nil), nil
}

func ChecksumEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func IsRemote(dst string) bool {
	for i, c := range dst {
		if c == ':' && i > 0 {
			return true
		}
	}

	return false
}

func ValidateChecksum(data, expected []byte) error {
	h := sha256.Sum256(data)
	if !ChecksumEqual(h[:], expected) {
		return fmt.Errorf("checksum mismatch")
	}

	return nil
}
