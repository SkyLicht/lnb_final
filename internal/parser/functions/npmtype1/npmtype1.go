package npmtype1

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lnb_tk/internal/parser/types"
)

const (
	eventTypeTower = "tower_event"
	eventTypeCT    = "ct_event"
)

var outputLocks sync.Map

type machineEvent struct {
	XMLName xml.Name     `xml:"MachineEvent"`
	Element eventElement `xml:"Element"`
}

type eventElement struct {
	Date               string `xml:"Date"`
	MDLN               string `xml:"MDLN"`
	EventSerial        string `xml:"EventSerial"`
	EventCode          string `xml:"EventCode"`
	EventDetailCode    string `xml:"EventDetailCode"`
	PcbSerial          string `xml:"PcbSerial"`
	Stage              string `xml:"Stage"`
	Lane               string `xml:"Lane"`
	CurrentPcbPosition string `xml:"CurrentPcbPosition"`
	ProductBoardCount  string `xml:"ProductBoardCount"`
	CycleTime1         string `xml:"CycleTime1"`
	CycleTime2         string `xml:"CycleTime2"`
	Lot                string `xml:"Lot"`
	ProductMode        string `xml:"ProductMode"`
	RedLightStatus     string `xml:"RedLightStatus"`
	YellowLightStatus  string `xml:"YellowLightStatus"`
	GreenLightStatus   string `xml:"GreenLightStatus"`
	ReserveLightStatus string `xml:"ReserveLightStatus"`
	BuzzerStatus       string `xml:"BuzzerStatus"`
	MCNo               string `xml:"MCNo"`
}

func Parse(ctx context.Context, req types.Request) (types.Result, error) {
	select {
	case <-ctx.Done():
		return types.Result{}, ctx.Err()
	default:
	}

	var event machineEvent
	if err := xml.Unmarshal(req.Content, &event); err != nil {
		return types.Result{}, fmt.Errorf("parse npm_type1 XML: %w", err)
	}

	eventType, err := classifyEvent(event.Element.EventCode, event.Element.EventDetailCode)
	if err != nil {
		return types.Result{}, err
	}

	switch eventType {
	case eventTypeTower:
		return handleTowerEvent(req, event.Element)
	case eventTypeCT:
		return handleCTEvent(req, event.Element), nil
	default:
		return types.Result{}, fmt.Errorf("unsupported event_type %q", eventType)
	}
}

func classifyEvent(eventCode string, eventDetailCode string) (string, error) {
	eventKey := fmt.Sprintf("%s-%s", eventCode, eventDetailCode)
	switch eventKey {
	case "50-000000":
		return eventTypeTower, nil
	case "04-000000":
		return eventTypeCT, nil
	default:
		return "", fmt.Errorf("unsupported npm_type1 event code %q", eventKey)
	}
}

func handleTowerEvent(req types.Request, event eventElement) (types.Result, error) {
	stageLaneKey := fmt.Sprintf("%s_%s", event.Stage, event.Lane)
	req.Log.Infof(
		"event_type=%s machine_name=%s machine=%s key=%s red=%s green=%s yellow=%s buzzer=%s reserve=%s event_serial=%s date=%s",
		eventTypeTower,
		req.WatcherName,
		event.MCNo,
		stageLaneKey,
		event.RedLightStatus,
		event.GreenLightStatus,
		event.YellowLightStatus,
		event.BuzzerStatus,
		event.ReserveLightStatus,
		event.EventSerial,
		event.Date,
	)
	if req.OutputDir != "" {
		if err := updateTowerStateFile(req, event, stageLaneKey); err != nil {
			return types.Result{}, fmt.Errorf("update tower output state: %w", err)
		}
	}
	return types.Result{Records: 1}, nil
}

