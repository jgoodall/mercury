package capture

import (
	"sync"

	"github.com/rs/zerolog/log"
)

var inChanLen int

// muxMessageChans takes a number of message input channels and
// combines them into a single output channel.
func muxMessageChans(outBufferSize int, done *sync.WaitGroup, inCh ...chan *Message) chan *Message {
	outCh := make(chan *Message, outBufferSize)
	logger := log.With().Str("component", "message-chan-muxer").Logger()

	logger.Info().Msg("started")

	inChanLen = len(inCh)

	for _, ch := range inCh {
		go func(out chan *Message, in chan *Message) {
			defer func() {
				// Decrement until last input channel and then cleanup.
				inChanLen -= 1
				if inChanLen == 0 {
					logger.Info().Msg("completed")
					close(outCh)
					done.Done()
				}
			}()
			for msg := range in {
				out <- msg
			}
		}(outCh, ch)
	}
	return outCh
}
