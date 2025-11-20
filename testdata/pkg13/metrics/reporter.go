package metrics

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Report struct {
	Counters map[string]int64            `json:"counters"`
	Timers   map[string]time.Duration    `json:"timers"`
}

type Reporter struct {
	counters map[string]*Counter
	timers   map[string]*Timer
}

func NewReporter() *Reporter {
	return &Reporter{
		counters: make(map[string]*Counter),
		timers:   make(map[string]*Timer),
	}
}

func (r *Reporter) GetCounter(name string) *Counter {
	if c, exists := r.counters[name]; exists {
		return c
	}
	c := NewCounter()
	r.counters[name] = c
	return c
}

func (r *Reporter) GetTimer(name string) *Timer {
	if t, exists := r.timers[name]; exists {
		return t
	}
	t := NewTimer()
	r.timers[name] = t
	return t
}

func (r *Reporter) GenerateReport() (*Report, error) {
	report := &Report{
		Counters: make(map[string]int64),
		Timers:   make(map[string]time.Duration),
	}

	for name, counter := range r.counters {
		report.Counters[name] = counter.Value()
	}

	for name, timer := range r.timers {
		report.Timers[name] = timer.Average()
	}

	return report, nil
}

func (r *Reporter) WriteReportToFile(path string) error {
	report, err := r.GenerateReport()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func (r *Reporter) PrintReport() {
	report, err := r.GenerateReport()
	if err != nil {
		fmt.Printf("Error generating report: %v\n", err)
		return
	}

	fmt.Println("=== Metrics Report ===")
	fmt.Println("Counters:")
	for name, value := range report.Counters {
		fmt.Printf("  %s: %d\n", name, value)
	}
	fmt.Println("Timers:")
	for name, value := range report.Timers {
		fmt.Printf("  %s: %v\n", name, value)
	}
}