func handleCTEvent(req types.Request, event eventElement) types.Result {
	stageLaneKey := fmt.Sprintf("%s_%s", event.Stage, event.Lane)
	req.Log.Infof(
		"event_type=%s machine_name=%s machine=%s key=%s pcb_serial=%s current_pcb_position=%s product_board_count=%s cycle_time_1=%s cycle_time_2=%s lot=%s event_serial=%s date=%s",
		eventTypeCT,
		req.WatcherName,
		event.MCNo,
		stageLaneKey,
		event.PcbSerial,
		event.CurrentPcbPosition,
		event.ProductBoardCount,
		event.CycleTime1,
		event.CycleTime2,
		event.Lot,
		event.EventSerial,
		event.Date,
	)
	return types.Result{Records: 1}
}

func updateTowerStateFile(req types.Request, event eventElement, stageLaneKey string) error {
	if event.MCNo == "" {
		return fmt.Errorf("missing MCNo")
	}
	if event.MCNo != "1" && event.MCNo != "2" && event.MCNo != "3" && event.MCNo != "4" {
		return fmt.Errorf("unsupported MCNo %q", event.MCNo)
	}

	outputFile := filepath.Join(req.OutputDir, fmt.Sprintf("%s.json", safeFileName(req.WatcherName)))
	lockValue, _ := outputLocks.LoadOrStore(outputFile, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)

	lock.Lock()
	defer lock.Unlock()

	state, err := readOutputState(outputFile)
	if err != nil {
		return err
	}
	ensureMachines(state)

	machineKey := fmt.Sprintf("machine_%s", event.MCNo)
	machine, ok := state[machineKey].(map[string]any)
	if !ok {
		machine = map[string]any{}
		state[machineKey] = machine
	}

	tower, ok := machine["tower"].(map[string]any)
	if !ok {
		tower = defaultTower()
		machine["tower"] = tower
	}

	lastUpdate := time.Now().Format(time.RFC3339)
	tower["state"] = map[string]any{
		"last_update": lastUpdate,
		"red":         statusToInt(event.RedLightStatus),
		"yellow":      statusToInt(event.YellowLightStatus),
		"green":       statusToInt(event.GreenLightStatus),
		"buzzer":      statusToInt(event.BuzzerStatus),
	}
	tower[stageLaneKey] = map[string]any{
		"last_update": lastUpdate,
		"green":       event.GreenLightStatus,
		"red":         event.RedLightStatus,
		"yellow":      event.YellowLightStatus,
		"buzzer":      event.BuzzerStatus,
	}

	return writeOutputState(outputFile, state)
}

func readOutputState(outputFile string) (map[string]any, error) {
	data, err := os.ReadFile(outputFile)
	if err != nil {
		if os.IsNotExist(err) {
			state := map[string]any{}
			ensureMachines(state)
			return state, nil
		}
		return nil, fmt.Errorf("read output state %q: %w", outputFile, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		state := map[string]any{}
		ensureMachines(state)
		return state, nil
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse output state %q: %w", outputFile, err)
	}
	return state, nil
}

func writeOutputState(outputFile string, state map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(outputFile), 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output state: %w", err)
	}
	data = append(data, '\n')

	tmpFile := fmt.Sprintf("%s.%d.%d.tmp", outputFile, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp output state: %w", err)
	}
	if err := replaceFile(tmpFile, outputFile); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("replace output state: %w", err)
	}
	return nil
}

func replaceFile(source string, target string) error {
	if err := os.Rename(source, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, target)
}

func ensureMachines(state map[string]any) {
	for machineNumber := 1; machineNumber <= 4; machineNumber++ {
		machineKey := fmt.Sprintf("machine_%d", machineNumber)
		machine, ok := state[machineKey].(map[string]any)
		if !ok {
			state[machineKey] = map[string]any{
				"tower": defaultTower(),
			}
			continue
		}
		if _, ok := machine["tower"].(map[string]any); !ok {
			machine["tower"] = defaultTower()
		}
	}
}

func defaultTower() map[string]any {
	return map[string]any{
		"state": map[string]any{
			"last_update": "",
			"red":         0,
			"yellow":      0,
			"green":       0,
			"buzzer":      0,
		},
	}
}

func statusToInt(status string) int {
	if status == "01" {
		return 1
	}
	return 0
}

func safeFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "machine_state"
	}

	replacer := strings.NewReplacer(
		"\\", "_",
		"/", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		" ", "_",
	)
	return replacer.Replace(value)
}
