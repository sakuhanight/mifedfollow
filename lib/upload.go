package lib

import (
	"fmt"
	"go.uber.org/zap"
	"net/http"
)

func UploadExample(host string, token string, path string) (*http.Response, error) {
	endpoint := fmt.Sprintf("https://%s/api/drive/files/create", host)
	zap.S().Debugln("UploadExample called")
	var options []MultipartRequestOption
	options = append(options, SetMultipartField("i", []byte(token)))
	options = append(options, SetMultipartField("force", []byte("true")))
	options = append(options, SetMultipartFile("file", path))

	return MultipartRequest(endpoint, options...)
}
