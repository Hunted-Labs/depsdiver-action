package logger

import (
	"io"
	"log"
	"os"
)

type Logger struct {
	infoLog  *log.Logger
	errorLog *log.Logger
	warnLog  *log.Logger
}

func NewLogger(output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}

	return &Logger{
		infoLog:  log.New(output, "[INFO] ", log.Ldate|log.Ltime),
		errorLog: log.New(output, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile),
		warnLog:  log.New(output, "[WARN] ", log.Ldate|log.Ltime),
	}
}

func (l *Logger) Info(msg string) {
	l.infoLog.Println(msg)
}

func (l *Logger) Error(msg string) {
	l.errorLog.Println(msg)
}

func (l *Logger) Warn(msg string) {
	l.warnLog.Println(msg)
}

