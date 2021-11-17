package index

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"net"
)

//===============================================
// Key
//===============================================
type RecordType byte

const (
	MACType RecordType = iota
	ProtoType
	IPv4Type
	IPv6Type
	PortType
)

type Key struct {
	RecType RecordType
	Data    []byte
}

func NewMACKey(macAddr net.HardwareAddr) *Key {
	return &Key{
		RecType: MACType,
		Data:    macAddr,
	}
}

func NewProtoKey(proto uint8) *Key {
	d := make([]byte, 1)
	d[0] = proto
	return &Key{
		RecType: ProtoType,
		Data:    d,
	}
}

func NewIPv4Key(ip net.IP) *Key {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil
	}
	return &Key{
		RecType: IPv4Type,
		Data:    ip4,
	}
}

func NewIPv6Key(ip net.IP) *Key {
	ip6 := ip.To16()
	if ip6 == nil {
		return nil
	}
	return &Key{
		RecType: IPv6Type,
		Data:    ip6,
	}
}

func NewPortKey(port uint16) *Key {
	d := make([]byte, 2)
	binary.LittleEndian.PutUint16(d, port)
	return &Key{
		RecType: PortType,
		Data:    d,
	}
}

func (k *Key) Hash() uint32 {
	h := fnv.New32a()
	h.Write([]byte{byte(k.RecType)})
	h.Write(k.Data)
	return h.Sum32()
}

func (k *Key) Equal(otherK *Key) bool {
	if k.RecType != otherK.RecType {
		return false
	}
	return bytes.Equal(k.Data, otherK.Data)
}

func (k *Key) MarshalBinary() (data []byte, err error) {
	b := make([]byte, len(k.Data)+1)
	b[0] = byte(k.RecType)
	copy(b[1:], k.Data)
	return b, nil
}

func (k *Key) UnmarshalBinary(data []byte) error {
	k.RecType = RecordType(data[0])
	k.Data = make([]byte, len(data)-1)
	copy(k.Data, data[1:])
	return nil
}

func (k *Key) String() string {
	switch k.RecType {
	case MACType:
		return fmt.Sprintf("MAC: %s", net.HardwareAddr(k.Data).String())
	case ProtoType:
		return fmt.Sprintf("Proto: %d", k.Data)
	case IPv4Type:
		return fmt.Sprintf("IPv4: %s", net.IP(k.Data).String())
	case IPv6Type:
		return fmt.Sprintf("IPv6: %s", net.IP(k.Data).String())
	case PortType:
		port := binary.LittleEndian.Uint16(k.Data)
		return fmt.Sprintf("Port: %d", port)
	default:
		return ""
	}
}

//===============================================
// Value Element
//===============================================
type ValueElement struct {
	PathIdx byte
	Offset  uint32
}

func NewValueElement(pathIdx byte, offset uint32) *ValueElement {
	return &ValueElement{
		PathIdx: pathIdx,
		Offset:  offset,
	}
}

func (v *ValueElement) MarshalBinary() (data []byte, err error) {
	b := make([]byte, 5)
	b[0] = v.PathIdx
	binary.LittleEndian.PutUint32(b[1:], v.Offset)
	return b, nil
}

func (v *ValueElement) UnmarshalBinary(data []byte) error {
	v.PathIdx = data[0]
	v.Offset = binary.LittleEndian.Uint32(data[1:])
	return nil
}

//===============================================
// Value
//===============================================
type Value []*ValueElement

func NewValue() *Value {
	v := make(Value, 0, 1024)
	return &v
}

func (v *Value) Append(ve *ValueElement) {
	*v = append(*v, ve)
}

func (v *Value) MarshalBinary() (data []byte, err error) {
	b := make([]byte, len(*v)*5)
	offset := 0
	for _, elem := range *v {
		data, _ := elem.MarshalBinary()
		copy(b[offset:offset+5], data)
		offset += 5
	}
	return b, nil
}

func (v *Value) UnmarshalBinary(data []byte) (err error) {
	for offset := 0; offset < len(data); offset += 5 {
		elem := &ValueElement{}
		err = elem.UnmarshalBinary(data[offset : offset+5])
		if err != nil {
			return err
		}
		*v = append(*v, elem)
	}
	return nil
}

//===============================================
// Mem Index
//===============================================
type MiValue struct {
	K *Key
	V *Value
}

type MemIndex map[uint32]MiValue

func NewMemIndex() MemIndex {
	return make(MemIndex)
}

func (m MemIndex) Put(k *Key, ve *ValueElement) {
	hash := k.Hash()

	for {
		miv, contains := m[hash]
		if !contains {
			v := NewValue()
			v.Append(ve)
			m[hash] = MiValue{K: k, V: v}
			break
		}
		if miv.K.Equal(k) {
			miv.V.Append(ve)
			break
		}
		hash++
	}
}
