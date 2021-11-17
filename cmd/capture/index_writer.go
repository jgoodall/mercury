package capture

import (
	"fmt"
	"path"
	"runtime"
	"sync"

	"github.com/dgraph-io/badger/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"code.ornl.gov/situ/mercury/common"
	idx "code.ornl.gov/situ/mercury/index"
)

var (
	basePath string
)

func indexWrite(indexBasePath string, inCh chan *Message, done *sync.WaitGroup) error {
	basePath = indexBasePath
	logger := log.With().Str("component", "index-writer").Logger()

	// For badger.
	// https://dgraph.io/docs/badger/faq/#are-there-any-go-specific-settings-that-i-should-use
	runtime.GOMAXPROCS(128)

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			done.Done()
		}()

		for msg := range inCh {
			if msg.msgType == msgTypeMemoryIndex {
				idxName := fmt.Sprintf("%s.%s", msg.Get(msgPayloadMemoryIndexFile).(string), common.IndexNameSuffix)
				logger.Debug().Str("index-name", idxName).Msg("writing index")
				memIndex := msg.Get(msgPayloadMemoryIndex).(idx.MemIndex)
				err := writeIndexFile(idxName, memIndex, logger)
				if err != nil {
					logger.Error().Err(err).Msg("error writing index file")
				}
			}
		}
	}()

	return nil
}

func writeIndexFile(idxName string, memIndex idx.MemIndex, logger zerolog.Logger) (err error) {
	var db *badger.DB
	idxPath := path.Join(basePath, idxName)
	logger.Debug().Str("db", idxPath).Msg("opening badger DB")
	opts := badger.DefaultOptions(idxPath).WithLogger(&common.BadgerLogger{Logger: logger}).WithSyncWrites(false).WithKeepL0InMemory(true)
	db, err = badger.Open(opts)
	if err != nil {
		return err
	}
	defer db.Close()

	wb := db.NewWriteBatch()
	defer wb.Cancel()

	for _, v := range memIndex {
		logger.Debug().Str("key", v.K.String()).Msg("")
		kBytes, _ := v.K.MarshalBinary()
		vBytes, _ := v.V.MarshalBinary()
		err = wb.Set(kBytes, vBytes)
		if err != nil {
			return err
		}
	}

	err = wb.Flush()
	return
}
