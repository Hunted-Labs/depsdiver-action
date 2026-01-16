package pkg1

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mailru/easyjson"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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

// TracedOperation demonstrates usage of OpenTelemetry for tracing
func TracedOperation(ctx context.Context, operationName string) {
	tracer := otel.Tracer("test-tracer")
	_, span := tracer.Start(ctx, operationName)
	defer span.End()

	span.SetAttributes(
		attribute.String("operation", operationName),
		attribute.Bool("test", true),
	)
}

