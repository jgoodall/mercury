package capture

import (
	"context"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/pcap"
	"github.com/rs/zerolog/log"
)

const (
	readIfChanSize = 8192
)

// readPacketsFromInterface reads packets from a network interface
// and sends them to the output channel, quitting when the passed in
// context.Context is canceled.
func readPacketsFromInterface(ctx context.Context, deviceName string, snapshotLen int32, promiscuous bool, timeout time.Duration) (chan *Message, error) {
	outCh := make(chan *Message, readIfChanSize)

	logger := log.With().Str("component", "interface-reader").Str("interface", deviceName).Int32("snapshot-length", snapshotLen).Bool("promiscuous", promiscuous).Logger()

	handle, err := pcap.OpenLive(deviceName, snapshotLen, promiscuous, timeout)
	if err != nil {
		return nil, err
	}
	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	go func() {
		logger.Info().Msg("started")

		defer func() {
			logger.Info().Msg("completed")
			handle.Close()
			close(outCh)
		}()

		for packet := range packetSource.Packets() {
			select {
			case outCh <- NewMessage(msgTypePacket).Set(msgPayloadPacket, packet):
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh, nil
}

// readPacketsFromFiles reads packets from a pcap file and sends them
// to the output channel, quitting when the passed in context.Context
// is canceled or the whole file has been read.
func readPacketsFromFiles(ctx context.Context, files []string, finished chan<- bool) (chan *Message, error) {
	outCh := make(chan *Message, readIfChanSize)

	logger := log.With().Str("component", "file-reader").Strs("files", files).Logger()

	logger.Info().Msg("started")
	var count uint64

	go func(files []string) {

		defer func() {
			logger.Info().Uint64("total-packets", count).Msg("completed")
			close(outCh)
			finished <- true
		}()

		for _, file := range files {
			logger.Debug().Str("file", file).Msg("starting reading file")
			handle, err := pcap.OpenOffline(file)
			if err != nil {
				logger.Error().Str("file", file).Err(err).Msg("unable to open pcap file for reading")
				continue
			}
			packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

			for packet := range packetSource.Packets() {
				select {
				case outCh <- NewMessage(msgTypePacket).Set(msgPayloadPacket, packet):
					count++
				case <-ctx.Done():
					return
				}
			}
			logger.Debug().Str("file", file).Msg("finished reading file")
			handle.Close()

		}
	}(files)

	return outCh, nil
}
