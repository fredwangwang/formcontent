package formcontent

import (
	"io"
	"mime/multipart"
	"bytes"
	"os"
	"errors"
	"path/filepath"
	"fmt"
)

type Form struct {
	contentType string
	boundary    string
	length      int64
	pr          *io.PipeReader
	pw          *io.PipeWriter
	formFields  *bytes.Buffer
	formWriter  *multipart.Writer
	files       []string
	fileKeys    []*bytes.Buffer
}

type ContentSubmission struct {
	Length      int64
	Content     io.Reader
	ContentType string
}

func NewForm() (*Form, error) {
	buf := &bytes.Buffer{}

	pr, pw := io.Pipe()

	formWriter := multipart.NewWriter(buf)

	return &Form{
		contentType: formWriter.FormDataContentType(),
		boundary:    formWriter.Boundary(),
		pr:          pr,
		pw:          pw,
		formFields:  buf,
		formWriter:  formWriter,
	}, nil
}

func (f *Form) AddField(key string, value string) error {
	fieldWriter, err := f.formWriter.CreateFormField(key)
	if err != nil {
		return err
	}

	_, err = fieldWriter.Write([]byte(value))
	return err
}

func (f *Form) AddFile(key string, path string) error {
	fileLength, err := verifyFile(path)
	if err != nil {
		return err
	}

	buf := &bytes.Buffer{}

	fileKey := multipart.NewWriter(buf)
	fileKey.SetBoundary(f.boundary)

	_, err = fileKey.CreateFormFile(key, filepath.Base(path))
	if err != nil {
		return err
	}

	f.length += fileLength
	f.length += int64(buf.Len())

	fmt.Println(buf.String() + "***")

	f.files = append(f.files, path)
	f.fileKeys = append(f.fileKeys, buf)

	return nil
}

func (f *Form) Finalize() (ContentSubmission, error) {
	trailingBoundary, err := f.generateTrailingBoundary()
	if err != nil {
		return ContentSubmission{}, err
	}

	f.length += int64(trailingBoundary.Len())

	if len(f.files) > 0 {
		f.length += int64(2 * (len(f.files) - 1))
	}

	go f.writeToPipe()

	return ContentSubmission{
		Length:      f.length,
		Content:     f.pr,
		ContentType: f.contentType,
	}, nil
}

func verifyFile(path string) (int64, error) {
	fileContent, err := os.Open(path)
	if err != nil {
		return 0, err
	}

	defer fileContent.Close()

	stats, err := fileContent.Stat()
	if err != nil {
		return 0, err
	}

	if stats.Size() == 0 {
		return 0, errors.New("file provided has no content")
	}

	return stats.Size(), nil
}

func (f *Form) generateTrailingBoundary() (buf *bytes.Buffer, err error) {
	buf = &bytes.Buffer{}
	_, err = fmt.Fprintf(buf, "\r\n--%s--\r\n", f.boundary)
	return
}

func (f *Form) writeToPipe() {
	var err error
	separate := false

	for i, key := range f.fileKeys {
		if separate {
			f.pw.Write([]byte("\r\n"))
		}

		_, err = io.Copy(f.pw, key)
		if err != nil {
			f.pw.CloseWithError(err)
			return
		}

		fileName := f.files[i]
		err = writeFileToPipe(fileName, f.pw)
		if err != nil {
			f.pw.CloseWithError(err)
			return
		}

		separate = true
	}

	trailingBoundary, err := f.generateTrailingBoundary()
	if err != nil {
		f.pw.CloseWithError(err)
		return
	}

	_, err = io.Copy(f.pw, trailingBoundary)
	if err != nil {
		f.pw.CloseWithError(err)
		return
	}

	f.pw.Close()
	return
}

func writeFileToPipe(fileName string, writer *io.PipeWriter) error {
	fileContent, err := os.Open(fileName)
	if err != nil {
		return err
	}

	defer fileContent.Close()

	_, err = io.Copy(writer, fileContent)
	if err != nil {
		return err
	}

	return nil
}
