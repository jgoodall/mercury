package capture

import (
	"fmt"
	"os"
	"path"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/rs/zerolog/log"

	"code.ornl.gov/situ/mercury/common"
)

const (
	pcapWriterChanSize = 8192
)

func writePcap(snapshotLen int32, inCh chan *Message, done *sync.WaitGroup) (chan *Message, error) {
	outCh := make(chan *Message, pcapWriterChanSize)

	logger := log.With().Str("component", "pcap-writer").Logger()

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			close(outCh)
			done.Done()
		}()

		var pcapFilename string
		var pcapIdx byte
		var pcapFile *os.File
		var pcapWriter *pcapgo.Writer

		for msg := range inCh {

			switch msg.msgType {

			case msgTypeNewPcapFile:
				if pcapFile != nil {
					pcapFile.Close()
					logger.Debug().
						Str("file-name", pcapFilename).
						Msg("sending file closed message")
					outCh <- NewMessage(msgTypeFileClosed).
						Set(msgPayloadPcapFilename, pcapFilename).
						Set(msgPayloadPcapIdx, msg.Get(msgPayloadPcapIdx).(byte))
				}
				pcapBase := msg.Get(msgPayloadPcapPathBase).(string)
				pcapFilename = msg.Get(msgPayloadPcapFilename).(string)
				pcapIdx = msg.Get(msgPayloadPcapIdx).(byte)
				var err error
				f := fmt.Sprintf("%s_%d.%s", path.Join(pcapBase, pcapFilename), pcapIdx, common.PcapNameSuffix)
				pcapFile, err = os.Create(f)
				if err != nil {
					logger.Error().Str("file", f).Err(err).Msg("error opening file")
					return // unrecoverable
				}
				pcapWriter = pcapgo.NewWriter(pcapFile)
				err = pcapWriter.WriteFileHeader(uint32(snapshotLen), layers.LinkTypeEthernet)
				if err != nil {
					logger.Error().Str("file", f).Err(err).Msg("error writing file header")
					return // unrecoverable
				}

			case msgTypePacket:
				offset, err := pcapFile.Seek(0, 1)
				if err != nil {
					logger.Warn().Str("file", pcapFile.Name()).Err(err).Msg("error seeking in file, unable to write packet")
					continue
				}
				msg.Set(msgPayloadOffset, uint32(offset))
				msg.Set(msgPayloadPcapFilename, pcapFilename)
				packet := msg.Get(msgPayloadPacket).(gopacket.Packet)
				err = pcapWriter.WritePacket(packet.Metadata().CaptureInfo, packet.Data())
				if err != nil {
					logger.Warn().Str("file", pcapFile.Name()).Err(err).Msg("error writing packet to file, unable to write packet")
					continue
				}

			}

			outCh <- msg
		}
	}()

	return outCh, nil
}
