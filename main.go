package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"

	"github.com/alecthomas/kingpin"
	"github.com/google/gops/agent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	v1 "code.ornl.gov/situ/mercury/api/v1"
	"code.ornl.gov/situ/mercury/cmd/capture"
	"code.ornl.gov/situ/mercury/cmd/info"
	"code.ornl.gov/situ/mercury/cmd/query"
	"code.ornl.gov/situ/mercury/cmd/serve"
	"code.ornl.gov/situ/mercury/common"
)

// Injected by build.
// Version, GitSHA and BuildTime are injected in Makefile.
var (
	Version   string
	GoVersion string
	GitSHA    string
	BuildTime string
)

const (
	defaultGRPCPort = 7123
	defaultHTTPPort = 8123
)

var (
	// These are set at init() and prepare protobuf enums for kingpin.
	queryTypes []string

	// Set up the command line options.
	app          = kingpin.New("mercury", "A packet capture, indexing and retrieval tool.")
	logLevel     = app.Flag("log-level", "Log level to print to stderr.").Short('l').Default("warn").Enum("debug", "info", "warn", "error")
	logJSON      = app.Flag("log-json", "Structured  JSON logging.").Bool()
	indexDirPath = app.Flag("index-path", "Directory to store the index data.").Default("./_index").String()
	pcapDirPaths = app.Flag("pcap-path", "List of directories to store the packet capture data.").Default("./_data").Strings()

	// Capture command and flags.
	captureCmd         = app.Command("capture", "Capture and index pcap data.").Alias("c")
	captureLabel       = captureCmd.Flag("label", "Label to assign for these packet captures.").Default(common.DefaultLabel).String()
	captureFiles       = captureCmd.Flag("file", "Pcap file(s) to ingest.").Short('f').ExistingFiles()
	captureInterface   = captureCmd.Flag("interface", "Listen on interface.").Short('i').String()
	capturePromiscuous = captureCmd.Flag("promiscuous", "Capture in promiscuous mode (must be root), use --no-promiscuous to turn off.").Default("true").Bool()
	captureGops        = captureCmd.Flag("gops", "Use gops to start the diagnostics agent.").Default("false").Bool()

	// Serve command and flags.
	serveCmd            = app.Command("serve", "Start the server that will listen for queries.").Alias("s")
	serveCert           = serveCmd.Flag("cert", "The server certificate for TLS.").Short('t').String()
	serveKey            = serveCmd.Flag("key", "The server key for TLS.").Short('k').String()
	serveGRPCPort       = serveCmd.Flag("port", "The gRPC port to listen for queries on.").Short('p').Default(fmt.Sprintf("%d", defaultGRPCPort)).Uint16()
	serveHTTPPort       = serveCmd.Flag("http-port", "The HTTP port to listen for queries on.").Default(fmt.Sprintf("%d", defaultHTTPPort)).Uint16()
	serveHTTPServerName = serveCmd.Flag("server-name", "The optional server name override for HTTP gateway TLS if the certificate hostname is different than the server hostname.").String()

	// Query command and flags.
	queryCmd        = app.Command("query", "Query indexed pcap data.").Alias("q")
	queryCA         = queryCmd.Flag("ca-path", "The certificate authority for TLS.").Short('c').String()
	queryServerName = queryCmd.Flag("server-name", "The optional server name override TLS if the certificate is different than the server-addr.").String()
	queryGRPCAddr   = queryCmd.Flag("server-addr", "TCP address of the gRPC server to query.").Default(fmt.Sprintf("localhost:%d", defaultGRPCPort)).TCP()
	queryBinOut     = queryCmd.Flag("binary", "Output binary pcap to stdout (for redirecting to a pcap file or another command (e.g. tshark or tcpdump).").Short('b').Default("false").Bool()
	queryShowAll    = queryCmd.Flag("show-all", "Show the full packet information, not just the summary.").Short('a').Default("false").Bool()
	queryLabel      = queryCmd.Flag("label", "Label to filter packet captures.").Default(common.DefaultLabel).String()
	queryStart      = queryCmd.Flag("start", "Filter to only packets after this start time (format: "+query.ShortQueryTimeFormat+" or "+query.LongQueryTimeFormat+")").Required().Short('s').String()
	queryDuration   = queryCmd.Flag("duration", "Filter to only packets between start time and this duration. Valid time units are 'ms', 's', 'm', 'h'.").Short('d').Default("15m").Duration()
	queryArg        = queryCmd.Arg("query", "The query to search the packet index for (e.g. '1.2.3.4' or '443' or 'tcp').").Required().String()

	// Info command and flags.
	infoCmd  = app.Command("info", "Get information about indexed pcap data.").Alias("i")
	infoKeys = infoCmd.Flag("show-keys", "Show all the unique keys in the database, sorted by type.").Short('k').Default("false").Bool()
)

