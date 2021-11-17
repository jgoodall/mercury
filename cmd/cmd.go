package cmd

import "context"

// Server is the simple interface for starting/stopping servers.
type Server interface {
	Run(ctx context.Context, done chan<- struct{}) error
	Stop()
}
