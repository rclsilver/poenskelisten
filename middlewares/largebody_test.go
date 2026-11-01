package middlewares

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// largeBodyRouter gates /upload with LargeBodies(slots, 10 bytes, wait). Its
// handler holds every large request until release is closed, and counts how
// many it holds at once.
func largeBodyRouter(slots int, wait time.Duration, release chan struct{}) (*gin.Engine, *atomic.Int32, *atomic.Int32) {
	gin.SetMode(gin.TestMode)
	var active, most atomic.Int32
	router := gin.New()
	router.POST("/upload", LargeBodies(slots, 10, wait), func(ctx *gin.Context) {
		if ctx.Request.ContentLength >= 0 && ctx.Request.ContentLength <= 10 {
			ctx.Status(http.StatusOK)
			return
		}
		now := active.Add(1)
		for {
			highest := most.Load()
			if now <= highest || most.CompareAndSwap(highest, now) {
				break
			}
		}
		<-release
		active.Add(-1)
		ctx.Status(http.StatusOK)
	})
	return router, &active, &most
}

func postBody(router *gin.Engine, length int) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(make([]byte, length)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	return w
}

// However many large requests arrive at once, only slots of them are handled
// at a time; the others wait their turn, and small ones never wait.
func TestLargeBodiesHandlesSlotsAtATime(t *testing.T) {
	release := make(chan struct{})
	router, active, most := largeBodyRouter(2, 5*time.Second, release)

	var done sync.WaitGroup
	codes := make([]int, 6)
	for i := range codes {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			codes[i] = postBody(router, 100).Code
		}(i)
	}
	deadline := time.Now().Add(2 * time.Second)
	for active.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := active.Load(); got != 2 {
		t.Errorf("large requests handled at once = %d, want 2", got)
	}
	if w := postBody(router, 5); w.Code != http.StatusOK {
		t.Errorf("a small request while the slots are taken = %d, want 200 at once", w.Code)
	}

	close(release)
	done.Wait()
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("large request %d = %d, want 200 once a slot freed", i, code)
		}
	}
	if got := most.Load(); got != 2 {
		t.Errorf("most large requests handled at once = %d, want 2", got)
	}
}

// A request that can't get a slot in time is refused before its body is read,
// with a status and a delay the client can retry on; a body of unknown length
// counts as large.
func TestLargeBodiesRefusesAfterWaiting(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	router, active, _ := largeBodyRouter(1, 50*time.Millisecond, release)

	go postBody(router, 100)
	for active.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	request := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewReader(make([]byte, 100)))
	request.ContentLength = -1
	w := httptest.NewRecorder()
	router.ServeHTTP(w, request)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" || !bytes.Contains(w.Body.Bytes(), []byte("Too many large requests")) {
		t.Errorf("waited too long = %d %q (Retry-After %q), want 503 with a message and Retry-After", w.Code, w.Body.String(), w.Header().Get("Retry-After"))
	}
}
