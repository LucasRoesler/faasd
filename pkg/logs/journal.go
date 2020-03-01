package logs

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/coreos/go-systemd/sdjournal"

	"github.com/openfaas/faas-provider/logs"
)

type logJournal struct {
	journal *sdjournal.Journal
	follow  bool
	name    string
}

// read and parse the next log entry
func (j logJournal) read() (logs.Message, error) {
	msg := logs.Message{}

	if j.journal == nil {
		return msg, fmt.Errorf("can not read unopened journal")
	}

	// Advance the journal cursor by one entry
	c, err := j.journal.Next()
	if err != nil {
		return msg, err
	}

	if c == 0 {
		log.Println("might be EOF")
		return msg, io.EOF
	}

	entry, err := j.journal.GetEntry()
	if err != nil {
		log.Println("GetEntry error", err)
		return msg, err
	}

	return parseEntry(entry)
}

// waitAtEnd will block to wait for new journal entries to be ready for reading. A non-nil
// error indicates that you should stop reading the journal.  A nil error indicates new
// log entries are available.
//
// This implementation is based on JournalReader.Follow
// https://github.com/coreos/go-systemd/blob/a4887aeaa186e68961d2d6af7d5fbac6bd6fa79b/sdjournal/read.go#L230
func (j logJournal) wait(ctx context.Context) error {
	if j.journal == nil {
		return nil
	}

	var waitCh = make(chan int, 1)
	for {
		go func() {
			status := j.journal.Wait(100 * time.Millisecond)
			waitCh <- status
		}()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-waitCh:
			switch e {
			case sdjournal.SD_JOURNAL_NOP:
				// the journal did not change since the last invocation
			case sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE:
				return nil
			default:
				if e < 0 {
					return fmt.Errorf("received error event: %d", e)
				}

				log.Printf("received unknown event: %d\n", e)
			}
		}

	}
}

func (j logJournal) close() error {
	if j.journal != nil {
		return j.journal.Close()
	}

	return nil
}

func open(path string, req logs.Request) (_ *logJournal, err error) {
	var journal *sdjournal.Journal
	// Open the journal
	if path != "" {
		journal, err = sdjournal.NewJournalFromDir(path)
	} else {
		journal, err = sdjournal.NewJournal()
	}
	if err != nil {
		return nil, err
	}

	ids, err := journal.GetUniqueValues(sdjournal.SD_JOURNAL_FIELD_SYSLOG_IDENTIFIER)
	if err != nil {
		return nil, err
	}
	log.Printf("syslog ids %+v\n", ids)

	namespace := req.Namespace
	if namespace == "" {
		namespace = "openfaas-fn"
	}

	// filter for containers based on the req
	fncMatch := fmt.Sprintf("%s=%s:%s", sdjournal.SD_JOURNAL_FIELD_SYSLOG_IDENTIFIER, namespace, req.Name)
	log.Println("filter for", fncMatch)
	log.Printf("req %+v\n", req)

	err = journal.AddMatch(fncMatch)
	if err != nil {
		return nil, fmt.Errorf("failed to set name filter: %w", err)
	}

	if req.Instance != "" {
		// TODO: add instance filter
		log.Println("skipping instance filter")
	}

	// set the cursor position based on req, default to 5m
	since := time.Now().Add(-5 * time.Minute)
	if req.Since != nil && req.Since.Before(time.Now()) {
		since = *req.Since
	}

	log.Println("start from ", since.String())

	err = journal.SeekRealtimeUsec(uint64(since.UnixNano() / 1000))
	if err != nil {
		return nil, fmt.Errorf("can not set initial cursor position: %w", err)
	}

	// journal.FlushMatches()

	logs := &logJournal{
		journal: journal,
		follow:  req.Follow,
		name:    req.Name,
	}

	return logs, nil
}

func parseEntry(entry *sdjournal.JournalEntry) (logs.Message, error) {
	log.Printf("got entry %+v\n", entry)
	logMsg := logs.Message{}

	text, ok := entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE]
	if !ok {
		return logMsg, fmt.Errorf("no MESSAGE field present in journal entry")
	}
	logMsg.Text = text

	usec := entry.RealtimeTimestamp
	logMsg.Timestamp = time.Unix(0, int64(usec)*int64(time.Microsecond))
	logMsg.Instance = entry.Fields[sdjournal.SD_JOURNAL_FIELD_PID]

	identifier := entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSLOG_IDENTIFIER]
	parts := strings.Split(identifier, ":")

	switch len(parts) {
	case 2:
		logMsg.Namespace = parts[0]
		logMsg.Name = parts[1]
	default:
		logMsg.Name = identifier
	}

	return logMsg, nil
}
