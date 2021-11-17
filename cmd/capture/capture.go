package capture

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"code.ornl.gov/situ/mercury/common"
)

type CaptureServer struct {
	// ctx should be a cancelable context that will trigger a shutdown.
	ctx context.Context
	// done is a channel used to signal that shutdown has finished.
	done chan<- struct{}
	// wg is a waitgroup used to signal that all of the processes have finished.
	wg sync.WaitGroup

	// readFromFile equals true if reading from file, false if from nic.
	readFromFile bool

	nic         string
	promiscuous bool

	files []string

	indexPath string
	pcapPaths []string
}

// start is used to calculate the duration at the end.
var start time.Time

const (
	timeout       time.Duration = 30 * time.Second
	muxBufferSize               = 8192
)

func NewCaptureServerInterface(nic string, promiscuous bool, indexPath string, pcapPaths []string) *CaptureServer {
	return &CaptureServer{
		readFromFile: false,
		nic:          nic,
		promiscuous:  promiscuous,
		indexPath:    indexPath,
		pcapPaths:    pcapPaths,
	}
}

func NewCaptureServerFile(files []string, indexPath string, pcapPaths []string) *CaptureServer {
	return &CaptureServer{
		readFromFile: true,
		files:        files,
		indexPath:    indexPath,
		pcapPaths:    pcapPaths,
	}
}

// Run starts the capture server. For opening a file it should read the file and return.
// For reading from a network interface, it will run until interupt is caught by main and
// passed here as part of the ctx.
// Each component below is run as a goroutine that logs its own errors and returns when
// its input channel closes.
func (s *CaptureServer) Run(ctx context.Context, done chan<- struct{}) error {
	start = time.Now()

	s.ctx = ctx
	s.done = done

	// Open from NIC or file
	var readOutChan chan *Message
	var err error

	// Interface/File reader does not have an input channel, cancel
	// with context. All others cancel by closing the channel.
	readFinished := make(chan bool)
	if !s.readFromFile {
		log.Info().
			Str("index-path", s.indexPath).
			Strs("pcap-paths", s.pcapPaths).
			Msg("starting capture from interface")
		readOutChan, err = readPacketsFromInterface(ctx, s.nic, common.SnapLen, s.promiscuous, timeout)
		if err != nil {
			return err
		}
	} else {
		log.Info().
			Str("index-path", s.indexPath).
			Strs("pcap-paths", s.pcapPaths).
			Msg("starting capture from file(s)")
		readOutChan, err = readPacketsFromFiles(ctx, s.files, readFinished)
		if err != nil {
			return err
		}
	}

	// Scheduler
	schedulerOutChans := schedule(s.pcapPaths, readOutChan, &s.wg)
	s.wg.Add(1)

	// PCAP writer
	var writerOutChans []chan *Message
	for _, schedChan := range schedulerOutChans {
		writerOutChan, err := writePcap(common.SnapLen, schedChan, &s.wg)
		if err != nil {
			return err
		}
		writerOutChans = append(writerOutChans, writerOutChan)
	}
	s.wg.Add(len(writerOutChans))

	// Mux
	muxOutChan := muxMessageChans(muxBufferSize, &s.wg, writerOutChans...)
	s.wg.Add(1)

	// IP extractor
	extractorOutChan, err := extractPacket(muxOutChan, &s.wg)
	if err != nil {
		return err
	}
	s.wg.Add(1)

	// Index
	indexerOutChan, err := index(extractorOutChan, &s.wg)
	if err != nil {
		return err
	}
	s.wg.Add(1)

	err = indexWrite(s.indexPath, indexerOutChan, &s.wg)
	if err != nil {
		return err
	}
	s.wg.Add(1)

	// Wait for finished...
	select {
	case <-s.ctx.Done():
		log.Debug().Msg("context done")
		s.Stop()
		return nil
	case <-readFinished:
		log.Debug().Msg("file read completed")
		s.Stop()
		return nil
	}

}

func (s *CaptureServer) Stop() {
	if s.readFromFile {
		log.Debug().
			Str("index-path", s.indexPath).
			Strs("pcap-paths", s.pcapPaths).
			Strs("files", s.files).
			Msg("stopping capture from file")
	} else {
		log.Debug().
			Str("index-path", s.indexPath).
			Strs("pcap-paths", s.pcapPaths).
			Str("interface", s.nic).
			Bool("promiscuous", s.promiscuous).
			Msg("stopping capture from interface")
	}

	// Wait for all goroutines to finish.
	s.wg.Wait()

	if s.readFromFile {
		log.Info().Str("duration", time.Since(start).Round(time.Millisecond).String()).Strs("files", s.files).Msg("finished capture from file")
	} else {
		log.Info().Str("duration", time.Since(start).Round(time.Millisecond).String()).Str("interface", s.nic).Msg("finished capture from interface")
	}

	// Signal back to main that everything has completed.
	s.done <- struct{}{}
}
