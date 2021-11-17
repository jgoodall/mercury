package capture

import (
	"fmt"
	"net"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"code.ornl.gov/situ/mercury/common"
	idx "code.ornl.gov/situ/mercury/index"
)

const (
	idxOutChanSize = 8192
)

type indexMeta struct {
	index       idx.MemIndex
	openWriters int
}

var (
	indexCache map[string]*indexMeta
)

func index(inCh chan *Message, done *sync.WaitGroup) (chan *Message, error) {
	outCh := make(chan *Message, idxOutChanSize)

	logger := log.With().Str("component", "indexer").Logger()
	indexCache = make(map[string]*indexMeta)

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			defer close(outCh)
			err := flushAll(logger)
			if err != nil {
				logger.Error().Err(err).Msg("error flushing index file")
			}
			done.Done()
		}()

		for msg := range inCh {
			switch msg.msgType {

			case msgTypeFileClosed:
				filename := msg.Get(msgPayloadPcapFilename).(string)
				im := indexCache[filename]
				im.openWriters -= 1
				logger.Debug().
					Str("file-name", filename).
					Int("open-writers", im.openWriters).
					Msg("received close file msg")
				if im.openWriters == 0 {
					logger.Debug().
						Str("file-name", filename).
						Msg("flushing memory index")
					outCh <- NewMessage(msgTypeMemoryIndex).
						Set(msgPayloadMemoryIndex, im.index).
						Set(msgPayloadMemoryIndexFile, filename)
					delete(indexCache, filename)
				}

			case msgTypeNewPcapFile:
				newMemIndexFile := msg.Get(msgPayloadPcapFilename).(string)
				if _, contains := indexCache[newMemIndexFile]; !contains {
					logger.Debug().
						Str("file-name", newMemIndexFile).
						Int("open-writers", 1).
						Msg("initialized new memory index")
					indexCache[newMemIndexFile] = &indexMeta{
						index:       idx.NewMemIndex(),
						openWriters: 1,
					}
					logger.Debug().
						Str("file-name", newMemIndexFile).
						Int("open-writers", indexCache[newMemIndexFile].openWriters).
						Msg("memory index")
				} else {
					indexCache[newMemIndexFile].openWriters += 1
					logger.Debug().
						Str("file-name", newMemIndexFile).
						Int("open-writers", indexCache[newMemIndexFile].openWriters).
						Msg("memory index")
				}

			case msgTypePacket:
				pcapFilename := msg.Get(msgPayloadPcapFilename).(string)
				im := indexCache[pcapFilename]
				memIndex := im.index

				valueElem := idx.NewValueElement(msg.Get(msgPayloadPcapIdx).(byte), msg.Get(msgPayloadOffset).(uint32))

				proto := msg.Get(msgPayloadIPProto)
				if proto != nil {
					memIndex.Put(idx.NewProtoKey(proto.(uint8)), valueElem)
				}

				srcIP := msg.Get(msgPayloadSrcIP)
				if srcIP != nil {
					sip := srcIP.(net.IP)
					if sip.To4() != nil {
						memIndex.Put(idx.NewIPv4Key(sip.To4()), valueElem)
					} else if sip.To16() != nil {
						memIndex.Put(idx.NewIPv6Key(sip.To16()), valueElem)
					}
				}

				dstIP := msg.Get(msgPayloadDstIP)
				if dstIP != nil {
					dip := dstIP.(net.IP)
					if dip.To4() != nil {
						memIndex.Put(idx.NewIPv4Key(dip.To4()), valueElem)
					} else if dip.To16() != nil {
						memIndex.Put(idx.NewIPv6Key(dip.To16()), valueElem)
					}
				}

				srcPort := msg.Get(msgPayloadSrcPort)
				if srcPort != nil {
					memIndex.Put(idx.NewPortKey(srcPort.(uint16)), valueElem)
				}

				dstPort := msg.Get(msgPayloadDstPort)
				if dstPort != nil {
					memIndex.Put(idx.NewPortKey(dstPort.(uint16)), valueElem)
				}
			}
		}
	}()

	return outCh, nil
}

func flushAll(logger zerolog.Logger) error {
	logger.Debug().Msg("starting flushing indices")
	for filename, im := range indexCache {
		idxName := fmt.Sprintf("%s.%s", filename, common.IndexNameSuffix)
		im.openWriters = 0
		err := writeIndexFile(idxName, im.index, logger)
		if err != nil {
			return err
		}
		delete(indexCache, filename)
	}
	logger.Debug().Msg("finished flushing indices")
	return nil
}
