package api

import (
	"sync"
	"time"

	"github.com/JESUSLUG/TowerCrane/backend/internal/db"
)

// maxLiveLog caps a stored operation log; the oldest output is dropped.
const maxLiveLog = 1 << 20

const liveLogFlush = 1500 * time.Millisecond

// liveLog collects an operation's command output while it runs and writes
// it to the operation's log every couple of seconds, so the UI can follow
// a plan or an apply as it happens.
type liveLog struct {
	store *db.OperationStore
	id    string

	mu    sync.Mutex
	buf   []byte
	dirty bool

	stop chan struct{}
	done chan struct{}
}

func newLiveLog(store *db.OperationStore, id, initial string) *liveLog {
	l := &liveLog{store: store, id: id, buf: []byte(initial), stop: make(chan struct{}), done: make(chan struct{})}
	go l.loop()
	return l
}

func (l *liveLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	l.buf = append(l.buf, p...)
	if over := len(l.buf) - maxLiveLog; over > 0 {
		l.buf = append([]byte("[... earlier output truncated ...]\n"), l.buf[over:]...)
	}
	l.dirty = true
	l.mu.Unlock()
	return len(p), nil
}

// WriteLine adds a TowerCrane message (not command output) to the log.
func (l *liveLog) WriteLine(msg string) {
	l.Write([]byte(msg + "\n"))
}

func (l *liveLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}

func (l *liveLog) flush() {
	l.mu.Lock()
	if !l.dirty {
		l.mu.Unlock()
		return
	}
	s := string(l.buf)
	l.dirty = false
	l.mu.Unlock()
	l.store.UpdateLog(l.id, s)
}

func (l *liveLog) loop() {
	defer close(l.done)
	t := time.NewTicker(liveLogFlush)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			l.flush()
		case <-l.stop:
			return
		}
	}
}

// close stops the periodic writes and returns the whole log.
func (l *liveLog) close() string {
	close(l.stop)
	<-l.done
	return l.String()
}

// liveLogFor returns the operation's live log, starting it if needed.
func (oh *OperationHandler) liveLogFor(op *db.Operation) *liveLog {
	oh.logsMu.Lock()
	defer oh.logsMu.Unlock()
	if oh.logs == nil {
		oh.logs = map[string]*liveLog{}
	}
	if l, ok := oh.logs[op.ID]; ok {
		return l
	}
	l := newLiveLog(oh.operationStore, op.ID, op.Log+"\n")
	oh.logs[op.ID] = l
	return l
}

// complete finishes an operation. With a live log, the stored log is that
// log (it already holds the command output); otherwise it is output.
func (oh *OperationHandler) complete(op *db.Operation, status, output, errMsg string) {
	oh.logsMu.Lock()
	l, ok := oh.logs[op.ID]
	delete(oh.logs, op.ID)
	oh.logsMu.Unlock()
	log := output
	if ok {
		if errMsg != "" {
			l.WriteLine("\n" + errMsg)
		}
		log = l.close()
	}
	oh.operationStore.Complete(op.ID, status, log, errMsg)
}
