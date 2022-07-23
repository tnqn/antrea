package tcpdump

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleFunc(t *testing.T) {
	handler := HandleFunc(nil)

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	assert.Nil(t, err)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	fmt.Println(recorder.Body)
}
