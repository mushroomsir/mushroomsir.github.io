package wire

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
)

// Header ...
type Header struct {
	DestinationAddress net.HardwareAddr
	SourceAddress      net.HardwareAddr
	EtherType          uint16
	//FCS  以太网帧尾部的FCS，发送的时候由硬件计算并添加，接收的时候由硬件校验并去除。
}

func (h *Header) String() string {
	return fmt.Sprintf("DestinationAddress: %v SourceAddress: %v EtherType: %v", h.DestinationAddress.String(), h.SourceAddress.String(), EtherType(h.EtherType))
}

// EtherType ...
func EtherType(et uint16) string {
	switch et {
	case 0x800:
		return "ipv4"
	}
	return "unknow" + strconv.FormatUint(uint64(et), 10)
}

// ParseHeader ...
func ParseHeader(buf []byte) *Header {
	header := new(Header)
	var hd net.HardwareAddr
	hd = buf[0:6]
	header.DestinationAddress = hd
	hd = buf[6:12]
	header.SourceAddress = hd
	header.EtherType = binary.BigEndian.Uint16(buf[12:14])
	return header
}

// Marshal ...
func (h *Header) Marshal() []byte {
	b := make([]byte, 14)
	copy(b[0:6], h.DestinationAddress)
	copy(b[6:12], h.SourceAddress)

	etype := make([]byte, 2)
	binary.BigEndian.PutUint16(etype, h.EtherType)
	copy(b[12:14], etype)
	return b
}

// Htons ...
func Htons(i uint16) uint16 {
	return (i<<8)&0xff00 | i>>8
}
