package backup

import (
	"io"
	"os"

	"github.com/apimgr/api/src/critical"
)

type criticalWriteReporter struct {
	writer io.Writer
}

type criticalFileWriter struct {
	file *os.File
}

func (w criticalFileWriter) Write(data []byte) (int, error) {
	return criticalWriteReporter{writer: w.file}.Write(data)
}

func (w criticalFileWriter) Close() error {
	err := w.file.Close()
	reportCriticalWriteError(err)
	return err
}

func (w criticalWriteReporter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	critical.ReportFileWriteFailure(err)
	return written, err
}

func reportCriticalWriteError(err error) {
	critical.ReportFileWriteFailure(err)
}
