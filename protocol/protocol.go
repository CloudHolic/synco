package protocol

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
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
	Path     string
	Checksum []byte
	Data     []byte
}

type Response struct {
	Code ResponseCode
	Msg  string
}

func WriteMessage(w io.Writer, msg Message) error {
	if _, err := w.Write([]byte{byte(msg.Type)}); err != nil {
		return err
	}

	pathBytes := []byte(msg.Path)
	if err := binary.Write(w, binary.BigEndian, uint32(len(pathBytes))); err != nil {
		return err
	}

	if _, err := w.Write(pathBytes); err != nil {
		return err
	}

	if msg.Type == MessageSync {
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

	var pathLen uint32
	if err := binary.Read(r, binary.BigEndian, &pathLen); err != nil {
		return msg, err
	}
	pathBuf := make([]byte, pathLen)
	if _, err := io.ReadFull(r, pathBuf); err != nil {
		return msg, err
	}
	msg.Path = string(pathBuf)

	if msg.Type == MessageSync {
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

	msgBytes := []byte(resp.Msg)
	if err := binary.Write(w, binary.BigEndian, uint32(len(msgBytes))); err != nil {
		return err
	}

	if len(msgBytes) > 0 {
		_, err := w.Write(msgBytes)
		return err
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

	var msgLen uint32
	if err := binary.Read(r, binary.BigEndian, &msgLen); err != nil {
		return resp, err
	}

	if msgLen > 0 {
		msgBuf := make([]byte, msgLen)
		if _, err := io.ReadFull(r, msgBuf); err != nil {
			return resp, err
		}

		resp.Msg = string(msgBuf)
	}

	return resp, nil
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