// During initialization set up Enum flags from protobuf spec.
func init() {
	queryTypes = make([]string, len(v1.QueryType_value))
	i := 0
	for k := range v1.QueryType_value {
		queryTypes[i] = k
		i++
	}
}

func main() {
	// Set up the application.
	vers := fmt.Sprintf("Version:\t%s\nGit SHA:\t%s\nGo Version:\t%s\nBuild Time:\t%s\n", Version, GitSHA, GoVersion, BuildTime)
	app.Version(vers)
	app.Author("ORNL")
	app.DefaultEnvars() // Default environment variable is: MERCURY_FLAG_NAME
	app.HelpFlag.Short('h')

	// Enum flags that are built during init().
	queryTypeHelp := fmt.Sprintf("The type of query to search the packet index for: %v", queryTypes)
	queryType := queryCmd.Flag("query-type", queryTypeHelp).Short('q').Required().Enum(queryTypes...)

	app.PreAction(func(c *kingpin.ParseContext) error {
		zerolog.SetGlobalLevel(zerolog.WarnLevel) // default
		if *logLevel == "debug" {
			zerolog.SetGlobalLevel(zerolog.DebugLevel)
		} else if *logLevel == "info" {
			zerolog.SetGlobalLevel(zerolog.InfoLevel)
		} else if *logLevel == "error" {
			zerolog.SetGlobalLevel(zerolog.ErrorLevel)
		}
		if !*logJSON {
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		}
		return nil
	})

	// Main context for canceling on interrupt.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to report when cancel is completed.
	done := make(chan struct{})
	go func() {
		<-done
	}()

	// If server has started, listen for ^C and stop gracefully.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	go func() {
		sig := <-sigs
		log.Debug().Str("signal", sig.String()).Msg("received signal")
		agent.Close()
		cancel()
		<-done
	}()

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {

	case captureCmd.FullCommand():
		if len(*captureFiles) == 0 && *captureInterface == "" {
			log.Fatal().Msg("please specify a pcap file to read or an interface to listen on")
		}
		if *captureGops {
			if err := agent.Listen(agent.Options{}); err != nil {
				log.Fatal().Err(err).Msg("unable to start gops agent")
			}
		}
		indexPath := path.Join(*indexDirPath, *captureLabel)
		err := setupDirs(indexPath, *pcapDirPaths)
		if err != nil {
			log.Fatal().Err(err).Msg("unable to setup directories")
		}
		var server *capture.CaptureServer
		if len(*captureFiles) > 0 {
			server = capture.NewCaptureServerFile(*captureFiles, indexPath, *pcapDirPaths)
		} else {
			server = capture.NewCaptureServerInterface(*captureInterface, *capturePromiscuous, indexPath, *pcapDirPaths)
		}
		kingpin.FatalIfError(server.Run(ctx, done), "Starting capture failed")

	// Serve pcap data over grpc/http.
	case serveCmd.FullCommand():
		err := setupDirs(*indexDirPath, *pcapDirPaths)
		if err != nil {
			log.Fatal().Err(err)
		}
		server := serve.NewQueryServer(*serveGRPCPort, *serveHTTPPort, *serveCert, *serveKey, *serveHTTPServerName, *indexDirPath, *pcapDirPaths)
		kingpin.FatalIfError(server.Run(ctx, done), "Starting query server failed")

	// Query captured pcap data.
	case queryCmd.FullCommand():
		client := query.NewClientConn(*queryGRPCAddr, *queryCA, *queryServerName)
		kingpin.FatalIfError(client.Open(ctx), "Client connection failed")
		kingpin.FatalIfError(client.Execute(ctx, *queryLabel, *queryStart, *queryDuration, *queryType, *queryArg, *queryBinOut, *queryShowAll), "Query failed")
		client.Close()
		done <- struct{}{}

	case infoCmd.FullCommand():
		err := info.Get(*indexDirPath, *infoKeys)
		if err != nil {
			kingpin.Fatalf("Error getting information: %s", err)
		}
		done <- struct{}{}

	}

}

// Check that index and pcap directories exist or make them if not.
func setupDirs(iDir string, pDirs []string) (err error) {
	err = os.MkdirAll(iDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("unable to create directory '%s': %s", iDir, err)
	}
	for _, d := range pDirs {
		err = os.MkdirAll(d, os.ModePerm)
		if err != nil {
			return fmt.Errorf("unable to create directory '%s': %s", d, err)
		}
	}
	return nil
}
