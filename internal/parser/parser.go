package parser

import (
	"context"
	"fmt"

	"lnb_tk/internal/parser/functions/defaultparser"
	"lnb_tk/internal/parser/functions/npmtype1"
	"lnb_tk/internal/parser/functions/parsemachine01"
	"lnb_tk/internal/parser/functions/parsemachine02"
	"lnb_tk/internal/parser/types"
)

type Dispatcher struct {
	functions map[string]types.Function
}

func NewDispatcher() *Dispatcher {
	d := &Dispatcher{functions: make(map[string]types.Function)}
	d.Register("parse_machine_01", parsemachine01.Parse)
	d.Register("parse_machine_02", parsemachine02.Parse)
	d.Register("default_parser", defaultparser.Parse)
	d.Register("npm_type1", npmtype1.Parse)
	return d
}

func (d *Dispatcher) Register(name string, fn types.Function) {
	d.functions[name] = fn
}

func (d *Dispatcher) Parse(ctx context.Context, functionName string, req types.Request) (types.Result, error) {
	fn, ok := d.functions[functionName]
	if !ok {
		return types.Result{}, fmt.Errorf("parser function %q is not registered", functionName)
	}
	return fn(ctx, req)
}
