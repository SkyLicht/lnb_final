package npmtype1

import (
	"context"
	"encoding/xml"
	"fmt"

	"lnb_tk/internal/parser/types"
)

const (
	eventTypeTower = "tower_event"
	eventTypeCT    = "ct_event"
)

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
		return handleTowerEvent(req, event.Element), nil
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

func handleTowerEvent(req types.Request, event eventElement) types.Result {
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
	return types.Result{Records: 1}
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
