package logs

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/openfaas/faas-provider/logs"
)

type requester struct {
	// If not empty, the systemd journal instance will point to a journal residing
	// in this directory.
	path string
}

// New returns a new Loki log Requester
func New(path string) logs.Requester {
	return &requester{
		path: path,
	}
}

// Query submits a log request to the actual logging system.
func (r *requester) Query(ctx context.Context, req logs.Request) (<-chan logs.Message, error) {
	log.Println("query journal in", r.path)
	journal, err := open(r.path, req)
	if err != nil {
		return nil, fmt.Errorf("can not open systemd journal: %w", err)
	}

	msgs := make(chan logs.Message, 100)
	go streamLogs(ctx, journal, msgs)

	return msgs, nil
}

func streamLogs(ctx context.Context, journal *logJournal, msgs chan logs.Message) {
	log.Println("starting journal stream")

	defer func() {
		log.Println("closing journal stream")
		err := journal.close()
		if err != nil {
			log.Printf("journal closed with error %s\n", err)
		}

		close(msgs)
	}()

	for {
		if ctx.Err() != nil {
			log.Println("log stream context cancelled")
			return
		}
		msg, err := journal.read()
		if err == nil {
			msgs <- msg
			continue
		}

		// we are at the tail of the log
		if err == io.EOF && journal.follow {
			waitErr := journal.wait(ctx)
			if waitErr != nil {
				log.Printf("unexpected error while waiting: %s", waitErr)
				return
			}
			// no error means, loop again to read new lines
			continue
		}

		if err != nil {
			log.Println("read error: ", err)
			return
		}
	}
}
