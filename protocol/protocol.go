package protocol

import (
	"encoding/binary"
	"io"
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
