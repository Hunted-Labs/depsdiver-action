package logger

import (
	"fmt"
	"runtime"
	"strings"
)

func FormatError(err error) string {
	pc, file, line, ok := runtime.Caller(1)
	if !ok {
		return fmt.Sprintf("Error: %v", err)
	}

	funcName := runtime.FuncForPC(pc).Name()
	funcName = strings.TrimPrefix(funcName, "main.")

	return fmt.Sprintf("%s:%d [%s] %v", file, line, funcName, err)
}

func GetCallerInfo(skip int) string {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}

	funcName := runtime.FuncForPC(pc).Name()
	return fmt.Sprintf("%s:%d in %s", file, line, funcName)
}

