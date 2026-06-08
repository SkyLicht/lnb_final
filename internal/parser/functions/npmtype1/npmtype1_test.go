package npmtype1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"lnb_tk/internal/parser/types"
)

type captureLogger struct {
	infos  []string
	errors []string
}

func (l *captureLogger) Infof(format string, args ...any) {
	l.infos = append(l.infos, sprintf(format, args...))
}

func (l *captureLogger) Errorf(format string, args ...any) {
	l.errors = append(l.errors, sprintf(format, args...))
}

func sprintf(format string, args ...any) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}

func TestClassifyEvent(t *testing.T) {
	tests := []struct {
		name            string
		eventCode       string
		eventDetailCode string
		want            string
		wantErr         bool
	}{
		{name: "tower", eventCode: "50", eventDetailCode: "000000", want: eventTypeTower},
		{name: "ct", eventCode: "04", eventDetailCode: "000000", want: eventTypeCT},
		{name: "unknown", eventCode: "99", eventDetailCode: "000000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyEvent(tt.eventCode, tt.eventDetailCode)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("event type mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestParseTowerEventLogsMachineState(t *testing.T) {
	log := &captureLogger{}
	content := []byte(`<?xml version="1.0" encoding="UTF-8"?><MachineEvent><Element><Date>2026/05/21,15:42:49</Date><MDLN>71100</MDLN><EventSerial>466554</EventSerial><EventCode>50</EventCode><EventDetailCode>000000</EventDetailCode><Stage>02</Stage><Lane>01</Lane><RedLightStatus>00</RedLightStatus><YellowLightStatus>00</YellowLightStatus><GreenLightStatus>01</GreenLightStatus><ReserveLightStatus>00</ReserveLightStatus><BuzzerStatus>00</BuzzerStatus><MCNo>1</MCNo></Element></MachineEvent>`)

	result, err := Parse(context.Background(), types.Request{
		WatcherName: "machine_01_logs",
		FilePath:    "event.xml",
		Content:     content,
		Log:         log,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Records != 1 {
		t.Fatalf("records mismatch: got %d want 1", result.Records)
	}
	if len(log.infos) != 1 {
		t.Fatalf("expected one info log, got %d", len(log.infos))
	}

	logLine := log.infos[0]
	required := []string{
		"event_type=tower_event",
		"machine_name=machine_01_logs",
		"machine=1",
		"key=02_01",
		"red=00",
		"green=01",
		"yellow=00",
		"buzzer=00",
	}
	for _, field := range required {
		if !strings.Contains(logLine, field) {
			t.Fatalf("expected log field %q in %q", field, logLine)
		}
	}
}
