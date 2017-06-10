package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type WebResponse struct {
	Ok    bool      `json:"ok"`
	Error *WebError `json:"error"`
}

type WebError string

func (s WebError) Error() string {
	return string(s)
}

func formReq(endpoint string, values url.Values) (*http.Request, error) {
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func jsonReq(endpoint string, body interface{}) (req *http.Request, err error) {
	buffer := bytes.NewBuffer([]byte{})
	if err = json.NewEncoder(buffer).Encode(body); err != nil {
		return nil, err
	}

	if req, err = http.NewRequest("POST", endpoint, buffer); err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return req, nil
}

func fileUploadReq(endpoint, fieldname, filename string, r io.Reader, values url.Values) (*http.Request, error) {
	var (
		err      error
		req      *http.Request
		iowriter io.Writer
	)

	body := &bytes.Buffer{}
	wr := multipart.NewWriter(body)

	if iowriter, err = wr.CreateFormFile(fieldname, filename); err != nil {
		wr.Close()
		return nil, err
	}

	if _, err = io.Copy(iowriter, r); err != nil {
		wr.Close()
		return nil, err
	}

	// Close the multipart writer or the footer won't be written
	if err = wr.Close(); err != nil {
		return nil, err
	}

	if req, err = http.NewRequest("POST", endpoint, body); err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", wr.FormDataContentType())
	req.URL.RawQuery = values.Encode()
	return req, nil
}

func newJSONResponseParser(dst interface{}) responseParser {
	return func(body io.Reader) error {
		return json.NewDecoder(body).Decode(dst)
	}
}

func newTextResponseParser(dst interface{}) responseParser {
	return func(body io.Reader) error {
		b, err := ioutil.ReadAll(body)
		if err != nil {
			return err
		}

		if !bytes.Equal(b, []byte("ok")) {
			return errors.New(string(b))
		}

		return nil
	}
}

func postLocalWithMultipartResponse(ctx context.Context, client HTTPRequester, path, fpath, fieldname string, values url.Values, intf interface{}, debug bool) error {
	fullpath, err := filepath.Abs(fpath)
	if err != nil {
		return err
	}
	file, err := os.Open(fullpath)
	if err != nil {
		return err
	}
	defer file.Close()
	return postWithMultipartResponse(ctx, client, SLACK_API+path, filepath.Base(fpath), fieldname, values, file, intf, debug)
}

func postWithMultipartResponse(ctx context.Context, client HTTPRequester, endpoint, name, fieldname string, values url.Values, r io.Reader, intf interface{}, debug bool) error {
	req, err := fileUploadReq(endpoint, fieldname, name, r, values)
	if err != nil {
		return err
	}
	return post(ctx, client, req, newJSONResponseParser(intf), debug)
}

func parseAdminResponse(ctx context.Context, client HTTPRequester, method string, teamName string, values url.Values, intf interface{}, debug bool) error {
	endpoint := fmt.Sprintf(SLACK_WEB_API_FORMAT, teamName, method, time.Now().Unix())
	return postForm(ctx, client, endpoint, values, intf, debug)
}

func postForm(ctx context.Context, client HTTPRequester, endpoint string, values url.Values, intf interface{}, debug bool) error {
	req, err := formReq(endpoint, values)
	if err != nil {
		return err
	}
	return post(ctx, client, req, newJSONResponseParser(intf), debug)
}

type responseParser func(body io.Reader) error

func post(ctx context.Context, client HTTPRequester, req *http.Request, parseResponseBody responseParser, debug bool) error {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Slack seems to send an HTML body along with 5xx error codes. Don't parse it.
	if resp.StatusCode != 200 {
		logResponse(resp, debug)
		return fmt.Errorf("Slack server error: %s.", resp.Status)
	}

	return parseResponseBody(resp.Body)
}

func logResponse(resp *http.Response, debug bool) error {
	if debug {
		text, err := httputil.DumpResponse(resp, true)
		if err != nil {
			return err
		}

		logger.Print(string(text))
	}

	return nil
}
