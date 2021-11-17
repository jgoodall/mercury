package capture

import (
	"math"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/rs/zerolog/log"

	"code.ornl.gov/situ/mercury/common"
)

const (
	schedulerChanSize = 8192
	maxPcapFileSize   = math.MaxUint32
)

// schedule listens on a message input channel and handles creating
// new pcap files.
func schedule(basePcapPath []string, inCh chan *Message, done *sync.WaitGroup) []chan *Message {
	outCh := make([]chan *Message, 0, len(basePcapPath))
	for i := 0; i < len(basePcapPath); i++ {
		outCh = append(outCh, make(chan *Message, schedulerChanSize))
	}

	logger := log.With().Str("component", "scheduler").Logger()

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			for _, c := range outCh {
				close(c)
			}
			done.Done()
		}()

		createNewFile := true
		createNewFileTime := time.Now()
		fileBytes := make([]uint64, len(outCh))
		for i := range fileBytes {
			fileBytes[i] = 24 // pcap header bytes
		}

		for msg := range inCh {
			// Find the smallest file size.
			var minFileBytes uint64 = math.MaxUint64
			minFileIdx := 0
			for i, v := range fileBytes {
				if v < minFileBytes {
					minFileBytes = v
					minFileIdx = i
				}
			}

			// Size the packet will be in the file once written.
			packetFileSize := uint64(msg.Get(msgPayloadPacket).(gopacket.Packet).Metadata().CaptureLength) + 16 // pcap packet header bytes
			if minFileBytes+packetFileSize >= maxPcapFileSize {
				createNewFile = true
			}
			if time.Since(createNewFileTime) >= common.MaxPcapFileTime {
				createNewFile = true
			}

			if createNewFile {
				for i, p := range basePcapPath {
					t := msg.Get(msgPayloadPacket).(gopacket.Packet).Metadata().Timestamp.UTC()
					timeStr := common.GetFileBaseName(t)
					logger.Debug().
						Str("directory-path", p).
						Str("file-base-name", timeStr).
						Int("file-index", i).
						Msg("sending create new file to writer")

					outCh[i] <- NewMessage(msgTypeNewPcapFile).
						Set(msgPayloadPcapPathBase, p).
						Set(msgPayloadPcapFilename, timeStr).
						Set(msgPayloadPcapIdx, byte(i))
				}
				for i := range fileBytes {
					fileBytes[i] = 24 // pcap header bytes
				}
				createNewFileTime = time.Now()
				createNewFile = false
			}

			msg.Set(msgPayloadPcapIdx, uint8(minFileIdx))
			outCh[minFileIdx] <- msg
			fileBytes[minFileIdx] += packetFileSize
		}

	}()

	return outCh
}
