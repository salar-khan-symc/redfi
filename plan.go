package redfi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrNotFound is returned iff SelectRule can't find a Rule that applies
	ErrNotFound = errors.New("no matching rule found")
)

// Plan defines a set of rules to be applied by the proxy
type Plan struct {
	Rules []*Rule `json:"rules,omitempty"`
	// a lookup table mapping rule name to index in the array
	rulesMap map[string]int

	m sync.RWMutex
}

// Rule is what get's applied on every client message iff it matches it
type Rule struct {
	Name        string `json:"name,omiempty"`
	Delay       int    `json:"delay,omitempty"`
	Drop        bool   `json:"drop,omitempty"`
	ReturnEmpty bool   `json:"return_empty,omitempty"`
	Percentage  int    `json:"percentage,omitempty"`
	// SelectRule does prefix matching on this value
	ClientAddr string `json:"client_addr,omitempty"`
	Command    string `json:"command,omitempty"`
	// filled by marshalCommand
	marshaledCmd []byte
}

// Parse the plan.json file
func Parse(planPath string) (*Plan, error) {
	fullPath, err := filepath.Abs(planPath)
	if err != nil {
		return nil, err
	}

	fd, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}

	buf, err := ioutil.ReadAll(fd)
	if err != nil {
		return nil, err
	}

	// this is the plan we will use
	plan := &Plan{rulesMap: map[string]int{}}

	// this is a draft of the plan
	// we use to parse the json file,
	// then copy its rules to the real plan
	pd := &Plan{}
	err = json.Unmarshal(buf, pd)
	if err != nil {
		return nil, err
	}

	for i, rule := range pd.Rules {
		if rule == nil {
			continue
		}
		err := plan.AddRule(*rule)
		if err != nil {
			return plan, fmt.Errorf("encountered error when adding rule #%d: %s", i, err)
		}
	}

	return plan, nil
}

func NewPlan() *Plan {
	return &Plan{
		Rules:    []*Rule{},
		rulesMap: map[string]int{},
	}
}

func (p *Plan) check() error {
	for idx, rule := range p.Rules {
		if rule.Percentage < 0 || rule.Percentage > 100 {
			return fmt.Errorf("Percentage in rule #%d is malformed. it must within 0-100", idx)
		}
	}
	return nil
}

func (p *Plan) MarshalCommands() {
	for _, rule := range p.Rules {
		if rule == nil {
			continue
		}
		if len(rule.Command) > 0 {
			rule.marshaledCmd = marshalCommand(rule.Command)
		}
	}
}

func marshalCommand(cmd string) []byte {
	return []byte(fmt.Sprintf("\r\n%s\r\n", strings.ToUpper(cmd)))
}

// SelectRule finds the first rule that applies to the given variables
func (p *Plan) SelectRule(clientAddr string, buf []byte) *Rule {
	var chosenRule *Rule
	for _, rule := range p.Rules {
		if len(rule.ClientAddr) > 0 && strings.HasPrefix(clientAddr, rule.ClientAddr) {
			continue
		}

		if len(rule.Command) > 0 && !bytes.Contains(buf, rule.marshaledCmd) {
			continue
		}

		chosenRule = rule
		break

	}
	if chosenRule == nil {
		return nil
	}

	if chosenRule.Percentage > 0 && rand.Intn(100) > chosenRule.Percentage {
		return nil
	}
	return chosenRule
}

// AddRule adds a rule to the current working plan
func (p *Plan) AddRule(r Rule) error {
	if r.Percentage < 0 || r.Percentage > 100 {
		return fmt.Errorf("Percentage in rule #%s is malformed. it must within 0-100", r.Name)
	}

	if len(r.Command) > 0 {
		r.marshaledCmd = marshalCommand(r.Command)
	}

	p.m.Lock()
	defer p.m.Unlock()

	p.Rules = append(p.Rules, &r)
	p.rulesMap[r.Name] = len(p.Rules) - 1

	return nil
}

// DeleteRule deletes the given ruleName if found
// otherwise it returns ErrNotFound
func (p *Plan) DeleteRule(name string) error {
	p.m.Lock()
	defer p.m.Unlock()

	idx, ok := p.rulesMap[name]
	if !ok {
		return ErrNotFound
	}

	p.Rules = append(p.Rules[:idx], p.Rules[idx+1:]...)
	delete(p.rulesMap, name)

	return nil
}

// GetRule returns the rule that matches the given name
func (p *Plan) GetRule(name string) (Rule, error) {
	p.m.RLock()
	defer p.m.RUnlock()

	idx, ok := p.rulesMap[name]
	if !ok {
		return Rule{}, ErrNotFound
	}

	return *p.Rules[idx], nil
}

// ListRules returns a slice of all the existing rules
// the slice will be empty if Plan has no rules
func (p *Plan) ListRules() []Rule {
	p.m.RLock()
	defer p.m.RUnlock()

	rules := []Rule{}
	for _, rule := range p.Rules {
		rules = append(rules, *rule)
	}

	return rules
}
