package common

import (
	"time"
)

const (
	// QueryOutputBinary is the command line option for binary (pcap) output.
	QueryOutputBinary = "binary"
	// QueryOutputText is the command line option for text output.
	QueryOutputText = "text"

	// QueryType options -- these are the fields that are indexed.
	QueryTypeIP    = "ip"
	QueryTypePort  = "port"
	QueryTypeMAC   = "mac"
	QueryTypeProto = "protocol"

	// DefaultLabel is used for storing the index and querying.
	DefaultLabel = "pcap"

	// SnapLen (or Snap Length, or snapshot length) is the amount of data for
	// each frame that is actually captured and stored.
	SnapLen int32 = 8192

	// FileTimeFormat is the format of the date for Pcap and index file names.
	// Pcap file names will have a numeric suffix at the end (e.g. `_0`).
	FileTimeFormat = "2006_01_02-15_04_05"

	// FilesPerEveryNMinutes is the number of minutes for each file.
	FilesPerEveryNMinutes = 1

	// MaxPcapFileTime sets the max size of a pcap file.
	MaxPcapFileTime = FilesPerEveryNMinutes * time.Minute

	// PcapNameSuffix defines the pcap file suffix.
	PcapNameSuffix = "pcap"

	// IndexNameSuffix defines the Badger index directory suffix.
	IndexNameSuffix = "idx"

	// GRPCMaxSize defines the maximum size of a message.
	GRPCMaxSize = 64 * 1024 * 1024
)

// GetFileBaseName returns the base file name given a start date.
func GetFileBaseName(d time.Time) string {
	return d.Format(FileTimeFormat)
}
