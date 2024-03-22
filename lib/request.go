package lib

import (
	"bytes"
	"encoding/json"
	"errors"
	"go.uber.org/zap"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
)

func Request(endpoint string, body interface{}) (*http.Response, error) {
	// bodyをjsonに変換
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("failed to marshal body: " + err.Error())
	}
	return RequestRaw(endpoint, "application/json", bytes.NewBuffer(jsonBody))
}

func RequestRaw(endpoint string, contentType string, body *bytes.Buffer) (*http.Response, error) {
	// リクエストを作成
	req, err := http.NewRequest("POST", endpoint, body)
	if err != nil {
		return nil, errors.New("failed to create request: " + err.Error())
	}
	req.Header.Set("Content-Type", contentType)

	// リクエストを送信
	zap.S().Debugln("RequestRaw request send")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		zap.S().Fatalln("RequestRaw request send failed: %+v", err)
	}
	zap.S().Debugf("RequestRaw result; %+v", res)

	zap.S().Debugln("RequestRaw end")
	return res, err
}

type MultipartRequestOption func(writer *multipart.Writer)

func MultipartRequest(endpoint string, options ...MultipartRequestOption) (*http.Response, error) {
	zap.S().Debugln("MultipartRequest called")
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	for _, opt := range options {
		opt(w)
	}
	err := w.Close()
	if err != nil {
		zap.S().Fatalf("MultipartRequest writer close failed: %+v", err)
	}
	zap.S().Debugln("MultipartRequest for end")

	return RequestRaw(endpoint, w.FormDataContentType(), body)
}

func SetMultipartField(field string, data []byte) MultipartRequestOption {
	return func(writer *multipart.Writer) {
		part, _ := writer.CreateFormField(field)
		_, err := part.Write(data)
		if err != nil {
			zap.S().Panicf("CreatePart Failed")
		}
	}
}

func SetMultipartFile(field string, path string) MultipartRequestOption {
	return func(writer *multipart.Writer) {
		filename := filepath.Base(path)
		file, err := os.Open(path)
		if err != nil {
			zap.S().Panicf("Could not open '%s': %v", path, err)
		}
		part, _ := writer.CreateFormFile(field, filename)
		_, err = io.Copy(part, file)
		if err != nil {
			zap.S().Panicf("CreatePart Failed")
		}
	}
}
