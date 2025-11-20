package testing

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
)

func NewTestRequest(method, url string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, url, body)
	return req
}

func NewTestResponse() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func ReadBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func CreateTestBody(data string) io.Reader {
	return bytes.NewBufferString(data)
}

