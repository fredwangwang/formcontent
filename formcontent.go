package formcontent

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

type Form struct {
	contentType string
	length      int64
	cp          *contentPreparer
}

type ContentSubmission struct {
	Content       io.Reader
	ContentType   string
	ContentLength int64
}

type contentPreparer struct {
	boundary           string
	formFields         *bytes.Buffer
	formWriter         *multipart.Writer
	files              []string
	fileKeys           []*bytes.Buffer
	writeSep           bool
	initialized        bool
	readFileKey        bool
	readFileContent    bool
	nextFile           int
	currentFileKey     io.Reader
	currentFileContent *os.File
}

func (c *contentPreparer) Read(p []byte) (int, error) {
	var err error

	// a limitation of writeSep that it assumes len(p) > 2, which I don't really see this as an issue as the default len is 512 on linux (at least for my machine)
	if c.writeSep {
		c.writeSep = false
		buf := bytes.NewBufferString("\r\n")
		return buf.Read(p)
	}

	if c.nextFile < len(c.files) {
		if !c.initialized {
			c.currentFileKey = c.fileKeys[c.nextFile]
			c.currentFileContent, err = os.Open(c.files[c.nextFile])
			if err != nil {
				return 0, err
			}

			c.readFileKey = true
			c.initialized = true
			return 0, nil
		}

		if c.readFileKey {
			length, err := c.currentFileKey.Read(p)
			if err == nil {
				return length, nil
			} else if err == io.EOF {
				c.readFileKey = false
				c.readFileContent = true
				return length, nil
			} else {
				return length, err
			}
		}

		if c.readFileContent {
			length, err := c.currentFileContent.Read(p)
			if err == nil {
				return length, nil
			} else if err == io.EOF {
				defer c.currentFileContent.Close()
				c.initialized = false
				c.readFileContent = false
				c.nextFile++
				// boundary+8 =>format: \r\n--boundary-words--\r\n
				if c.nextFile < len(c.files) || c.formFields.Len() > len(c.boundary)+8 {
					c.writeSep = true
				}
				return length, nil
			} else {
				return length, err
			}
		}
	}

	return c.formFields.Read(p)
}

func NewForm() *Form {
	buf := &bytes.Buffer{}

	formWriter := multipart.NewWriter(buf)

	cp := &contentPreparer{
		formFields: buf,
		formWriter: formWriter,
		boundary:   formWriter.Boundary(),
	}

	return &Form{
		cp:          cp,
		contentType: formWriter.FormDataContentType(),
	}
}

func (f *Form) AddField(key string, value string) error {
	fieldWriter, err := f.cp.formWriter.CreateFormField(key)
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
	fileKey.SetBoundary(f.cp.boundary)

	_, err = fileKey.CreateFormFile(key, filepath.Base(path))
	if err != nil {
		return err
	}

	f.length += fileLength
	f.length += int64(buf.Len())

	f.cp.files = append(f.cp.files, path)
	f.cp.fileKeys = append(f.cp.fileKeys, buf)

	return nil
}

func (f *Form) Finalize() ContentSubmission {
	f.cp.formWriter.Close()

	// add the length of form fields, including trailing boundary
	f.length += int64(f.cp.formFields.Len())

	// add the length of `\r\n` between fields
	if len(f.cp.files) > 0 {
		f.length += int64(2 * (len(f.cp.files) - 1))
		if f.cp.formFields.Len() > len(f.cp.boundary)+8 {
			f.length += 2
		}
	}

	return ContentSubmission{
		ContentLength: f.length,
		Content:       f.cp,
		ContentType:   f.contentType,
	}
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
