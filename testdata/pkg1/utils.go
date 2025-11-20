package pkg1

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mailru/easyjson"
)

func FormatData(data interface{}) string {
	bytes, _ := json.Marshal(data)
	return strings.TrimSpace(string(bytes))
}

func FormatDataEasyJSON(data easyjson.Marshaler) ([]byte, error) {
	return easyjson.Marshal(data)
}

func PrintMessage(msg string) {
	fmt.Println(msg)
}

