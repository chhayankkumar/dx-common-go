package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/datakaveri/dx-common-go/auth"
)

// AuditEvent carries all information about a single handled request for audit
// logging or forwarding to an external audit service.
type AuditEvent struct {
	RequestID  string    `json:"request_id"`
	UserID     string    `json:"user_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	StatusCode int       `json:"status_code"`
	Timestamp  time.Time `json:"timestamp"`
}

// AuditEmitter is the interface that audit sinks must implement.
type AuditEmitter interface {
	Emit(ctx context.Context, event AuditEvent) error
}

var _ AuditEmitter = (*LogAuditEmitter)(nil)

// LogAuditEmitter is the default implementation; it writes events to a zap logger.
type LogAuditEmitter struct {
	Logger *zap.Logger
}

// Emit logs the audit event at Info level.
func (e *LogAuditEmitter) Emit(_ context.Context, event AuditEvent) error {
	e.Logger.Info("audit",
		zap.String("request_id", event.RequestID),
		zap.String("user_id", event.UserID),
		zap.String("action", event.Action),
		zap.String("resource", event.Resource),
		zap.String("resource_id", event.ResourceID),
		zap.String("method", event.Method),
		zap.String("path", event.Path),
		zap.Int("status_code", event.StatusCode),
		zap.Time("timestamp", event.Timestamp),
	)
	return nil
}

// AuditWorker manages background audit event emission with a bounded queue
// and graceful shutdown. Create one per service and share it across routes.
type AuditWorker struct {
	emitter AuditEmitter
	events  chan AuditEvent
	wg      sync.WaitGroup
}

// NewAuditWorker creates a worker with the given queue capacity.
// It starts a background goroutine that drains the queue. Call Stop to
// flush pending events on shutdown.
func NewAuditWorker(emitter AuditEmitter, queueSize int) *AuditWorker {
	if queueSize <= 0 {
		queueSize = 256
	}
	w := &AuditWorker{
		emitter: emitter,
		events:  make(chan AuditEvent, queueSize),
	}
	w.wg.Add(1)
	go w.drain()
	return w
}

func (w *AuditWorker) drain() {
	defer w.wg.Done()
	for event := range w.events {
		if err := w.emitter.Emit(context.Background(), event); err != nil {
			w.logEmitError(event, err)
		}
	}
}

func (w *AuditWorker) logEmitError(event AuditEvent, err error) {
	if le, ok := w.emitter.(*LogAuditEmitter); ok && le.Logger != nil {
		le.Logger.Warn("audit emit failed",
			zap.String("request_id", event.RequestID),
			zap.Error(err),
		)
	}
}

// Send enqueues an event. If the queue is full, the event is dropped silently
// to avoid blocking the HTTP response path.
func (w *AuditWorker) Send(event AuditEvent) {
	select {
	case w.events <- event:
	default:
	}
}

// Stop closes the queue and blocks until all pending events have been emitted.
func (w *AuditWorker) Stop() {
	close(w.events)
	w.wg.Wait()
}

// Audit returns a middleware that emits an AuditEvent for every request.
// resource and action describe the business context (e.g. "dataset", "read").
func Audit(emitter AuditEmitter, resource, action string) func(http.Handler) http.Handler {
	return AuditWithWorker(NewAuditWorker(emitter, 256), resource, action)
}

// AuditWithWorker returns a middleware that uses a shared AuditWorker.
// Prefer this over Audit when you need to call worker.Stop() on shutdown.
func AuditWithWorker(worker *AuditWorker, resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)

			user, _ := auth.UserFromCtx(r.Context())

			worker.Send(AuditEvent{
				RequestID:  RequestIDFromCtx(r.Context()),
				UserID:     user.ID,
				Action:     action,
				Resource:   resource,
				Method:     r.Method,
				Path:       r.URL.Path,
				StatusCode: rw.status,
				Timestamp:  time.Now().UTC(),
			})
		})
	}
}
