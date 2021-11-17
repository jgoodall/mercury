package capture

type messageType uint8

const (
	msgTypePacket messageType = iota
	msgTypeFileClosed
	msgTypeNewPcapFile
	msgTypeMemoryIndex
)

type messagePayload uint8

const (
	msgPayloadPacket messagePayload = iota
	msgPayloadPcapPathBase
	msgPayloadPcapFilename
	msgPayloadPcapIdx
	msgPayloadOffset
	msgPayloadIPProto
	msgPayloadSrcMAC
	msgPayloadDstMAC
	msgPayloadSrcIP
	msgPayloadSrcPort
	msgPayloadDstIP
	msgPayloadDstPort
	msgPayloadMemoryIndex
	msgPayloadMemoryIndexFile
)

type Message struct {
	msgType messageType
	payload map[messagePayload]interface{}
}

func NewMessage(msgType messageType) *Message {
	return &Message{
		msgType: msgType,
		payload: make(map[messagePayload]interface{}, 32),
	}
}

func (m *Message) Set(key messagePayload, value interface{}) *Message {
	m.payload[key] = value
	return m
}

func (m *Message) Get(key messagePayload) interface{} {
	value, contains := m.payload[key]
	if !contains {
		return nil
	}
	return value
}
