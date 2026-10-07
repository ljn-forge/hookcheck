package hookcheck

import (
	"encoding/json"
	"strings"
	"testing"
)

const validScenario = `{"version":1,"name":"payments","seed":7,"steps":[{"name":"paid","path":"/webhooks","events":[{"name":"paid-1","body":{"id":"event-1"},"repeat":3}]}],"checks":[{"name":"granted once","path":"/state","fields":{"grants":1,"status":"paid"}}]}`

func TestScenarioLoad(t *testing.T) {
	s, err := Load(strings.NewReader(validScenario))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "payments" || s.Steps[0].Events[0].Repeat != 3 {
		t.Fatalf("unexpected scenario: %+v", s)
	}
	if string(s.Steps[0].Events[0].Body) != `{"id":"event-1"}` {
		t.Fatal("body bytes changed")
	}
}

func TestScenarioRejectsInvalidInput(t *testing.T) {
	tests := map[string]string{
		"unknown field":             strings.Replace(validScenario, `"seed":7`, `"seed":7,"typo":true`, 1),
		"unknown nested field":      strings.Replace(validScenario, `"repeat":3`, `"repeats":3`, 1),
		"trailing JSON":             validScenario + ` {}`,
		"unsupported version":       strings.Replace(validScenario, `"version":1`, `"version":2`, 1),
		"no steps":                  `{"version":1,"name":"empty","steps":[]}`,
		"bad path":                  strings.Replace(validScenario, `"/webhooks"`, `"//evil.example/webhooks"`, 1),
		"fragment in delivery path": strings.Replace(validScenario, `"/webhooks"`, `"/webhooks#discarded"`, 1),
		"fragment in check path":    strings.Replace(validScenario, `"/state"`, `"/state?q=1#discarded"`, 1),
		"negative repeat":           strings.Replace(validScenario, `"repeat":3`, `"repeat":-1`, 1),
		"too much concurrency":      strings.Replace(validScenario, `"path":"/webhooks"`, `"path":"/webhooks","concurrency":65`, 1),
		"bad timeout":               strings.Replace(validScenario, `"seed":7`, `"seed":7,"timeout":"zero"`, 1),
		"zero timeout":              strings.Replace(validScenario, `"seed":7`, `"seed":7,"timeout":"0s"`, 1),
		"missing body":              strings.Replace(validScenario, `"body":{"id":"event-1"},`, ``, 1),
		"empty name":                strings.Replace(validScenario, `"name":"payments"`, `"name":""`, 1),
		"header injection":          strings.Replace(validScenario, `"seed":7`, `"seed":7,"headers":{"X-Test":"a\r\nb"}`, 1),
		"hop header":                strings.Replace(validScenario, `"seed":7`, `"seed":7,"headers":{"Content-Length":"2"}`, 1),
		"polling without window":    strings.Replace(validScenario, `"path":"/state"`, `"path":"/state","interval":"1ms"`, 1),
		"oversized":                 validScenario + strings.Repeat(" ", 1<<20),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(strings.NewReader(input)); err == nil {
				t.Fatal("invalid scenario accepted")
			}
		})
	}
}

func FuzzLoad(f *testing.F) {
	f.Add(validScenario)
	f.Add(`{"version":1}`)
	f.Add(`null`)
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 2<<20 {
			t.Skip()
		}
		s, err := Load(strings.NewReader(input))
		if err != nil {
			return
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Load(strings.NewReader(string(b))); err != nil {
			t.Fatal(err)
		}
	})
}
