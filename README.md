# Mercury Packet Archiver

Reads packet from a network interface or file(s), archives, and indexes them.

## Usage

This assumes you are running everything on localhost. If the server is going to be on another host, you will likely need to generate certificates; see **Certificates** below. These examples use the test data downloaded from [NETRESEC](https://www.netresec.com/?page=PCAP4SICS), which can be found in the `testdata` directory in git (be sure to `gunzip testdata/4SICS-GeekLounge-151020.pcap.gz` first).

1. Load some data:

    ```sh
    ./bin/mercury-darwin-amd64 capture -l info --file ./testdata/4SICS-GeekLounge-151020.pcap
    ```

*Note*: To read from multiple files, just use multiple `--file <filename>` arguments, but all of the files should be from the same day in order to be indexed properly.

1. Start the query server:

    ```sh
    ./bin/mercury-darwin-amd64 serve -l info --cert ./certs/localhost.crt --key ./certs/localhost.key --server-name localhost
    ```

1. Execute queries with the client:

    ```sh
    ./bin/mercury-darwin-amd64 query --ca-path ./certs/AAI.crt --server-name localhost --show-all --start 2015-10-20 --duration 24h --query-type port 57711
    ./bin/mercury-darwin-amd64 query -c ./certs/AAI.crt --server-name localhost -a -s 2015-10-20 -d 24h -q ip 21.2.2.2
    ./bin/mercury-darwin-amd64 query -c ./certs/AAI.crt --server-name localhost -s 2015-10-20 -d 24h -q protocol UDP
    ```

If `query` command is run without `--show-all` the output is very similar to using `tcpdump -q -nn`; using `show-all` shows all of the details of each of four layers corresponding to the 4 layers of the TCP/IP layering scheme, roughly anagalous to layers 2, 3, 4, and 7 of the OSI model; for example, IPv4 and IPv6 are both considered Network Layer, while TCP and UDP are both Transport Layer.

1. Save output to a pcap file:

    ```sh
    ./bin/mercury-darwin-amd64 query -l info --ca-path ./certs/AAI.crt --server-name localhost --start 2015-10-20 --duration 24h --binary --query-type ip 192.168.88.61 > output.pcap
    tcpdump -nn -r output.pcap
    ```

1. Redirect output to tshark:

    ```sh
    ./bin/mercury-darwin-amd64 query -l info --ca-path ./certs/AAI.crt --server-name localhost --start 2015-10-20 --duration 24h --binary --query-type ip 192.168.88.61 | tshark -r -
    ```

1. Run a query with certificate chain and host name verficiation disabled (susceptible to a machine-in-the-middle attack) by not including the `--ca-path` and `--server-name` options:

    ```sh
    ./bin/mercury-darwin-amd64 query -l info --start 2015-10-20 --duration 24h --query-type ip 192.168.88.61
    ```

1. Run an http query with curl:

    ```sh
    curl "localhost:8123/v1/q?startTime=2015-10-20T00:00:00Z&duration=24h&queryType=ip&query=192.168.88.61"
    ```

1. Run an http query with curl, getting response base64 encoded:

    ```sh
    curl "localhost:8123/v1/q?startTime=2015-10-20T00:00:00Z&duration=24h&queryType=ip&query=192.168.88.61&encode=true"
    ```

To capture data from a network interface, run something like the following: `./bin/mercury-darwin-amd64 capture -l info -i en0`. See `./bin/mercury-darwin-amd64 capture --help` for more information. To spread the captured pcap data across multiple directories (potentially on different disks), use multiple `--pcap-path=<di>` options; this can be used to improve performance when there are multiple drives.

## Certificates

To generate certificates, follow the instructions below using [certstrap](https://github.com/square/certstrap):

1. Initialize a new certificate authority:

    ```sh
    CN=$(whoami) # or AAI-Group or division name
    certstrap --depot-path certs init --common-name $CN --expires "10 years" --organization ORNL --country US --province TN --passphrase ""
    ```

1. Request a certificate, including keypair:

    ```sh
    CN=$(hostname) # or localhost or other FQDN
    certstrap --depot-path certs request-cert --common-name $CN --domain $CN --organization ORNL --country US --province TN --passphrase ""
    ```

1. Sign certificate request of host and generate the certificate:

    ```sh
    CN=$(hostname) # or localhost or other FQDN
    certstrap --depot-path certs sign $CN --CA AAI --expires "10 years" --passphrase ""
    ```

## Development

1. Install [go](https://golang.org/dl/) and set the `GOPATH` (this is where the go modules will be installed):  `export GOPATH=$HOME/go`.

2. Install [mage](https://magefile.org/):

    ```sh
    cd /tmp
    git clone https://github.com/magefile/mage
    cd mage
    go run bootstrap.go
    ```

3. Install libpcap. On Ubuntu: `sudo apt install libpcap-dev`. On Mac: `brew install libpcap`.

4. Install [protocol buffer compiler](https://github.com/protocolbuffers/protobuf#protocol-compiler-installation). On Ubuntu: `sudo apt install protobuf-compiler`. On Mac: `brew install protobuf`.

5. Build

    ```sh
    cd /path/to/mercury
    mage build
    ```

## Design

Each packet is processed through a several stage pipeline.  The **Interface Reader** stage reads packets from an interface and passes them to the **Scheduler**.  The **Scheduler** assigns a path and filename, then passes the packet along to one of several **PCAP Writers**, balancing the byte count to each writer, and requesting new files as needed.  The **PCAP writers** then passes the packet to the **Packet Data Extractor**, which extract the protocol, addresses, and ports information from the packet, then passes the packet to the **Indexer**.  The **Indexer** creates an in memory index for the current set of files open in the **PCAP Writers** using the data from the **Packet Data Extractor**, and once the **PCAP Writers** close the file, passes the in memory index on to the **Index Writer**.  The **Index Writer** writes the in memory index to a [Badger DB](https://github.com/dgraph-io/badger).

The following is a list of the processing stages in the order that packets move through them.  Information moves through the stages encapsulated in a `Message`

### Interface Reader

Reads packets from a system interface and inserts them into the pipeline.

#### Output Messages

```
| Type               | Payload                  | Type                   | Description                            |
|--------------------|--------------------------|------------------------|----------------------------------------|
| msgTypePacket      | > msgPayloadPacket       | gopacket.Packet        | Packet metadata and bytes              |
```

### Scheduler

Balances the load by byte count to multiple PCAP writers.  Creates PCAP and index file base names based on date.  Decides when a new file needs to be started based on file size or time limit.

#### Output Messages

```
| Type               | Payload                  | Type                   | Description                            |
|--------------------|--------------------------|------------------------|----------------------------------------|
| msgTypeNewPcapFile | > msgPayloadPcapPathBase | string                 | Directory to store PCAP file           |
|                    | > msgPayloadPcapFilename | string                 | Base file name for PCAP file           |
|                    | > msgPayloadPcapIdx      | byte                   | Uniquely indicates PCAP writer         |
|                    |                          |                        |                                        |
| msgTypePacket      | msgPayloadPacket         | gopacket.Packet        | Packet metadata and bytes              |
|                    | > msgPayloadPcapIdx      | byte                   | Uniquely indicates PCAP writer         |      
```

### PCAP Writer

Writes PCAP data to a file.  There will be multiple instances of this stage.  Creates new PCAP files in response to the scheduler requests.  Notifies subsequent stages when a PCAP file has been closed.

#### Output Messages

```
| Type               | Payload                  | Type                   | Description                            |
|--------------------|--------------------------|------------------------|----------------------------------------|
| msgTypeFileClosed  | > msgPayloadPcapFilename | string                 | Base file name for PCAP file           |
|                    | > msgPayloadPcapIdx      | byte                   | Uniquely indicates PCAP writer         |
|                    |                          |                        |                                        |
| msgTypeNewPcapFile | msgPayloadPcapPathBase   | string                 | Directory to store PCAP file           |
|                    | msgPayloadPcapFilename   | string                 | Base file name for PCAP file           |
|                    | msgPayloadPcapIdx        | byte                   | Uniquely indicates PCAP writer         |
|                    |                          |                        |                                        |
| msgTypePacket      | msgPayloadPacket         | gopacket.Packet        | Packet metadata and bytes              |
|                    | msgPayloadPcapIdx        | byte                   | Uniquely indicates PCAP writer         |      
|                    | > msgPayloadOffset       | uint32                 | Offset in the PCAP file for packet     |
|                    | > msgPayloadPcapFilename | string                 | Base file name for PCAP file           |         
```

### Packet Data Extractor

Extracts the protocol, source IP, source port, destination IP, and destination port from the packet.

#### Output Messages

```
| Type               | Payload                  | Type                   | Description                            |
|--------------------|--------------------------|------------------------|----------------------------------------|
| msgTypeFileClosed  | msgPayloadPcapFilename   | string                 | Base file name for PCAP file           |
|                    | msgPayloadPcapIdx        | byte                   | Uniquely indicates PCAP writer         |
|                    |                          |                        |                                        |
| msgTypeNewPcapFile | msgPayloadPcapPathBase   | string                 | Directory to store PCAP file           |
|                    | msgPayloadPcapFilename   | string                 | Base file name for PCAP file           |
|                    | msgPayloadPcapIdx        | byte                   | Uniquely indicates PCAP writer         |
|                    |                          |                        |                                        |
| msgTypePacket      | msgPayloadPacket         | gopacket.Packet        | Packet metadata and bytes              |
|                    | msgPayloadPcapIdx        | byte                   | Uniquely indicates PCAP writer         |      
|                    | msgPayloadOffset         | uint32                 | Offset in the PCAP file for packet     |
|                    | msgPayloadPcapFilename   | string                 | Base file name for PCAP file           |
|                    | > msgPayloadSrcMAC       | net.HardwareAddr       | Source MAC address                     |
|                    | > msgPayloadDstMAC       | net.HardwareAddr       | Destination MAC addre                  |
|                    | > msgPayloadIPProto      | uint8                  | Packet protocol                        |
|                    | > msgPayloadSrcIP        | net.IP                 | Source IP address                      |
|                    | > msgPayloadSrcPort      | uint16                 | Source port                            |
|                    | > msgPayloadDstIP        | net.IP                 | Destination IP address                 |
|                    | > msgPaylodDstPort       | uint16                 | Destination Port                       |
```

### Indexer

Caches index data in memory.  When a `mstTypeFileClosed` is received from all of the PCAP writers, the in memory index is passed on to the index writer stage.

#### Output Messages

```
| Type               | Payload                     | Type                   | Description                            |
|--------------------|-----------------------------|------------------------|----------------------------------------|
| msgTypeMemoryIndex | > msgPayloadMemoryIndex     | MemIndex               | In memory index                        |
|                    | > msgPayloadMemoryIndexFile | string                 | Base file name for the index           |
```

### Index Writer

Receives a in memory access data structure from the indexer stage and writes the data out to a Badger DB.  The Badger DB key and value entries are in the following format:

#### Key

```
| Record Type (byte) | Data (1 to 16 bytes) | Length (bytes) |
|--------------------|----------------------|----------------|
| 0                  | MAC Address          | 6              |
| 1                  | Protocol             | 1              |
| 2                  | IPv4 Address         | 4              |
| 3                  | IPv6 Address         | 16             |    
| 4                  | Port                 | 2              |
```

#### Value

```
| PCAP Idx (1 byte) | PCAP file offset (4 bytes) |
|-------------------|----------------------------|
```