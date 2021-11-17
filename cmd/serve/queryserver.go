package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v2"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"
	"github.com/rs/zerolog/log"

	v1 "code.ornl.gov/situ/mercury/api/v1"
	"code.ornl.gov/situ/mercury/common"
	"code.ornl.gov/situ/mercury/index"
)

// packetServiceServer is implementation of v1.QueryServiceServer proto interface
type packetServiceServer struct {
	indexBasePath string
	pcapPaths     []string
}

const (
	// apiVersion is version of API is provided by server
	apiVersion = "v1"
)

var (
	logger *common.BadgerLogger
)

func NewPacketQueryService(indexPath string, pcapPaths []string) v1.PacketServiceServer {
	logger = &common.BadgerLogger{Logger: log.Logger}
	return &packetServiceServer{
		indexBasePath: indexPath,
		pcapPaths:     pcapPaths,
	}
}

// QueryStream sends protobuf or text data based on request.
func (s *packetServiceServer) QueryStream(req *v1.QueryReq, stream v1.PacketService_QueryStreamServer) (err error) {
	label := req.Label
	if label == "" {
		label = common.DefaultLabel
	}
	indexPath := path.Join(s.indexBasePath, label)
	startTime, endTime := getTimes(req.StartTime, req.Duration)
	indices, err := getIndexPaths(indexPath, startTime, endTime)
	if err != nil {
		return fmt.Errorf("error getting index paths, perhaps label is not set correctly: %s", err)
	}
	if len(indices) == 0 {
		return fmt.Errorf("no indices within the time range %s - %s", startTime.Format(common.FileTimeFormat), endTime.Format(common.FileTimeFormat))
	}

	log.Info().
		Str("component", "query-server").
		Str("label", label).
		Time("start-time", startTime).
		Time("end-time", endTime).
		Str("index-path", indexPath).
		Strs("indices", indices).
		Str("query-type", req.QueryType.String()).
		Str("query-arg", req.Query).
		Msg("executing index query")

	// Loop through the indices and check for the search params.
	for _, indexName := range indices {
		dbPath := path.Join(indexPath, indexName)
		log.Info().Str("db", dbPath).Msg("opening index database")
		db, err := badger.Open(badger.DefaultOptions(dbPath).WithReadOnly(true).WithLogger(logger))
		if err != nil {
			return fmt.Errorf("error opening db: %s", err)
		}
		// Query the index for the requested IP.
		err = db.View(func(txn *badger.Txn) error {
			key, err := createKey(req.QueryType, req.Query)
			if err != nil {
				return err
			}
			item, err := txn.Get(key)
			if err != nil {
				if err == badger.ErrKeyNotFound {
					return nil
				}
				return fmt.Errorf("error getting key '%s': %s", req.Query, err)
			}

			var v []byte
			err = item.Value(func(val []byte) error {
				v = append([]byte{}, val...)
				return nil
			})
			if err != nil {
				return fmt.Errorf("error getting value: %s", err)
			}
			var values index.Value
			err = values.UnmarshalBinary(v)
			if err != nil {
				return fmt.Errorf("error unmarshalling values: %s", err)
			}

			// Loop through the pcap file path/offset pairs.
			for _, val := range values {
				pcapDir := s.pcapPaths[val.PathIdx]
				n := strings.Replace(indexName, "."+common.IndexNameSuffix, "", 1)
				pcapFileName := fmt.Sprintf("%s_%d.%s", n, val.PathIdx, common.PcapNameSuffix)
				pcapFilePath := path.Join(pcapDir, pcapFileName)
				offset := val.Offset

				file, err := os.Open(pcapFilePath)
				if err != nil {
					return fmt.Errorf("error opening file %s: %s", pcapFilePath, err)
				}

				ts, packetLen, err := readHeaderFromFile(file, int64(offset))
				if err != nil {
					return fmt.Errorf("error reading packet header from file %s: %s", pcapFilePath, err)
				}

				packet, err := readPacketFromFile(file, int64(offset+16), packetLen, ts)
				if err != nil {
					return fmt.Errorf("error reading packet data from file %s: %s", pcapFilePath, err)
				}
				protoTs, err := ptypes.TimestampProto(ts)
				if err != nil {
					return fmt.Errorf("error converting timestamp %s for protobuf: %s", ts.String(), err)
				}

				resp := createResp(protoTs, packetLen, packet, req.ShowAll, req.Encode)
				err = stream.Send(resp)
				if err != nil {
					return fmt.Errorf("error sending response: %s", err)
				}

				file.Close()
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("error querying index %s: %s", dbPath, err)
		}

		db.Close()
	}

	return nil
}

// QueryBinaryStream sends binary packet data based on request.
func (s *packetServiceServer) QueryBinaryStream(req *v1.QueryReq, stream v1.PacketService_QueryBinaryStreamServer) (err error) {
	label := req.Label
	if label == "" {
		label = common.DefaultLabel
	}
	indexPath := path.Join(s.indexBasePath, label)
	startTime, endTime := getTimes(req.StartTime, req.Duration)
	indices, err := getIndexPaths(indexPath, startTime, endTime)
	if err != nil {
		return fmt.Errorf("error getting index paths, perhaps label is not set correctly: %s", err)
	}
	if len(indices) == 0 {
		return fmt.Errorf("no indices within the time range %s - %s", startTime.Format(common.FileTimeFormat), endTime.Format(common.FileTimeFormat))
	}

	log.Info().
		Str("component", "query-server").
		Str("label", label).
		Time("start-time", startTime).
		Time("end-time", endTime).
		Str("index-path", indexPath).
		Strs("indices", indices).
		Str("query-type", req.QueryType.String()).
		Str("query-arg", req.Query).
		Msg("executing index query")

	var buf = new(bytes.Buffer)
	output := pcapgo.NewWriter(buf)
	err = output.WriteFileHeader(uint32(common.SnapLen), layers.LinkTypeEthernet)
	if err != nil {
		return fmt.Errorf("error writing pcap file header: %s", err)
	}

	err = stream.Send(&v1.QueryBinaryResp{Binary: buf.Bytes()})
	if err != nil {
		return fmt.Errorf("error sending response: %s", err)
	}

	// Loop through the indices and check for the search params.
	for _, indexName := range indices {
		dbPath := path.Join(indexPath, indexName)
		log.Info().Str("index", dbPath).Msg("opening index db")
		db, err := badger.Open(badger.DefaultOptions(dbPath).WithReadOnly(true).WithLogger(logger))
		if err != nil {
			return fmt.Errorf("error opening db: %s", err)
		}
		// Query the index for the requested IP.
		err = db.View(func(txn *badger.Txn) error {
			key, err := createKey(req.QueryType, req.Query)
			if err != nil {
				return err
			}
			item, err := txn.Get(key)
			if err != nil {
				if err == badger.ErrKeyNotFound {
					return nil
				}
				return fmt.Errorf("error getting key '%s': %s", req.Query, err)
			}
			var v []byte
			err = item.Value(func(val []byte) error {
				v = append([]byte{}, val...)
				return nil
			})
			if err != nil {
				return fmt.Errorf("error getting value: %s", err)
			}
			var values index.Value
			err = values.UnmarshalBinary(v)
			if err != nil {
				return fmt.Errorf("error unmarshalling values: %s", err)
			}

			// Loop through the pcap file path/offset pairs.
			for _, val := range values {
				buf.Reset()
				pcapDir := s.pcapPaths[val.PathIdx]
				n := strings.Replace(indexName, "."+common.IndexNameSuffix, "", 1)
				pcapFileName := fmt.Sprintf("%s_%d.%s", n, val.PathIdx, common.PcapNameSuffix)
				pcapFilePath := path.Join(pcapDir, pcapFileName)
				offset := val.Offset

				file, err := os.Open(pcapFilePath)
				if err != nil {
					return fmt.Errorf("error opening file %s: %s", pcapFilePath, err)
				}

				timestamp, packetLen, err := readHeaderFromFile(file, int64(offset))
				if err != nil {
					return fmt.Errorf("error reading packet header from file %s: %s", pcapFilePath, err)
				}

				packet, err := readPacketFromFile(file, int64(offset+16), packetLen, timestamp)
				if err != nil {
					return fmt.Errorf("error reading packet data from file %s: %s", pcapFilePath, err)
				}

				err = output.WritePacket(packet.Metadata().CaptureInfo, packet.Data())
				if err != nil {
					return fmt.Errorf("error writing packet to buffer: %s", err)
				}

				err = stream.Send(&v1.QueryBinaryResp{Binary: buf.Bytes()})
				if err != nil {
					return fmt.Errorf("error sending response: %s", err)
				}

				file.Close()
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("error querying index %s: %s", dbPath, err)
		}

		db.Close()
	}

	return nil
}

func getTimes(s *timestamp.Timestamp, d *duration.Duration) (start, end time.Time) {
	start = time.Unix(s.GetSeconds(), int64(s.GetNanos()))
	nanosecDur := d.GetSeconds()*1000000000 + int64(d.GetNanos())
	duration, _ := time.ParseDuration(fmt.Sprintf("%dns", nanosecDur))
	end = start.Add(duration)
	return
}

// Figure out the index paths. ioutil.ReadDir() returns files sorted
// by filename, so the directories will be in timestamp order.
func getIndexPaths(indexDir string, start, end time.Time) ([]string, error) {
	indices := make([]string, 0)
	dirs, err := ioutil.ReadDir(indexDir)
	if err != nil {
		return nil, fmt.Errorf("unable to read directory %s:%s", indexDir, err)
	}

	for _, dir := range dirs {
		if dir.IsDir() && strings.HasSuffix(dir.Name(), common.IndexNameSuffix) {
			ts := strings.TrimSuffix(dir.Name(), "."+common.IndexNameSuffix)
			t, err := time.Parse(common.FileTimeFormat, ts)
			if err != nil {
				return nil, fmt.Errorf("unable to parse time from directory %s:%s", indexDir, err)
			}

			if t.After(start) && t.Before(end) {
				indices = append(indices, dir.Name())
			}
		}
	}
	return indices, nil
}

func createKey(queryType v1.QueryType, queryArg string) (key []byte, err error) {
	var k *index.Key
	switch queryType {
	case v1.QueryType_ip:
		ip := net.ParseIP(queryArg)
		if ip == nil {
			return nil, fmt.Errorf("error parsing ip %s: %s", queryArg, err)
		}
		// Check if it is IPv4 or IPv6.
		if ip.To4() != nil {
			k = index.NewIPv4Key(ip)
		} else if ip.To16() != nil {
			k = index.NewIPv6Key(ip)
		} else {
			return nil, fmt.Errorf("error creating ip key for %s", queryArg)
		}
	case v1.QueryType_port:
		port, err := strconv.ParseUint(queryArg, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("error parsing port %s: %s", queryArg, err)
		}
		k = index.NewPortKey(uint16(port))
	case v1.QueryType_protocol:
		var proto uint8
		switch strings.ToLower(queryArg) {
		case "tcp":
			proto = 6
		case "udp":
			proto = 17
		case "icmp":
			proto = 1
		case "icmp6":
			proto = 58
		default:
			return nil, fmt.Errorf("query protocol %s is not supported", queryArg)
		}
		k = index.NewProtoKey(proto)
	case v1.QueryType_mac:
		mac, err := net.ParseMAC(queryArg)
		if err != nil {
			return nil, fmt.Errorf("error parsing MAC %s: %s", queryArg, err)
		}
		k = index.NewMACKey(mac)
	default:
		return nil, fmt.Errorf("query type %s is not supported", queryType)
	}
	key, err = k.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error creating key: %s", err)
	}
	return key, nil
}

func readHeaderFromFile(file *os.File, offset int64) (time.Time, int64, error) {
	packetHeader := make([]byte, 16)
	_, err := file.ReadAt(packetHeader, int64(offset))
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("error reading packet length from file %s: %s", file.Name(), err)
	}
	sec := binary.LittleEndian.Uint32(packetHeader[0:4])
	microsec := binary.LittleEndian.Uint32(packetHeader[4:8])
	timestamp := time.Unix(int64(sec), int64(microsec*1000))
	packetLen := binary.LittleEndian.Uint32(packetHeader[8:12])
	return timestamp, int64(packetLen), nil
}

func readPacketFromFile(file *os.File, offset, packetLen int64, ts time.Time) (gopacket.Packet, error) {
	packetData := make([]byte, packetLen)
	_, err := file.ReadAt(packetData, offset)
	if err != nil {
		return nil, fmt.Errorf("error reading packet data from file %s: %s", file.Name(), err)
	}
	p := gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy)
	p.Metadata().Timestamp = ts
	p.Metadata().Length = int(packetLen)
	p.Metadata().CaptureLength = int(packetLen)
	return p, nil
}

func createResp(ts *timestamp.Timestamp, packetLen int64, packet gopacket.Packet, showAll, base64Enc bool) (resp *v1.QueryResp) {

	// Base 64 encode the text response
	var t bytes.Buffer
	t.WriteString(packet.Dump())

	// Only show the full text response if showAll is true.
	var text string
	if showAll {
		if base64Enc {
			text = base64.StdEncoding.EncodeToString(t.Bytes())
		} else {
			text = packet.String()
		}
	}

	resp = &v1.QueryResp{
		Timestamp: ts,
		Length:    packetLen,
		Text:      text,
		// Data:      packet.Data(),
	}

	vers, srcMAC, dstMAC, srcIP, dstIP, srcPort, dstPort, _, proto := common.ParsePacket(packet)
	resp.SrcMAC = srcMAC.String()
	resp.DstMAC = dstMAC.String()
	resp.SrcIP = srcIP.String()
	resp.DstIP = dstIP.String()
	resp.SrcPort = uint32(srcPort)
	resp.SrcPortStr = strconv.FormatUint(uint64(srcPort), 10)
	resp.DstPort = uint32(dstPort)
	resp.DstPortStr = strconv.FormatUint(uint64(dstPort), 10)
	resp.Proto = proto
	if vers == 6 {
		resp.Ipv6 = true
	}

	return
}
