package capture

import (
	"sync"

	"code.ornl.gov/situ/mercury/common"
	"github.com/google/gopacket"
	"github.com/rs/zerolog/log"
)

const (
	ExtractorChanSize = 8192
)

func extractPacket(inChan chan *Message, done *sync.WaitGroup) (chan *Message, error) {
	outCh := make(chan *Message, ExtractorChanSize)

	logger := log.With().Str("component", "packet-extractor").Logger()

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			defer close(outCh)
			done.Done()
		}()

		for msg := range inChan {
			if msg.msgType == msgTypePacket {
				packet := msg.Get(msgPayloadPacket).(gopacket.Packet)

				_, srcMAC, dstMAC, srcIP, dstIP, srcPort, dstPort, proto, _ := common.ParsePacket(packet)
				msg.Set(msgPayloadSrcMAC, srcMAC)
				msg.Set(msgPayloadDstMAC, dstMAC)
				msg.Set(msgPayloadSrcIP, srcIP)
				msg.Set(msgPayloadDstIP, dstIP)
				msg.Set(msgPayloadSrcPort, srcPort)
				msg.Set(msgPayloadDstPort, dstPort)
				msg.Set(msgPayloadIPProto, proto)

			}

			outCh <- msg

		}
	}()

	return outCh, nil
}
