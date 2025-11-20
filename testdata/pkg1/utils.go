package pkg1

import (
	"encoding/json"
	"fmt"
	"strings"
)

func FormatData(data interface{}) string {
	bytes, _ := json.Marshal(data)
	return strings.TrimSpace(string(bytes))
}

func PrintMessage(msg string) {
	fmt.Println(msg)
}

